import type {
  AppSettings,
  ImageEditMode,
  InputImage,
  MaskDraft,
  MiloRouterConfig,
  StoredImage,
  StoredImageThumbnail,
  TaskParams,
  TaskRecord,
  TaskStatus,
} from '../types'
import { DEFAULT_PARAMS } from '../types'
import i18n from '@/i18n'
import { callImageApi } from '../lib/api'
import { assertOpenAIReferenceInputs, usesOpenAIReferenceLimits } from '../lib/referenceInputLimits'
import { buildStudioJobSpec } from '../lib/studioJobSpec'
import { createStudioJob, deleteStudioJob, getStudioJob, listStudioJobs, type StudioJob, type StudioJobStatus } from '../lib/studioJobsApi'
import { fetchMiloRouterImageModels } from '../lib/miloRouterApi'
import { createMiloRouterConfig, readStoredMiloRouterConfig, saveMiloRouterApiKey, stripApiKeysFromSettings } from '../lib/miloRouterConfig'
import { getImageModel, getModelResolution, getRequestModelId, GPT_IMAGE_2_MODEL_ID, migrateLegacyImageModelId, type ImageModelDefinition } from '../lib/modelRegistry'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'
import { normalizeParamsForSettings, MAX_OUTPUT_IMAGES, isRequestedImageSizeAvailable } from '../lib/paramCompatibility'
import {
  clearTasks as dbClearTasks,
  commitTaskDeletion,
  deleteImage as dbDeleteImage,
  getAllImageIds,
  getAllTasks,
  getImage,
  getImageStudioDbScope,
  getImageThumbnail,
  getLegacyImage,
  getLegacyTasks,
  getPendingSyncTaskIds,
  getPendingSyncRecords,
  getTaskSyncRecord,
  setTaskSyncRecord,
  clearTaskPendingSync,
  discardGoneTask,
  putImage,
  putTask as dbPutTask,
  putTaskWithPendingSync,
  setImageStudioUserScope,
  clearImages as dbClearImages,
  storeImage,
  storeImageWithSize,
  type GallerySyncRecord,
} from '../lib/db'
import { cacheImage, clearImageCaches, clearImageVariantCaches, deleteImageCacheEntry, ensureImageCached, registerCloudAssetIds } from '../lib/imageCache'
import { deleteGalleryTask, ensureGalleryIdentity, fetchGalleryAssetDataUrl, getGalleryAssetUrl, getGalleryStatus, listGalleryTasks, putGalleryAsset, GalleryApiError, getGalleryErrorMessage, type GalleryStats } from '../lib/galleryApi'
import { taskAssetIds, uploadGalleryTask } from '../lib/gallerySync'
import { captureGallerySession, isGallerySessionCurrent, isVerifiedGalleryAuth, verifyGalleryIdentity } from '../lib/gallerySession'
import { captureStudioIdentity, isCurrentStudioApiKey, isSameStudioLogin, isStudioIdentityCurrent, studioTokenUserId } from '../lib/studioKeySession'
import { blobToDataUrl, fileToDataUrl, imageSourceToBlob } from '../lib/dataUrl'
import { createTransparentOutputMeta, getTransparentRequestParams, removeKeyedBackgroundFromDataUrl } from '../lib/transparentImage'
import { createMaskConstrainedPrompt, orderInputImagesForMask } from '../lib/mask'
import { compositeMaskEditResult, createMaskedInputImage, loadImage, validateMaskMatchesImage } from '../lib/canvasImage'
import { addImageSizeParam, createTaskDonePatch, createTaskErrorPatch, mapRevisedPromptsByImage } from '../lib/taskState'
import { hasActiveDataOperations } from '../lib/dataOperations'
import {
  buildExportZip,
  createExportBlob,
  getExportImageEstimatedBytes,
  getExportZipPlan,
  readExportZip,
  readExportZipFileAsDataUrl,
} from '../lib/exportZip'
import { getBrowserNotificationSession, isBrowserNotificationBackground, showBrowserNotification } from '../lib/browserNotification'
import { ApiAuthError, ApiInsufficientBalanceError, getUserFacingApiErrorMessage } from '../lib/apiErrors'
import { remapImageMentionsForOrder, replaceImageMentionsForApi, stripImageMentionMarkers } from '../lib/promptImageMentions'
import { GPT_IMAGE_MIN_PIXELS, normalizeImageSize } from '../lib/size'
const MAX_INPUT_IMAGES = 16
const DEFAULT_API_TIMEOUT = 600
const ERROR_TOAST_MAX_LENGTH = 80
const taskControllers = new Map<string, AbortController>()
const stoppedTaskIds = new Set<string>()
let cacheClearInProgress = false
let submitInProgress = 0

export type ToastType = 'info' | 'success' | 'error'

export interface ConfirmDialogState {
  title: string
  message: string
  tone?: 'primary' | 'danger' | 'warning'
  confirmText?: string
  cancelText?: string
  action?: () => void | Promise<void>
}

export interface ImageEditSession {
  targetImageId: string
  sourceDataUrl: string
  prompt: string
  params: TaskParams
  inputImages: InputImage[]
  selectedModelId: string
  maskDraft: MaskDraft | null
}

export interface ImageEditCommit {
  mode: ImageEditMode
  prompt: string
  params: TaskParams
  selectedModelId: string
  maskDataUrl: string | null
}


/* ════════════════════════════════════════════════════════════════
 * 生图工作台状态。
 *
 * 迁移说明：原实现基于 zustand（含 persist 中间件）。这里保留全部
 * action 与业务逻辑原样不动，只把容器换成 Pinia + 形状兼容层，
 * 让「迁移」不等于「重写」——重写会引入大量回归风险。
 *
 * 类型上下文：builder 的返回值显式标注为 AppState，使 action 参数
 * 能从接口签名反推，避免改动 405 行 action 内部代码。
 * ════════════════════════════════════════════════════════════════ */
import { defineStore } from 'pinia'
import { reactive } from 'vue'


interface AppState {
  settings: AppSettings
  setSettings: (patch: Partial<AppSettings>) => void
  setMiloRouterConfig: (patch: Partial<MiloRouterConfig>) => void
  refreshImageModelCatalog: () => Promise<boolean>
  modelCatalog: ImageModelDefinition[]
  /** 后端算出的单张实价：map[计费模型][尺寸档位]，已含倍率 */
  imagePricing: Record<string, Record<string, number>>
  setImagePricing: (pricing: Record<string, Record<string, number>>) => void
  catalogStatus: 'idle' | 'loading' | 'ready' | 'error'
  catalogError?: string
  selectModel: (modelId: string) => void

  prompt: string
  setPrompt: (prompt: string) => void
  inputImages: InputImage[]
  addInputImage: (image: InputImage) => void
  replaceInputImage: (index: number, image: InputImage) => void
  removeInputImage: (index: number) => void
  clearInputImages: () => void
  setInputImages: (images: InputImage[], options?: { equivalentImageIds?: Record<string, string> }) => void
  moveInputImage: (fromIndex: number, toIndex: number) => void
  maskDraft: MaskDraft | null
  setMaskDraft: (draft: MaskDraft | null) => void
  clearMaskDraft: () => void
  maskEditorImageId: string | null
  setMaskEditorImageId: (id: string | null) => void
  imageEditSession: ImageEditSession | null
  commitImageEditSession: (commit: ImageEditCommit) => boolean

  params: TaskParams
  setParams: (patch: Partial<TaskParams>) => void

  tasks: TaskRecord[]
  galleryEnabled: boolean
  galleryInitialized: boolean
  galleryUserId: number | null
  galleryNextCursor: string | null
  galleryHasMore: boolean
  galleryLoading: boolean
  galleryStats: GalleryStats | null
  galleryStatusKnown: boolean
  studioJobsSupported: boolean
  studioJobsEnabled: boolean
  studioJobStatus: Record<string, StudioJobStatus>
  studioSubmissionStage: Record<string, 'preparing' | 'uploading' | 'submitting'>
  gallerySyncPaused: boolean
  gallerySyncStatus: Record<string, 'pending' | 'syncing' | 'synced' | 'error'>
  gallerySyncErrorCode: Record<string, string>
  galleryImportProgress: { total: number; processed: number; imported: number; skipped: number } | null
  streamPreviews: Record<string, { image: string; partialIndex: number; requestIndex?: number }>
  latestSubmittedTaskId: string | null
  setTasks: (tasks: TaskRecord[]) => void

  searchQuery: string
  setSearchQuery: (query: string) => void
  filterStatus: TaskStatus | 'all'
  setFilterStatus: (status: TaskStatus | 'all') => void

  detailTaskId: string | null
  detailImageId: string | null
  setDetailTaskId: (id: string | null, imageId?: string | null) => void
  lightboxImageId: string | null
  lightboxImageList: string[]
  setLightboxImageId: (id: string | null, list?: string[]) => void
  showSettings: boolean
  setShowSettings: (open: boolean) => void
  toast: { message: string; type: ToastType } | null
  showToast: (message: string, type?: ToastType) => void
  confirmDialog: ConfirmDialogState | null
  setConfirmDialog: (dialog: ConfirmDialogState | null) => void
}

const defaultSettings = (): AppSettings => ({
  miloRouter: readStoredMiloRouterConfig(),
  selectedModelId: GPT_IMAGE_2_MODEL_ID,
  connectionOnboardingSeen: false,
  timeout: DEFAULT_API_TIMEOUT,
  clearInputAfterSubmit: false,
  persistInputOnRestart: true,
  taskCompletionNotification: false,
  enterSubmit: false,
})



type StoreBuilder = (
  set: (partial: Partial<AppState> | ((s: AppState) => Partial<AppState>)) => void,
  get: () => AppState,
) => AppState

const buildStore: StoreBuilder = (set, get) => {
  let catalogRequest = 0
  return ({
        settings: defaultSettings(),
        modelCatalog: [],
        imagePricing: {},
        catalogStatus: 'idle',
        catalogError: undefined,
        connectionDialogReason: 'manage',
        setSettings: (patch) => set((state) => ({
          settings: normalizeSettings({ ...state.settings, ...patch }),
        })),
        setMiloRouterConfig: (patch) => set((state) => {
          const next = { ...state.settings.miloRouter, ...patch, baseUrl: state.settings.miloRouter.baseUrl }
          const keyChanged = patch.apiKey !== undefined
            && patch.apiKey.trim() !== state.settings.miloRouter.apiKey.trim()
          if (keyChanged) catalogRequest += 1
          if (patch.apiKey !== undefined || patch.rememberApiKey !== undefined) {
            saveMiloRouterApiKey(next.apiKey, next.rememberApiKey)
          }
          return {
            settings: {
              ...state.settings,
              miloRouter: {
                ...next,
                connectionStatus: keyChanged ? 'idle' : next.connectionStatus,
                billing: keyChanged ? undefined : next.billing,
                error: keyChanged ? undefined : next.error,
              },
            },
            ...(keyChanged ? { modelCatalog: [], imagePricing: {}, catalogStatus: 'idle' as const, catalogError: undefined } : {}),
          }
        }),
        selectModel: (modelId) => set((state) => {
          const model = state.modelCatalog.find((item) => item.id === modelId)
          if (!model) return state
          const settings = { ...state.settings, selectedModelId: model.id }
          return {
            settings,
            params: normalizeParamsForSettings(state.params, settings, state.modelCatalog),
            maskDraft: model.capabilities.mask ? state.maskDraft : null,
          }
        }),
        refreshImageModelCatalog: async () => {
          const apiKey = get().settings.miloRouter.apiKey.trim()
          if (!apiKey) return false
          const identity = captureStudioIdentity()
          const request = ++catalogRequest
          set({ catalogStatus: 'loading', catalogError: undefined })
          try {
            const modelCatalog = await fetchMiloRouterImageModels(apiKey)
            if (request !== catalogRequest || get().settings.miloRouter.apiKey.trim() !== apiKey || !isStudioIdentityCurrent(identity)) return false
            set((state) => {
              const selectedModelId = modelCatalog.some((model) => model.id === state.settings.selectedModelId)
                ? state.settings.selectedModelId
                : modelCatalog[0].id
              const settings = { ...state.settings, selectedModelId }
              return {
                modelCatalog,
                catalogStatus: 'ready',
                catalogError: undefined,
                settings,
                params: normalizeParamsForSettings(state.params, settings, modelCatalog),
                maskDraft: modelCatalog.find((model) => model.id === selectedModelId)?.capabilities.mask ? state.maskDraft : null,
              }
            })
            return true
          } catch (error) {
            if (request !== catalogRequest || get().settings.miloRouter.apiKey.trim() !== apiKey || !isStudioIdentityCurrent(identity)) return false
            // 不能一律显示「加载失败，请稍后重试」：
            // 余额不足、Key 无效、超时都长这样，用户根本不知道该干嘛。
            // 这里按错误类型给出具体原因，认不出来才退回通用文案。
            set({
              modelCatalog: [],
              catalogStatus: 'error',
              catalogError: describeCatalogError(error),
            })
            return false
          }
        },
        setImagePricing: (imagePricing) => set({ imagePricing }),
        prompt: '',
        setPrompt: (prompt) => set({ prompt }),
        inputImages: [],
        addInputImage: (image) => set((state) => state.inputImages.length >= MAX_INPUT_IMAGES
          ? state
          : { inputImages: [...state.inputImages, image] }),
        replaceInputImage: (index, image) => set((state) => {
          const inputImages = state.inputImages.map((item, itemIndex) => itemIndex === index ? image : item)
          const previous = state.inputImages[index]
          return {
            inputImages,
            prompt: remapImageMentionsForOrder(
              state.prompt,
              state.inputImages,
              inputImages,
              previous ? { [previous.id]: image.id } : {},
            ),
            maskDraft: state.maskDraft?.targetImageId === previous?.id
              ? { ...state.maskDraft, targetImageId: image.id }
              : state.maskDraft,
          }
        }),
        removeInputImage: (index) => set((state) => {
          const removed = state.inputImages[index]
          const inputImages = state.inputImages.filter((_, itemIndex) => itemIndex !== index)
          return {
            inputImages,
            prompt: remapImageMentionsForOrder(state.prompt, state.inputImages, inputImages),
            maskDraft: removed && state.maskDraft?.targetImageId === removed.id ? null : state.maskDraft,
          }
        }),
        clearInputImages: () => set((state) => ({
          inputImages: [],
          prompt: remapImageMentionsForOrder(state.prompt, state.inputImages, []),
          maskDraft: null,
        })),
        setInputImages: (inputImages, options) => set((state) => {
          const equivalent = options?.equivalentImageIds ?? {}
          const maskDraft = state.maskDraft && equivalent[state.maskDraft.targetImageId]
            ? { ...state.maskDraft, targetImageId: equivalent[state.maskDraft.targetImageId] }
            : state.maskDraft
          const nextInputImages = inputImages.slice(0, MAX_INPUT_IMAGES)
          return {
            inputImages: nextInputImages,
            prompt: remapImageMentionsForOrder(state.prompt, state.inputImages, nextInputImages, equivalent),
            maskDraft,
          }
        }),
        moveInputImage: (fromIndex, toIndex) => set((state) => {
          if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= state.inputImages.length || toIndex >= state.inputImages.length) return state
          const inputImages = [...state.inputImages]
          const [image] = inputImages.splice(fromIndex, 1)
          inputImages.splice(toIndex, 0, image)
          return {
            inputImages,
            prompt: remapImageMentionsForOrder(state.prompt, state.inputImages, inputImages),
          }
        }),
        maskDraft: null,
        setMaskDraft: (maskDraft) => set({ maskDraft }),
        clearMaskDraft: () => set({ maskDraft: null }),
        maskEditorImageId: null,
        imageEditSession: null,
        setMaskEditorImageId: (maskEditorImageId) => set((state) => {
          if (!maskEditorImageId) return { maskEditorImageId: null, imageEditSession: null }
          const source = state.inputImages.find((image) => image.id === maskEditorImageId)
          if (!source) return state
          return {
            maskEditorImageId,
            imageEditSession: {
              targetImageId: maskEditorImageId,
              sourceDataUrl: source.dataUrl,
              prompt: state.prompt,
              params: { ...state.params },
              inputImages: [...state.inputImages],
              selectedModelId: state.settings.selectedModelId,
              maskDraft: state.maskDraft?.targetImageId === maskEditorImageId ? state.maskDraft : null,
            },
          }
        }),
        commitImageEditSession: (commit) => {
          let committed = false
          set((state) => {
            const session = state.imageEditSession
            const model = state.modelCatalog.find((item) => item.id === commit.selectedModelId)
            if (
              !session ||
              !model ||
              state.catalogStatus !== 'ready' ||
              session.inputImages.length > model.capabilities.referenceImages ||
              (!model.capabilities.mask && !model.capabilities.transparentOutput) ||
              ((commit.mode === 'region' || commit.mode === 'erase') && (!commit.maskDataUrl || !model.capabilities.mask)) ||
              ((commit.mode === 'background' || commit.mode === 'full') && Boolean(commit.maskDataUrl)) ||
              (commit.mode === 'background' && !model.capabilities.transparentOutput)
            ) return state
            const settings = { ...state.settings, selectedModelId: model.id }
            committed = true
            return {
              settings,
              prompt: commit.prompt,
              params: normalizeParamsForSettings(commit.params, settings, state.modelCatalog),
              inputImages: session.inputImages,
              maskDraft: commit.maskDataUrl && model.capabilities.mask
                ? { targetImageId: session.targetImageId, maskDataUrl: commit.maskDataUrl, updatedAt: Date.now(), mode: commit.mode === 'erase' ? 'erase' : 'region' }
                : null,
              maskEditorImageId: null,
              imageEditSession: null,
            }
          })
          return committed
        },

        params: { ...DEFAULT_PARAMS },
        setParams: (patch) => set((state) => ({
          params: normalizeParamsForSettings({ ...state.params, ...patch }, state.settings, state.modelCatalog),
        })),

        tasks: [],
        galleryEnabled: false,
        galleryInitialized: false,
        galleryUserId: null,
        galleryNextCursor: null,
        galleryHasMore: false,
        galleryLoading: false,
        galleryStats: null,
        galleryStatusKnown: false,
        studioJobsSupported: false,
        studioJobsEnabled: false,
        studioJobStatus: {},
        studioSubmissionStage: {},
        gallerySyncPaused: false,
        gallerySyncStatus: {},
        gallerySyncErrorCode: {},
        galleryImportProgress: null,
        streamPreviews: {},
        latestSubmittedTaskId: null,
        setTasks: (tasks) => set({ tasks }),

        searchQuery: '',
        setSearchQuery: (searchQuery) => set({ searchQuery }),
        filterStatus: 'all',
        setFilterStatus: (filterStatus) => set({ filterStatus }),

        detailTaskId: null,
        detailImageId: null,
        setDetailTaskId: (detailTaskId, imageId) => set({ detailTaskId, detailImageId: detailTaskId ? imageId ?? null : null }),
        lightboxImageId: null,
        lightboxImageList: [],
        setLightboxImageId: (lightboxImageId, lightboxImageList = []) => set({
          lightboxImageId,
          lightboxImageList: lightboxImageId ? lightboxImageList : [],
        }),
        showSettings: false,
        setShowSettings: (showSettings) => set({ showSettings }),
        toast: null,
        showToast: (message, type = 'info') => set({ toast: { message: getToastMessage(message, type), type } }),
        confirmDialog: null,
        setConfirmDialog: (confirmDialog) => set({ confirmDialog }),
  })
}

function createImageStudioStore(): AppState {
  return buildStore(
    (partial) => {
      const patch = typeof partial === 'function'
        ? (partial as (s: AppState) => Partial<AppState>)(storeState)
        : partial
      Object.assign(storeState, patch)
    },
    () => storeState,
  )
}

/**
 * 全局状态对象。
 *
 * 必须用 reactive 包装：组件直接引用它（`import { storeState as state }`），
 * 若保持普通对象，模板读取不会建立依赖，状态变了 UI 也不更新
 * —— 表现为「接口返回正确但页面始终是初始值」。
 */
/**
 * store 里没有组件上下文，用全局 i18n 实例翻译。
 *
 * 这些提示会直接弹给用户（toast / 错误原因），必须跟随界面语言，
 * 不能把中文写死在逻辑里。
 */
function st(key: string, params?: Record<string, unknown>): string {
  return i18n.global.t(key, params as never)
}

const storeState = reactive({}) as AppState

export const useStore = Object.assign(() => storeState, {
  getState: () => storeState,
  setState: (partial: Partial<AppState> | ((s: AppState) => Partial<AppState>)) => {
    const patch = typeof partial === 'function' ? partial(storeState) : partial
    Object.assign(storeState, patch)
  },
})

/** Pinia store：组件通过它拿到响应式状态 */
export const useImageStudioStore = defineStore('imageStudio', () => storeState)

export { storeState }

/* 延迟初始化：buildStore 内部会读 defaultSettings() 等，
   放到模块末尾避免 TDZ */
Object.assign(storeState, createImageStudioStore())


/**
 * 初始化本地数据。
 *
 * 从 IndexedDB 恢复历史任务与参考图，并把上次异常中断的任务标记为失败。
 *
 * 与原实现的差异：不再在「无 API Key」时弹出连接对话框 ——
 * 集成后密钥由后端按用户下发（见 lib/studioAuth.ts），
 * 用户不需要也不应该手动配置。
 */
let initPromise: Promise<void> | null = null
let initSession: string | null = null
const syncingGalleryTasks = new Map<string, Promise<void>>()
const deletingGalleryTasks = new Set<string>()
const studioJobPollers = new Map<string, { timer: ReturnType<typeof setTimeout> | null; controller: AbortController; session: ReturnType<typeof captureGallerySession>; misses: number }>()
const applyingStudioJobs = new Map<string, Promise<void>>()
const cloudCancellationRequested = new Set<string>()
const cloudStopWrites = new Map<string, Promise<unknown>>()
const cloudNotificationSessions = new Map<string, string | null>()
let galleryRetryTimer: ReturnType<typeof setTimeout> | null = null
let galleryRetryDueAt = 0
let galleryOnlineListenerInstalled = false

function galleryPauseKey(userId: number) { return 'milorouter-image-gallery-sync-paused-' + userId }

function clearGalleryRetryTimer() {
  if (galleryRetryTimer) clearTimeout(galleryRetryTimer)
  galleryRetryTimer = null
  galleryRetryDueAt = 0
}

function clearStudioJobPollers() {
  for (const poller of studioJobPollers.values()) {
    if (poller.timer) clearTimeout(poller.timer)
    poller.controller.abort()
  }
  studioJobPollers.clear()
  cloudCancellationRequested.clear()
  cloudStopWrites.clear()
  cloudNotificationSessions.clear()
}

function stopStudioJobPoller(taskId: string, session: ReturnType<typeof captureGallerySession>) {
  const key = session.scope + ':' + taskId
  const poller = studioJobPollers.get(key)
  if (!poller) return
  if (poller.timer) clearTimeout(poller.timer)
  poller.controller.abort()
  studioJobPollers.delete(key)
}

function scheduleGalleryRetry(scope: string, dueAt: number) {
  if (useStore.getState().gallerySyncPaused || !dueAt || galleryRetryDueAt && galleryRetryDueAt <= dueAt) return
  clearGalleryRetryTimer()
  galleryRetryDueAt = dueAt
  galleryRetryTimer = setTimeout(() => {
    clearGalleryRetryTimer()
    if (captureGallerySession().scope === scope) void retryPendingGalleryTasks()
  }, Math.max(0, dueAt - Date.now()))
}

function gallerySyncError(error: unknown): { code: string; retryable: boolean } {
  if (!(error instanceof GalleryApiError)) return { code: 'NETWORK', retryable: true }
  const code = error.code && /^[A-Z_]{2,64}$/.test(error.code) ? error.code : ''
  if (code === 'GALLERY_QUOTA_EXCEEDED' || code === 'GALLERY_DISABLED' || code === 'GALLERY_CONFLICT'
    || code === 'GALLERY_INVALID' || code === 'GALLERY_PUBLIC_STORAGE' || code === 'GALLERY_NOT_FOUND') {
    return { code, retryable: false }
  }
  if (error.status === 401 || error.status === 403) return { code: 'AUTH', retryable: false }
  if (error.status === 0) return { code: 'NETWORK', retryable: true }
  if (error.status >= 500 || error.status === 429) return { code: code || 'UNAVAILABLE', retryable: true }
  return { code: code || 'UNKNOWN', retryable: false }
}

function galleryRetryDelay(attempts: number) {
  return Math.min(5 * 60_000, 5000 * 2 ** Math.min(6, Math.max(0, attempts - 1)))
}

export function setGallerySyncPaused(paused: boolean) {
  const userId = useStore.getState().galleryUserId
  if (!userId) return
  localStorage.setItem(galleryPauseKey(userId), paused ? '1' : '0')
  useStore.setState({ gallerySyncPaused: paused })
  if (paused) clearGalleryRetryTimer()
  else void retryPendingGalleryTasks()
}

export async function refreshGalleryStats() {
  const session = captureGallerySession()
  if (!session.token || !session.userId) return false
  let status
  try { status = await getGalleryStatus() } catch (error) {
    if (isGallerySessionCurrent(session)) {
      const legacyServer = error instanceof GalleryApiError && error.status === 404
      useStore.setState({ galleryStatusKnown: legacyServer, ...(legacyServer ? { galleryEnabled: false, studioJobsSupported: false, studioJobsEnabled: false } : {}) })
    }
    return false
  }
  if (status.user_id !== session.userId || !isGallerySessionCurrent(session)) return false
  verifyGalleryIdentity(status.user_id, session.token)
  useStore.setState({ galleryEnabled: status.enabled, galleryStats: status.stats ?? null, galleryStatusKnown: true, studioJobsSupported: 'jobs_enabled' in status, studioJobsEnabled: status.jobs_enabled === true })
  return true
}

function authSessionKey() {
  return (localStorage.getItem('auth_token') ?? '') + '|' + (cachedUserId() ?? '')
}

function cachedUserId(): number | null {
  try {
    const value = JSON.parse(localStorage.getItem('auth_user') ?? 'null') as { id?: unknown } | null
    const id = Number(value?.id)
    return Number.isSafeInteger(id) && id > 0 ? id : null
  } catch {
    return null
  }
}

export async function loadNextGalleryPage() {
  const state = useStore.getState()
  if (!state.galleryHasMore || state.galleryLoading || !state.galleryUserId) return
  const session = captureGallerySession()
  if (!session.token) return
  try { await ensureGalleryIdentity(session.token) } catch { return }
  if (!isGallerySessionCurrent(session)) return
  const userId = state.galleryUserId
  useStore.setState({ galleryLoading: true })
  try {
    const page = await listGalleryTasks(state.galleryNextCursor)
    if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
    const pending = new Set(await getPendingSyncTaskIds(session.scope))
    if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
    const merged = new Map(useStore.getState().tasks.map((task) => [task.id, task]))
    for (const item of page.items) {
      if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
      const task = normalizeTask(item)
      if (!task || pending.has(task.id)) continue
      const local = merged.get(task.id)
      if (local?.cloudCancelRequested || local?.cloudJobStatus === 'cancelled') continue
      merged.set(task.id, task)
      await dbPutTask(task, session.scope)
    }
    if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
    for (const task of merged.values()) if (!pending.has(task.id)) registerCloudAssetIds(taskAssetIds(task))
    useStore.setState({
      tasks: [...merged.values()],
      galleryNextCursor: page.next_cursor,
      galleryHasMore: Boolean(page.next_cursor),
    })
  } catch {
    // Local history remains available while the cloud is offline.
  } finally {
    if (isGallerySessionCurrent(session) && userId === useStore.getState().galleryUserId) useStore.setState({ galleryLoading: false })
  }
}

export async function syncGalleryTask(taskId: string, force = false) {
  const state = useStore.getState()
  const authToken = localStorage.getItem('auth_token')
  if (!authToken) return
  try { await ensureGalleryIdentity(authToken) } catch { return }
  if (authToken !== localStorage.getItem('auth_token')) return
  if (!state.galleryUserId) return
  const task = state.tasks.find((item) => item.id === taskId)
  if (!task || task.status === 'running') return
  const session = captureGallerySession()
  const userId = state.galleryUserId
  const key = session.scope + ':' + taskId
  if (deletingGalleryTasks.has(key)) return
  const existing = syncingGalleryTasks.get(key)
  if (existing) return existing
  const job = (async () => {
    const previous = await getTaskSyncRecord(taskId, session.scope)
    if (!previous || !isGallerySessionCurrent(session)) return
    if (!force && (state.gallerySyncPaused || !previous.retryable || previous.nextRetryAt > Date.now())) {
      useStore.setState((current) => ({
        gallerySyncStatus: { ...current.gallerySyncStatus, [taskId]: previous.status },
        gallerySyncErrorCode: previous.errorCode
          ? { ...current.gallerySyncErrorCode, [taskId]: previous.errorCode }
          : current.gallerySyncErrorCode,
      }))
      if (previous.retryable) scheduleGalleryRetry(session.scope, previous.nextRetryAt)
      return
    }
    if (!state.galleryEnabled) return
    const attempts = previous.attempts + 1
    await setTaskSyncRecord(taskId, { status: 'syncing', attempts, nextRetryAt: 0, retryable: true }, session.scope)
    if (!isGallerySessionCurrent(session)) return
    useStore.setState((current) => ({ gallerySyncStatus: { ...current.gallerySyncStatus, [taskId]: 'syncing' } }))
    try {
      await uploadGalleryTask(task, () => isGallerySessionCurrent(session) && !deletingGalleryTasks.has(key), session.scope)
      if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId || deletingGalleryTasks.has(key)) return
      await clearTaskPendingSync(taskId, session.scope)
      if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId || deletingGalleryTasks.has(key)) return
      registerCloudAssetIds(taskAssetIds(task))
      useStore.setState((current) => {
        const { [taskId]: _, ...gallerySyncErrorCode } = current.gallerySyncErrorCode
        return { gallerySyncStatus: { ...current.gallerySyncStatus, [taskId]: 'synced' }, gallerySyncErrorCode }
      })
      void refreshGalleryStats()
    } catch (error) {
      if (error instanceof GalleryApiError && error.status === 410 && error.code === 'GALLERY_GONE') {
        const discarded = await discardGoneTask(taskId, session.scope).then(() => true, () => false)
        if (discarded) {
          if (isGallerySessionCurrent(session) && userId === useStore.getState().galleryUserId) {
            useStore.setState((current) => {
              const { [taskId]: _, ...gallerySyncStatus } = current.gallerySyncStatus
              const { [taskId]: _error, ...gallerySyncErrorCode } = current.gallerySyncErrorCode
              return { tasks: current.tasks.filter((item) => item.id !== taskId), gallerySyncStatus, gallerySyncErrorCode }
            })
          }
          void refreshGalleryStats()
          return
        }
      }
      if (isGallerySessionCurrent(session) && userId === useStore.getState().galleryUserId && !deletingGalleryTasks.has(key)) {
        const failure = gallerySyncError(error)
        const nextRetryAt = failure.retryable ? Date.now() + galleryRetryDelay(attempts) : 0
        const record: GallerySyncRecord = { status: 'error', attempts, nextRetryAt, retryable: failure.retryable, errorCode: failure.code }
        await setTaskSyncRecord(taskId, record, session.scope)
        if (!isGallerySessionCurrent(session)) return
        useStore.setState((current) => ({
          gallerySyncStatus: { ...current.gallerySyncStatus, [taskId]: 'error' },
          gallerySyncErrorCode: { ...current.gallerySyncErrorCode, [taskId]: failure.code },
        }))
        if (failure.retryable) scheduleGalleryRetry(session.scope, nextRetryAt)
      }
    }
  })()
  syncingGalleryTasks.set(key, job)
  try { await job } finally {
    syncingGalleryTasks.delete(key)
    const refreshed = captureGallerySession()
    if (refreshed.token && refreshed.token !== session.token && refreshed.userId === userId && refreshed.scope === session.scope) {
      void ensureGalleryIdentity(refreshed.token).then(() => retryPendingGalleryTasks()).catch(() => undefined)
    }
  }
}

export async function retryGalleryTask(taskId: string) {
  await refreshGalleryStats()
  if (!useStore.getState().galleryEnabled) {
    useStore.getState().showToast(st('imageStudio.gallery.disabled'), 'info')
    return
  }
  await syncGalleryTask(taskId, true)
}

export async function retryFailedGalleryTasks() {
  await refreshGalleryStats()
  if (!useStore.getState().galleryEnabled) {
    useStore.getState().showToast(st('imageStudio.gallery.disabled'), 'info')
    return
  }
  const session = captureGallerySession()
  const ids = Object.entries(useStore.getState().gallerySyncStatus)
    .filter(([, status]) => status === 'error')
    .map(([id]) => id)
  for (const id of ids) {
    if (!isGallerySessionCurrent(session)) return
    await syncGalleryTask(id, true)
  }
}

export async function retryPendingGalleryTasks() {
  const session = captureGallerySession()
  if (!session.token || !session.userId || session.scope !== 'milorouter-ai-image-user-' + session.userId) return
  if (useStore.getState().gallerySyncPaused) return
  if (!useStore.getState().galleryEnabled) {
    const status = await getGalleryStatus().catch(() => null)
    if (!status || !status.enabled || status.user_id !== session.userId || !isGallerySessionCurrent(session)) return
    verifyGalleryIdentity(status.user_id, session.token)
    useStore.setState({ galleryEnabled: true, galleryStats: status.stats ?? null })
  }
  const entries = await getPendingSyncRecords(session.scope).catch(() => [])
  if (!isGallerySessionCurrent(session)) return
  for (const { id, record } of entries) {
    if (!isGallerySessionCurrent(session)) return
    if (!record.retryable) continue
    if (record.nextRetryAt > Date.now()) {
      scheduleGalleryRetry(session.scope, record.nextRetryAt)
      continue
    }
    await syncGalleryTask(id)
  }
}

/** Legacy data has no owner; copying it into this user is always an explicit action. */
export async function importLegacyHistory() {
  const state = useStore.getState()
  if (!state.galleryInitialized || !state.galleryUserId) {
    state.showToast(st('imageStudio.gallery.importFailed'), 'error')
    return
  }
  const session = captureGallerySession()
  const userId = state.galleryUserId
  try {
    const legacy = await getLegacyTasks()
    if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
    const known = new Set(state.tasks.map((task) => task.id))
    const imported: TaskRecord[] = []
    let processed = 0
    let skipped = 0
    useStore.setState({ galleryImportProgress: { total: legacy.length, processed: 0, imported: 0, skipped: 0 } })
    const advance = (didImport: boolean) => {
      processed += 1
      if (!didImport) skipped += 1
      if (isGallerySessionCurrent(session) && userId === useStore.getState().galleryUserId) {
        useStore.setState({ galleryImportProgress: { total: legacy.length, processed, imported: imported.length, skipped } })
      }
    }
    for (const raw of legacy) {
      if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
      const task = normalizeTask(raw)
      if (!task || known.has(task.id)) { advance(false); continue }
      if (task.status === 'running') Object.assign(task, createTaskErrorPatch(task, st('imageStudio.store.interrupted'), Date.now()))
      const images = await Promise.all(taskAssetIds(task).map(getLegacyImage))
      if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
      if (images.some((image) => !image?.dataUrl)) { advance(false); continue }
      const collisions = await Promise.all(images.map((image) => image ? getImage(image.id, session.scope) : undefined))
      if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
      if (collisions.some((current, index) => current && current.dataUrl !== images[index]?.dataUrl)) { advance(false); continue }
      for (const image of images) {
        if (!isGallerySessionCurrent(session)) return
        if (image) await putImage(image, session.scope)
      }
      if (!isGallerySessionCurrent(session)) return
      await putTaskWithPendingSync(task, session.scope)
      imported.push(task)
      known.add(task.id)
      advance(true)
    }
    if (!isGallerySessionCurrent(session) || userId !== useStore.getState().galleryUserId) return
    useStore.setState((current) => ({
      tasks: [...current.tasks, ...imported],
      gallerySyncStatus: Object.fromEntries([
        ...Object.entries(current.gallerySyncStatus),
        ...imported.map((task) => [task.id, 'pending' as const]),
      ]),
    }))
    state.showToast(st('imageStudio.gallery.importLocalDone', { count: imported.length, skipped }), imported.length ? 'success' : 'info')
    if (state.galleryEnabled) void retryPendingGalleryTasks()
  } catch {
    state.showToast(st('imageStudio.gallery.importFailed'), 'error')
  }
}

export function initStore(): Promise<void> {
  if (!galleryOnlineListenerInstalled && typeof window !== 'undefined') {
    window.addEventListener('online', () => { void retryPendingGalleryTasks() })
    galleryOnlineListenerInstalled = true
  }
  const session = authSessionKey()
  const authToken = localStorage.getItem('auth_token')
  const localUserId = cachedUserId()
  const tokenUserId = studioTokenUserId(authToken)
  const scopedUserId = authToken && tokenUserId === localUserId ? localUserId : null
  if (initPromise && initSession === session) return initPromise
  if (initSession !== session) {
    // Isolate storage before the first await; the old user's DB must not remain active.
    clearGalleryRetryTimer()
    clearStudioJobPollers()
    setImageStudioUserScope(scopedUserId)
    clearImageCaches()
    useStore.setState({ tasks: [], inputImages: [], gallerySyncStatus: {}, gallerySyncErrorCode: {}, galleryImportProgress: null, galleryStats: null, galleryStatusKnown: false, studioJobsSupported: false, studioJobsEnabled: false, studioJobStatus: {}, studioSubmissionStage: {}, gallerySyncPaused: false, galleryUserId: null, galleryEnabled: false, galleryInitialized: false, galleryLoading: false, galleryHasMore: false, galleryNextCursor: null, confirmDialog: null })
    if (initSession !== null) {
      // A failed key fetch for the new user must never leave the previous user's
      // connected key, catalog or prices available in this shared store.
      useStore.getState().setMiloRouterConfig({ apiKey: '', connectionStatus: 'idle' })
      useStore.setState({ modelCatalog: [], imagePricing: {}, catalogStatus: 'idle', catalogError: undefined })
    }
  }
  initSession = session

  initPromise = (async () => {
    try {
      let status
      let statusKnown = false
      try {
        status = await getGalleryStatus()
        statusKnown = true
      } catch (error) {
        status = null
        statusKnown = error instanceof GalleryApiError && error.status === 404
      }
      if (session !== authSessionKey()) return
      if (status && status.user_id !== cachedUserId()) return
      if (status && tokenUserId && status.user_id !== tokenUserId) return
      const userId = status?.user_id ?? scopedUserId
      setImageStudioUserScope(userId)
      if (status && authToken && userId && cachedUserId() === userId) verifyGalleryIdentity(userId, authToken)
      useStore.setState({
        galleryUserId: userId,
        galleryEnabled: status?.enabled === true,
        galleryStats: status?.stats ?? null,
        galleryStatusKnown: statusKnown,
        studioJobsSupported: Boolean(status && 'jobs_enabled' in status),
        studioJobsEnabled: status?.jobs_enabled === true,
        gallerySyncPaused: userId ? localStorage.getItem(galleryPauseKey(userId)) === '1' : false,
        galleryNextCursor: null,
        galleryHasMore: Boolean(status && userId),
      })
      const loadedTasks = (await getAllTasks())
        .map(normalizeTask)
        .filter((task): task is TaskRecord => Boolean(task))
      if (session !== authSessionKey()) return

      const current = useStore.getState()

      // 收藏功能已下线：旧数据里可能残留 isFavorite / favoriteCollectionIds，
      // 这里统一剥掉，避免残缺字段继续流到 UI 或后续持久化里。
      const cleanedTasks = loadedTasks.map((task) => {
        const raw = task as TaskRecord & Record<string, unknown>
        if (!('isFavorite' in raw) && !('favoriteCollectionIds' in raw)) return task
        const { isFavorite: _isFavorite, favoriteCollectionIds: _favoriteCollectionIds, ...rest } = raw
        return rest as unknown as TaskRecord
      })

      // 上次退出时仍在生成的任务 → 标记为中断
      const interrupted = cleanedTasks.map((task) =>
        task.status === 'running' && task.execution !== 'cloud'
          ? { ...task, ...createTaskErrorPatch(task, st('imageStudio.store.interrupted'), Date.now()) }
          : task,
      )
      const taskScope = getImageStudioDbScope()
      await Promise.all(interrupted.map((task, index) => {
        if (task.status !== 'error') return Promise.resolve()
        // A task interrupted before its first successful upload still needs an outbox marker.
        return cleanedTasks[index].status === 'running' && cleanedTasks[index].execution !== 'cloud' && userId
          ? putTaskWithPendingSync(task, taskScope)
          : dbPutTask(task, taskScope)
      }))
      if (session !== authSessionKey()) return

      // 参考图只存了元数据，需要把 dataUrl 从缓存补回来
      const inputImages = await Promise.all(
        current.inputImages.map(async (image) => ({
          id: image.id,
          dataUrl: image.dataUrl || (await ensureImageCached(image.id)) || '',
        })),
      )
      const usableInputImages = inputImages.filter((image) => image.dataUrl)
      if (session !== authSessionKey()) return
      const pendingEntries = await getPendingSyncRecords().catch(() => [])
      if (session !== authSessionKey()) return

      useStore.setState({
        tasks: interrupted,
        galleryInitialized: true,
        gallerySyncStatus: Object.fromEntries(pendingEntries.map(({ id, record }) => [id, record.status])),
        gallerySyncErrorCode: Object.fromEntries(pendingEntries.filter(({ record }) => record.errorCode).map(({ id, record }) => [id, record.errorCode!])),
        prompt: remapImageMentionsForOrder(
          current.prompt,
          current.inputImages,
          usableInputImages,
        ),
        inputImages: usableInputImages,
        params: normalizeParamsForSettings(current.params, current.settings, current.modelCatalog),
      })
      if (session !== authSessionKey()) return
      if (status && userId) await loadNextGalleryPage()
      if (status && 'jobs_enabled' in status && userId && authToken) void restoreStudioJobs(authToken)
      if (status?.enabled && !useStore.getState().gallerySyncPaused) void retryPendingGalleryTasks()
    } catch (error) {
      if (session !== authSessionKey()) return
      console.warn('初始化本地数据失败', error)
      useStore.getState().showToast(st('imageStudio.store.localLoadFailed'), 'error')
      useStore.setState({ galleryInitialized: true })
    }
  })()

  return initPromise
}

async function isGenerationSessionCurrent(session: ReturnType<typeof captureGallerySession>): Promise<boolean> {
  if (isGallerySessionCurrent(session)) return true
  const refreshed = captureGallerySession()
  if (!refreshed.token || !session.userId || refreshed.userId !== session.userId || refreshed.scope !== session.scope) return false
  if (isSameStudioLogin(session.token, refreshed.token)) return true
  try { await ensureGalleryIdentity(refreshed.token) } catch { return false }
  const current = captureGallerySession()
  return current.token === refreshed.token && current.userId === session.userId && current.scope === session.scope
}

async function currentCloudSession(original: ReturnType<typeof captureGallerySession>) {
  if (isGallerySessionCurrent(original)) return original
  const refreshed = captureGallerySession()
  if (!refreshed.token || refreshed.userId !== original.userId || refreshed.scope !== original.scope) return null
  try { await ensureGalleryIdentity(refreshed.token) } catch { return null }
  return isGallerySessionCurrent(refreshed) ? refreshed : null
}

function studioJobIsActive(status: StudioJobStatus) {
  return status === 'queued' || status === 'generating' || status === 'saving'
}

function wantsCloudCancellation(taskId: string) {
  return cloudCancellationRequested.has(taskId)
    || useStore.getState().tasks.some((task) => task.id === taskId && task.cloudCancelRequested === true)
}

async function finishCloudCancellation(taskId: string, session: ReturnType<typeof captureGallerySession>) {
  cloudCancellationRequested.delete(taskId)
  if (!taskControllers.has(taskId)) stoppedTaskIds.delete(taskId)
  stopStudioJobPoller(taskId, session)
  if (!isGallerySessionCurrent(session)) return
  await cloudStopWrites.get(taskId)?.catch(() => undefined)
  cloudStopWrites.delete(taskId)
  const task = useStore.getState().tasks.find((item) => item.id === taskId)
  if (!task?.cloudCancelRequested) return
  const { cloudCancelRequested: _requested, ...rest } = task
  const cancelled: TaskRecord = { ...rest, cloudJobStatus: 'cancelled' }
  useStore.setState((current) => ({
    tasks: current.tasks.map((item) => item.id === taskId ? cancelled : item),
    studioJobStatus: { ...current.studioJobStatus, [taskId]: 'cancelled' },
  }))
  await dbPutTask(cancelled, session.scope)
}

async function applyStudioJob(job: StudioJob, session: ReturnType<typeof captureGallerySession>) {
  const key = session.scope + ':' + job.task_id
  const previous = applyingStudioJobs.get(key) ?? Promise.resolve()
  const operation = previous.catch(() => undefined).then(() => applyStudioJobNow(job, session))
  applyingStudioJobs.set(key, operation)
  try { await operation } finally {
    if (applyingStudioJobs.get(key) === operation) applyingStudioJobs.delete(key)
  }
}

async function applyStudioJobNow(job: StudioJob, session: ReturnType<typeof captureGallerySession>) {
  if (!isGallerySessionCurrent(session) || session.userId !== useStore.getState().galleryUserId) return
  if (deletingGalleryTasks.has(session.scope + ':' + job.task_id)) return
  if (wantsCloudCancellation(job.task_id)) return
  if (useStore.getState().tasks.some((task) => task.id === job.task_id && task.cloudJobStatus === 'cancelled')) return
  const knownStatus = useStore.getState().studioJobStatus[job.task_id]
  if (knownStatus && !studioJobIsActive(knownStatus) && studioJobIsActive(job.status)) return
  const normalized = normalizeTask(job.task)
  if (!normalized || normalized.id !== job.task_id) return
  const active = studioJobIsActive(job.status)
  const done = (job.status === 'completed' || job.status === 'partial') && normalized.outputImages.length > 0
  const notifySession = !active ? cloudNotificationSessions.get(job.task_id) : undefined
  if (!active) cloudNotificationSessions.delete(job.task_id)
  const task: TaskRecord = {
    ...normalized,
    execution: 'cloud',
    cloudJobStatus: job.status,
    status: active ? 'running' : done ? 'done' : 'error',
    error: active || done ? null : job.status === 'unknown'
      ? st('imageStudio.store.cloudResultUnknown')
      : job.status === 'cancelled'
        ? st('imageStudio.store.stopped')
        : normalized.error || st('imageStudio.store.cloudGenerationFailed'),
    finishedAt: active ? null : normalized.finishedAt ?? Date.now(),
  }
  useStore.setState((current) => ({
    tasks: current.tasks.some((item) => item.id === task.id)
      ? current.tasks.map((item) => item.id === task.id ? task : item)
      : [...current.tasks, task],
    studioJobStatus: { ...current.studioJobStatus, [task.id]: job.status },
    ...(done ? { gallerySyncStatus: { ...current.gallerySyncStatus, [task.id]: 'synced' as const } } : {}),
  }))
  await dbPutTask(task, session.scope)
  if (!isGallerySessionCurrent(session)) return
  if (deletingGalleryTasks.has(session.scope + ':' + job.task_id)) return
  if (done) {
    registerCloudAssetIds(taskAssetIds(task))
    void refreshGalleryStats()
    const current = useStore.getState()
    if (notifySession && current.settings.taskCompletionNotification && getBrowserNotificationSession() === notifySession
      && isBrowserNotificationBackground()) {
      showBrowserNotification(st('imageStudio.store.notificationTitle'), { body: task.prompt })
    }
  }
}

function pollStudioJob(taskId: string, session: ReturnType<typeof captureGallerySession>) {
  const authToken = session.token
  if (!authToken || !session.userId || !isGallerySessionCurrent(session)) return
  const key = session.scope + ':' + taskId
  if (studioJobPollers.has(key)) return
  const poller = { timer: null as ReturnType<typeof setTimeout> | null, controller: new AbortController(), session, misses: 0 }
  studioJobPollers.set(key, poller)
  const stop = () => {
    if (poller.timer) clearTimeout(poller.timer)
    poller.controller.abort()
    studioJobPollers.delete(key)
  }
  const rebindAfterRefresh = () => {
    stop()
    const refreshed = captureGallerySession()
    if (!refreshed.token || refreshed.token === session.token || refreshed.userId !== session.userId || refreshed.scope !== session.scope) return
    void ensureGalleryIdentity(refreshed.token).then(() => {
      if (isGallerySessionCurrent(refreshed)) pollStudioJob(taskId, refreshed)
    }).catch(() => undefined)
  }
  const next = (delay: number) => {
    if (!isGallerySessionCurrent(session)) { rebindAfterRefresh(); return }
    poller.timer = setTimeout(() => { void tick() }, delay)
  }
  const tick = async () => {
    if (!isGallerySessionCurrent(session)) { rebindAfterRefresh(); return }
    if (!studioJobPollers.has(key)) return
    try {
      const { job } = await getStudioJob(taskId, authToken, poller.controller.signal)
      if (!isGallerySessionCurrent(session)) { rebindAfterRefresh(); return }
      if (!studioJobPollers.has(key)) return
      if (wantsCloudCancellation(taskId)) {
        if (job.status === 'cancelled') {
          cloudNotificationSessions.delete(taskId)
          await finishCloudCancellation(taskId, session)
          return
        }
        try {
          await deleteStudioJob(taskId, authToken)
          cloudNotificationSessions.delete(taskId)
          await finishCloudCancellation(taskId, session)
        } catch (error) {
          next(error instanceof GalleryApiError && error.status === 404 ? 3000 : 10000)
        }
        return
      }
      poller.misses = 0
      await applyStudioJob(job, session)
      if (studioJobIsActive(job.status)) next(3000)
      else stop()
    } catch (error) {
      if (!isGallerySessionCurrent(session)) { rebindAfterRefresh(); return }
      if (!studioJobPollers.has(key)) return
      if (error instanceof GalleryApiError && (error.status === 401 || error.status === 403)) { stop(); return }
      if (error instanceof GalleryApiError && error.status === 404) {
        poller.misses += 1
        if (poller.misses >= 3) {
          const current = useStore.getState().tasks.find((item) => item.id === taskId)
          if (current?.status === 'running' && current.execution === 'cloud') {
            const failed: TaskRecord = { ...current, cloudJobStatus: 'unknown', ...createTaskErrorPatch(current,
              st('imageStudio.store.cloudSubmitUnconfirmed'), Date.now()) }
            useStore.setState((state) => ({ tasks: state.tasks.map((item) => item.id === taskId ? failed : item) }))
            // Admission is still ambiguous: never upload this ID into the gallery.
            await dbPutTask(failed, session.scope).catch(() => undefined)
          }
          cloudNotificationSessions.delete(taskId)
          stop()
          return
        }
      }
      next(error instanceof GalleryApiError && error.status === 404 ? 3000 : 10000)
    }
  }
  void tick()
}

/** Reconnects accepted jobs using only safe GET requests; it never calls a model. */
export async function resumeStudioJobsForCurrentUser() {
  const state = useStore.getState()
  const session = captureGallerySession()
  if (!state.galleryInitialized || !state.studioJobsSupported || !session.token || !session.userId || session.userId !== state.galleryUserId) return
  try { await ensureGalleryIdentity(session.token) } catch { return }
  if (!isGallerySessionCurrent(session)) return
  await restoreStudioJobs(session.token)
}

async function restoreStudioJobs(authToken: string) {
  const session = captureGallerySession()
  if (!session.userId || session.token !== authToken || !isGallerySessionCurrent(session)) return
  for (const task of useStore.getState().tasks) {
    if (task.execution === 'cloud' && (task.status === 'running' || task.cloudCancelRequested)) pollStudioJob(task.id, session)
  }
  let cursor: string | null = null
  for (let pageNumber = 0; pageNumber < 11; pageNumber += 1) {
    if (!isGallerySessionCurrent(session)) return
    let page
    try { page = await listStudioJobs(cursor, 100, authToken, undefined, true) } catch { return }
    if (!isGallerySessionCurrent(session)) return
    for (const job of page.items) {
      if (!isGallerySessionCurrent(session)) return
      if (wantsCloudCancellation(job.task_id)) { pollStudioJob(job.task_id, session); continue }
      await applyStudioJob(job, session)
      const currentStatus = useStore.getState().studioJobStatus[job.task_id]
      if (studioJobIsActive(job.status) && (!currentStatus || studioJobIsActive(currentStatus))) pollStudioJob(job.task_id, session)
    }
    cursor = page.next_cursor
    if (!cursor) break
  }
}

export async function submitTask() {
  if (cacheClearInProgress) {
    useStore.getState().showToast(st('imageStudio.gallery.cacheClearing'), 'info')
    return
  }
  if (submitInProgress > 0) return
  submitInProgress = 1
  try {
    if (!useStore.getState().galleryInitialized) await initStore()
    if (!useStore.getState().galleryStatusKnown) await refreshGalleryStats()
    if (!useStore.getState().galleryStatusKnown) {
      useStore.getState().showToast(st('imageStudio.store.cloudStatusUnavailable'), 'error')
      return
    }
    await submitTaskInternal()
  } finally { submitInProgress = 0 }
}

async function submitTaskInternal() {
  const state = useStore.getState()
  const submittedIdentity = captureStudioIdentity()
  const prompt = state.prompt.trim()
  if (!prompt) {
    state.showToast(st('imageStudio.store.promptRequired'), 'error')
    return
  }
  if (state.settings.miloRouter.apiKey && !isCurrentStudioApiKey(state.settings.miloRouter.apiKey.trim())) {
    const { ensureStudioApiKey } = await import('../lib/studioAuth')
    await ensureStudioApiKey()
    if (!isStudioIdentityCurrent(submittedIdentity)) return
  }
  const apiKey = state.settings.miloRouter.apiKey.trim()
  if (!apiKey || state.settings.miloRouter.connectionStatus !== 'connected' || !isCurrentStudioApiKey(apiKey)) {
    // 密钥由系统下发，用户无法也不该手填；这里只提示去哪恢复。
    state.showToast(st('imageStudio.store.keyNotReady'), 'error')
    state.setShowSettings(true)
    return
  }
  const generationSession = captureGallerySession()
  const generationScope = getImageStudioDbScope()
  const generationSettings = { ...state.settings, miloRouter: { ...state.settings.miloRouter } }
  const notificationSession = getBrowserNotificationSession()
  if (state.catalogStatus !== 'ready') {
    state.showToast(state.catalogError ?? st('imageStudio.store.catalogLoading'), 'error')
    return
  }
  if (state.tasks.some((task) => task.status === 'running')) {
    state.showToast(st('imageStudio.store.waitTask'), 'info')
    return
  }

  const modelId = state.settings.selectedModelId
  const model = getImageModel(modelId, state.modelCatalog)
  if (!model) {
    state.showToast(st('imageStudio.store.modelInvalid'), 'error')
    return
  }
  const params = normalizeParamsForSettings(state.params, state.settings, state.modelCatalog)
  if (!isRequestedImageSizeAvailable(model, state.params.resolution === 'auto' ? params : state.params)) {
    state.showToast(st('imageStudio.store.channelSizeUnavailable'), 'error')
    return
  }
  const capabilities = getImageModelCapabilities(modelId, state.modelCatalog)
  const requestModelId = getRequestModelId(model, params.size, params.resolution)
  if (!requestModelId) {
    state.showToast(st('imageStudio.store.resolutionUnavailable'), 'error')
    return
  }
  // A server job is selected only when the backend advertises a ready worker.
  // Partial-image SSE remains on the existing browser path until a resumable
  // preview channel exists; silently dropping that preview would be a regression.
  const useCloudJob = Boolean(state.galleryEnabled && state.studioJobsEnabled && generationSession.token
    && generationSession.userId && params.partial_images === 0)
  const inputImages = state.inputImages
  if (inputImages.length > model.capabilities.referenceImages) {
    state.showToast(`当前模型最多支持 ${model.capabilities.referenceImages} 张参考图`, 'error')
    return
  }
  const maskDraft = capabilities.mask ? state.maskDraft : null
  if (maskDraft && !inputImages.some((image) => image.id === maskDraft?.targetImageId)) {
    state.clearMaskDraft()
    state.showToast(st('imageStudio.store.maskTargetMissing'), 'error')
    return
  }

  const orderedInputImages = maskDraft
    ? orderInputImagesForMask(inputImages, maskDraft.targetImageId)
    : inputImages
  const inputImageDataUrls = await Promise.all(orderedInputImages.map(async (image) => image.dataUrl || await ensureImageCached(image.id) || ''))
  if (!await isGenerationSessionCurrent(generationSession)) return
  if (inputImageDataUrls.some((url) => !url)) {
    state.showToast(st('imageStudio.store.referenceReadFailed'), 'error')
    return
  }
  const openAIReferenceLimits = usesOpenAIReferenceLimits(model.api, model.provider)
  if (openAIReferenceLimits && orderedInputImages.length) {
    try { assertOpenAIReferenceInputs(inputImageDataUrls, maskDraft?.maskDataUrl) }
    catch (error) {
      if (!await isGenerationSessionCurrent(generationSession)) return
      state.showToast(st('imageStudio.store.referencePreparationFailed') + '\n' + getGalleryErrorMessage(error), 'error')
      return
    }
  }
  let requestInputImageDataUrls = inputImageDataUrls
  let maskImageId: string | null = null
  let maskTargetSize: string | null = null
  if (maskDraft) {
    try {
      await validateMaskMatchesImage(maskDraft.maskDataUrl, inputImageDataUrls[0])
      if (!await isGenerationSessionCurrent(generationSession)) return
      const maskedInput = await createMaskedInputImage(inputImageDataUrls[0], maskDraft.maskDataUrl)
      if (!await isGenerationSessionCurrent(generationSession)) return
      // Mask preprocessing creates a PNG, which can be larger than a compact
      // JPEG source. Check the actual submission bytes without resizing it.
      if (openAIReferenceLimits) assertOpenAIReferenceInputs([maskedInput], maskDraft.maskDataUrl)
      const sourceImage = await loadImage(inputImageDataUrls[0])
      if (!await isGenerationSessionCurrent(generationSession)) return
      maskTargetSize = model.capabilities.customSize
        ? normalizeImageSize(`${sourceImage.naturalWidth}x${sourceImage.naturalHeight}`, { minPixels: GPT_IMAGE_MIN_PIXELS })
        : null
      requestInputImageDataUrls = [maskedInput, ...inputImageDataUrls.slice(1)]
      maskImageId = await storeImage(maskDraft.maskDataUrl, 'mask', generationScope)
      if (!await isGenerationSessionCurrent(generationSession)) return
      cacheImage(maskImageId, maskDraft.maskDataUrl)
    } catch (error) {
      if (!await isGenerationSessionCurrent(generationSession)) return
      state.showToast(error instanceof Error ? error.message : st('imageStudio.store.maskInvalid'), 'error')
      return
    }
  }

  // 遮罩编辑与整图透明背景是互斥意图，避免旧任务参数把“擦除”误提交成移除背景。
  const transparent = Boolean(!maskDraft && params.transparent_output && model.capabilities.transparentOutput)
  const nativeTransparent = transparent && model.capabilities.nativeTransparentOutput
  const transparentMeta = transparent && !nativeTransparent ? createTransparentOutputMeta(prompt) : null
  const requestParams = {
    ...(transparent && !nativeTransparent ? getTransparentRequestParams(params) : params),
    ...(maskDraft ? { ...(maskTargetSize ? { size: maskTargetSize } : {}), transparent_output: false } : {}),
  }
  const now = Date.now()
  const task: TaskRecord = {
    id: createId(),
    ...(useCloudJob ? { execution: 'cloud' as const } : {}),
    prompt,
    params,
    apiKind: model.api,
    modelId: model.id,
    modelName: model.name,
    apiModel: requestModelId,
    ...(transparent ? { transparentOutput: true } : {}),
    ...(transparentMeta ? { transparentPrompt: transparentMeta.effectivePrompt } : {}),
    inputImageIds: orderedInputImages.map((image) => image.id),
    ...(maskDraft ? { editMode: maskDraft.mode ?? 'region' } : {}),
    maskTargetImageId: maskDraft?.targetImageId ?? null,
    maskImageId,
    outputImages: [],
    status: 'running',
    error: null,
    createdAt: now,
    finishedAt: null,
    elapsed: null,
  }
  if (useCloudJob) {
    cloudNotificationSessions.set(task.id, notificationSession)
    useStore.setState((current) => ({ studioSubmissionStage: { ...current.studioSubmissionStage, [task.id]: 'preparing' } }))
  }
  if (!await isGenerationSessionCurrent(generationSession)) return
  useStore.setState((current) => ({ tasks: [...current.tasks, task], latestSubmittedTaskId: task.id }))
  await dbPutTask(task, generationScope)
  if (!await isGenerationSessionCurrent(generationSession)) {
    await dbPutTask({ ...task, ...createTaskErrorPatch(task, st('imageStudio.store.interrupted'), Date.now()) }, generationScope)
    return
  }

  const controller = new AbortController()
  taskControllers.set(task.id, controller)
  try {
    if (useCloudJob) {
      const authToken = generationSession.token!
      const requestInputImageIds = [...task.inputImageIds]
      if (maskDraft) {
        const prepared = await storeImageWithSize(requestInputImageDataUrls[0], 'mask', generationScope)
        requestInputImageIds[0] = prepared.id
        cacheImage(prepared.id, requestInputImageDataUrls[0])
      }
      const assets = new Map<string, string>()
      task.inputImageIds.forEach((id, index) => assets.set(id, inputImageDataUrls[index]))
      if (maskDraft && maskImageId) {
        assets.set(maskImageId, maskDraft.maskDataUrl)
        assets.set(requestInputImageIds[0], requestInputImageDataUrls[0])
      }
      if (assets.size) useStore.setState((current) => ({ studioSubmissionStage: { ...current.studioSubmissionStage, [task.id]: 'uploading' } }))
      for (const [id, dataUrl] of assets) {
        if (controller.signal.aborted || stoppedTaskIds.has(task.id)) return
        if (!await isGenerationSessionCurrent(generationSession)) return
        try { await putGalleryAsset(id, dataUrl, authToken, controller.signal) }
        catch (error) {
          if (controller.signal.aborted) throw error
          throw new Error(st('imageStudio.store.referenceUploadFailed') + '\n' + getGalleryErrorMessage(error))
        }
      }
      if (controller.signal.aborted || stoppedTaskIds.has(task.id)) return
      if (!await isGenerationSessionCurrent(generationSession)) return
      const effectivePrompt = maskDraft
        ? createMaskConstrainedPrompt(replaceImageMentionsForApi(prompt, orderedInputImages.length))
        : replaceImageMentionsForApi(transparentMeta?.effectivePrompt ?? prompt, orderedInputImages.length)
      const spec = buildStudioJobSpec({
        settings: generationSettings,
        modelId: requestModelId,
        apiKind: model.api,
        modelCapabilities: model.capabilities,
        modelProvider: model.provider,
        prompt: effectivePrompt,
        params: requestParams,
        inputImageDataUrls: requestInputImageDataUrls,
        maskDataUrl: maskDraft?.maskDataUrl,
        nativeTransparentOutput: nativeTransparent,
      }, {
        inputImageIds: task.inputImageIds,
        requestInputImageIds,
        maskImageId: maskImageId ?? undefined,
        maskTargetImageId: maskDraft?.targetImageId,
        providerProfile: model.profiles?.length === 1 ? model.profiles[0].profile : undefined,
        postprocess: maskDraft ? 'mask' : transparent && !nativeTransparent ? 'keyed' : 'none',
      })
      useStore.setState((current) => ({ studioSubmissionStage: { ...current.studioSubmissionStage, [task.id]: 'submitting' } }))
      try {
        let job: StudioJob
        try {
          job = (await createStudioJob(task, spec, authToken)).job
        } catch (error) {
          if (!(error instanceof GalleryApiError && error.code === 'STUDIO_LOGIN_CHANGED_BEFORE_SEND')) throw error
          const refreshed = await currentCloudSession(generationSession)
          if (!refreshed?.token) return
          job = (await createStudioJob(task, spec, refreshed.token)).job
        }
        const activeSession = await currentCloudSession(generationSession)
        if (!activeSession?.token) return
        if (stoppedTaskIds.has(task.id) && !wantsCloudCancellation(task.id)) return
        if (wantsCloudCancellation(task.id)) {
          try {
            await deleteStudioJob(task.id, activeSession.token)
            await finishCloudCancellation(task.id, activeSession)
          } catch {
            pollStudioJob(task.id, activeSession)
          }
          return
        }
        clearStudioSubmissionStage(task.id)
        await applyStudioJob(job, activeSession)
        pollStudioJob(task.id, activeSession)
        if (generationSettings.clearInputAfterSubmit && isGallerySessionCurrent(activeSession)) useStore.setState({ prompt: '', inputImages: [], maskDraft: null })
        return
      } catch (error) {
        // The POST may have committed before a network failure. Query the same
        // task ID; never re-issue a paid model request to resolve ambiguity.
        const activeSession = await currentCloudSession(generationSession)
        if (!activeSession) return
        if (error instanceof GalleryApiError && error.status >= 400 && error.status < 500
          && error.status !== 401 && error.status !== 408 && error.status !== 429) {
          if (wantsCloudCancellation(task.id)) await finishCloudCancellation(task.id, activeSession)
          throw error
        }
        pollStudioJob(task.id, activeSession)
        return
      }
    }
    const result = await callImageApi({
      settings: generationSettings,
      modelId: requestModelId,
      apiKind: model.api,
      modelCapabilities: model.capabilities,
      modelProvider: model.provider,
      prompt: maskDraft
        ? createMaskConstrainedPrompt(replaceImageMentionsForApi(prompt, orderedInputImages.length))
        : replaceImageMentionsForApi(transparentMeta?.effectivePrompt ?? prompt, orderedInputImages.length),
      params: requestParams,
      inputImageDataUrls: requestInputImageDataUrls,
      maskDataUrl: maskDraft?.maskDataUrl,
      signal: controller.signal,
      nativeTransparentOutput: nativeTransparent,
      streamingGeneration: model.capabilities.streamingGeneration,
      streamingEdit: model.capabilities.streamingEdit,
      onPartialImage: (partial) => useStore.setState((current) => {
        const active = current.tasks.some((item) => item.id === task.id && item.status === 'running')
        return active && isGallerySessionCurrent(generationSession)
          ? { streamPreviews: { ...current.streamPreviews, [task.id]: partial } } : current
      }),
    })
    if (stoppedTaskIds.has(task.id)) return

    const outputImages: string[] = []
    const originalImages: string[] = []
    const actualParamsByImage: Record<string, Partial<TaskParams>> = {}
    const actualParamsList = result.actualParamsList ?? []
    for (const [index, image] of result.images.entries()) {
      const actual = actualParamsList[index]
      if (transparent && !nativeTransparent) {
        const original = await storeImageWithSize(image, 'generated', generationScope)
        originalImages.push(original.id)
        const processed = await removeKeyedBackgroundFromDataUrl(
          image,
          undefined,
          requestParams.output_format === 'webp' ? 'webp' : 'png',
          requestParams.output_compression,
        )
        const output = await storeImageWithSize(processed, 'generated', generationScope)
        outputImages.push(output.id)
        if (actual) actualParamsByImage[output.id] = actual
      } else {
        const finalImage = maskDraft
          ? await compositeMaskEditResult(
              inputImageDataUrls[0],
              image,
              maskDraft.maskDataUrl,
              requestParams.output_format,
              requestParams.output_compression,
            )
          : image
        const output = await storeImageWithSize(finalImage, 'generated', generationScope)
        outputImages.push(output.id)
        const paramsWithActualSize = addImageSizeParam(actual, output)
        if (paramsWithActualSize) actualParamsByImage[output.id] = paramsWithActualSize
      }
    }
    if (!outputImages.length) throw new Error(st('imageStudio.store.noImageReturned'))

    const finished = {
      ...task,
      ...createTaskDonePatch(task, Date.now()),
      outputImages,
      ...(originalImages.length ? { transparentOriginalImages: originalImages } : {}),
      ...(result.failedRequests?.length ? { outputErrors: result.failedRequests } : {}),
      ...(result.actualParams || Object.keys(actualParamsByImage).length
        ? { actualParams: result.actualParams, actualParamsByImage }
        : {}),
      ...(result.revisedPrompts ? { revisedPromptByImage: mapRevisedPromptsByImage(outputImages, result.revisedPrompts) } : {}),
    }
    if (await isGenerationSessionCurrent(generationSession)) {
      useStore.setState((current) => {
        const { [task.id]: _, ...streamPreviews } = current.streamPreviews
        return { tasks: current.tasks.map((item) => item.id === task.id ? finished : item), streamPreviews }
      })
    }
    await putTaskWithPendingSync(finished, generationScope)
    if (!await isGenerationSessionCurrent(generationSession)) return
    useStore.setState((current) => ({ gallerySyncStatus: { ...current.gallerySyncStatus, [task.id]: 'pending' } }))
    void syncGalleryTask(task.id)
    const current = useStore.getState()
    if (current.settings.taskCompletionNotification && notificationSession !== null &&
      getBrowserNotificationSession() === notificationSession && current.settings.miloRouter.apiKey.trim() === apiKey &&
      isBrowserNotificationBackground()) {
      showBrowserNotification(st('imageStudio.store.notificationTitle'), { body: prompt })
    }
    if (generationSettings.clearInputAfterSubmit) {
      useStore.setState({ prompt: '', inputImages: [], maskDraft: null })
    }
  } catch (error) {
    if (stoppedTaskIds.has(task.id)) return
    cloudNotificationSessions.delete(task.id)
    const message = error instanceof Error && error.message.startsWith(st('imageStudio.store.referenceUploadFailed'))
      ? error.message : getUserFacingApiErrorMessage(error)
    const failed = { ...task, ...createTaskErrorPatch(task, message, Date.now()) }
    if (await isGenerationSessionCurrent(generationSession)) {
      useStore.setState((current) => {
        const { [task.id]: _, ...streamPreviews } = current.streamPreviews
        return { tasks: current.tasks.map((item) => item.id === task.id ? failed : item), streamPreviews }
      })
    }
    await putTaskWithPendingSync(failed, generationScope)
    if (await isGenerationSessionCurrent(generationSession)) {
      useStore.setState((current) => ({ gallerySyncStatus: { ...current.gallerySyncStatus, [task.id]: 'pending' } }))
      void syncGalleryTask(task.id)
      state.showToast(message, 'error')
    }
  } finally {
    if (await isGenerationSessionCurrent(generationSession)) {
      useStore.setState((current) => {
        if (!current.streamPreviews[task.id]) return current
        const { [task.id]: _, ...streamPreviews } = current.streamPreviews
        return { streamPreviews }
      })
    }
    clearStudioSubmissionStage(task.id)
    taskControllers.delete(task.id)
    stoppedTaskIds.delete(task.id)
  }
}

function clearStudioSubmissionStage(taskId: string) {
  useStore.setState((current) => {
    const { [taskId]: _removed, ...studioSubmissionStage } = current.studioSubmissionStage
    return { studioSubmissionStage }
  })
}

export function stopTaskGeneration(taskId: string) {
  const task = useStore.getState().tasks.find((item) => item.id === taskId)
  if (task?.execution === 'cloud') {
    const session = captureGallerySession()
    const controller = taskControllers.get(taskId)
    cloudCancellationRequested.add(taskId)
    cloudNotificationSessions.delete(taskId)
    stoppedTaskIds.add(taskId)
    controller?.abort()
    const stopped: TaskRecord = { ...task, cloudCancelRequested: true, ...createTaskErrorPatch(task, st('imageStudio.store.stopped'), Date.now()) }
    useStore.setState((state) => ({ tasks: state.tasks.map((item) => item.id === taskId ? stopped : item) }))
    const stopWrite = dbPutTask(stopped, session.scope)
    cloudStopWrites.set(taskId, stopWrite)
    void stopWrite.then(
      () => { if (cloudStopWrites.get(taskId) === stopWrite) cloudStopWrites.delete(taskId) },
      () => { if (cloudStopWrites.get(taskId) === stopWrite) cloudStopWrites.delete(taskId) },
    )
    const stage = useStore.getState().studioSubmissionStage[taskId]
    if (stage === 'preparing' || stage === 'uploading') {
      // The paid job has not been submitted; cancellation is already certain.
      void finishCloudCancellation(taskId, session).catch(() => undefined)
      return
    }
    if (session.token && isGallerySessionCurrent(session)) {
      void deleteStudioJob(taskId, session.token).then(() => {
        if (!isGallerySessionCurrent(session)) return
        void finishCloudCancellation(taskId, session).catch(() => undefined)
      }, () => {
        if (!isGallerySessionCurrent(session)) return
        // 404 can precede a slow POST commit; retain the persisted stop intent.
        pollStudioJob(taskId, session)
      })
    }
    return
  }
  const controller = taskControllers.get(taskId)
  if (!controller) return
  stoppedTaskIds.add(taskId)
  controller.abort()
  if (!task) return
  const stopped = { ...task, ...createTaskErrorPatch(task, st('imageStudio.store.stopped'), Date.now()) }
  useStore.setState((state) => {
    const { [taskId]: _, ...streamPreviews } = state.streamPreviews
    return { tasks: state.tasks.map((item) => item.id === taskId ? stopped : item), streamPreviews }
  })
  void dbPutTask(stopped)
}

export async function reuseConfig(task: TaskRecord) {
  const state = useStore.getState()
  const session = captureGallerySession()
  const images = await Promise.all(task.inputImageIds.map(async (id) => ({
    id,
    dataUrl: await ensureImageCached(id) ?? '',
  })))
  if (!isGallerySessionCurrent(session)) return
  const modelId = task.modelId ?? getImageModel(task.apiModel ?? '')?.id
  const modelAvailable = Boolean(modelId && state.modelCatalog.some((model) => model.id === modelId))
  const settings = modelAvailable ? { ...state.settings, selectedModelId: modelId! } : state.settings
  const inputImages = images.filter((image) => image.dataUrl)
  useStore.setState({
    settings,
    prompt: remapImageMentionsForOrder(
      task.prompt,
      task.inputImageIds.map((id) => ({ id })),
      inputImages,
    ),
    params: normalizeParamsForSettings(task.params, settings, state.modelCatalog),
    inputImages,
    maskDraft: null,
  })
  state.showToast(modelAvailable ? st('imageStudio.store.paramsReused') : st('imageStudio.store.modelFallback'), modelAvailable ? 'success' : 'info')
}

/**
 * 判断一个 dataUrl 是否能真正当图片用。
 *
 * 空串、非 data:image/ 开头、长度过短（损坏记录）都不算可用。
 * 这类脏数据如果进了 inputImages，界面会渲染成「图片损坏」的占位符。
 */
function isUsableImageDataUrl(value: unknown): value is string {
  if (typeof value !== 'string') return false
  if (!value.startsWith('data:image/')) return false
  // 一个最小的有效 PNG base64 也有几百字符，过短基本可以判定为损坏
  return value.length > 128
}

export async function editOutputs(task: TaskRecord, imageIds = task.outputImages) {
  const state = useStore.getState()
  const session = captureGallerySession()
  const model = getImageModel(state.settings.selectedModelId, state.modelCatalog)
  const limit = Math.min(MAX_INPUT_IMAGES, model?.capabilities.referenceImages ?? 0)
  if (state.catalogStatus !== 'ready' || !limit) {
    state.showToast(st('imageStudio.store.needReferenceModel'), 'info')
    return
  }
  if (state.inputImages.length >= limit) {
    state.showToast(`参考图数量已达上限（${limit} 张）`, 'info')
    return
  }
  const existingIds = new Set(state.inputImages.map((image) => image.id))
  // 读取输出图作为参考图。
  //
  // 注意：必须校验 dataUrl 是合法的图片数据。
  // 之前只是 `?? ''`，一旦 IndexedDB 里读不到（配额清理、跨标签页删除、
  // 记录损坏），空字符串会原样塞进参考图列表，界面上就显示成一张「损坏的图」。
  // 现在读不到就直接丢弃，并在最后统一提示，而不是塞一个坏图进去。
  const outputImages = await Promise.all(
    [...new Set(imageIds)]
      .filter((id) => task.outputImages.includes(id))
      .map(async (id) => {
        const dataUrl = await ensureImageCached(id).catch(() => undefined)
        if (!isUsableImageDataUrl(dataUrl)) return null
        return { id, dataUrl }
      }),
  ).then((list) => list.filter((item): item is { id: string; dataUrl: string } => item !== null))
  if (!isGallerySessionCurrent(session)) return
  // 图片读取期间切换模型或修改草稿时，不覆盖用户的新输入。
  const latest = useStore.getState()
  if (latest.settings !== state.settings || latest.modelCatalog !== state.modelCatalog || latest.catalogStatus !== 'ready' || latest.inputImages !== state.inputImages || latest.prompt !== state.prompt || latest.params !== state.params || latest.maskDraft !== state.maskDraft || latest.maskEditorImageId !== state.maskEditorImageId) return
  const next = [...state.inputImages, ...outputImages.filter((image) => image.dataUrl && !existingIds.has(image.id))]
  const inputImages = next.slice(0, limit)
  if (inputImages.length === state.inputImages.length) {
    // 区分两种情况：图都读不出来（损坏/已清理） vs 本来就在列表里。
    // 前者必须明确告诉用户「图片读取失败」，否则用户只会看到一张坏图或没反应。
    state.showToast(
      outputImages.length === 0
        ? st('imageStudio.store.imageUnreadable')
        : st('imageStudio.store.noImagesToAdd'),
      'info',
    )
    return
  }
  useStore.setState({
    prompt: remapImageMentionsForOrder(
      task.prompt,
      task.inputImageIds.map((id) => ({ id })),
      inputImages,
    ),
    params: normalizeParamsForSettings(task.params, state.settings, state.modelCatalog),
    inputImages,
    maskDraft: null,
  })
  state.showToast(next.length > limit ? st('imageStudio.store.referenceLimit', { limit }) : st('imageStudio.store.outputsAdded'), 'success')
}

export async function editOutputMask(task: TaskRecord, imageId: string) {
  const state = useStore.getState()
  const session = captureGallerySession()
  const model = getImageModel(state.settings.selectedModelId, state.modelCatalog)
  const limit = Math.min(MAX_INPUT_IMAGES, model?.capabilities.referenceImages ?? 0)
  if (!task.outputImages.includes(imageId) || state.catalogStatus !== 'ready' || !model || (!model.capabilities.mask && !model.capabilities.transparentOutput) || !limit) {
    state.showToast(st('imageStudio.store.needEditModel'), 'info')
    return
  }
  if (!state.inputImages.some((image) => image.id === imageId) && state.inputImages.length >= limit) {
    state.showToast(`参考图数量已达上限（${limit} 张）`, 'info')
    return
  }
  const dataUrl = await ensureImageCached(imageId).catch(() => undefined)
  if (!isGallerySessionCurrent(session)) return
  const latest = useStore.getState()
  if (latest.settings !== state.settings || latest.modelCatalog !== state.modelCatalog || latest.catalogStatus !== 'ready' || latest.inputImages !== state.inputImages || latest.prompt !== state.prompt || latest.params !== state.params || latest.maskDraft !== state.maskDraft || latest.imageEditSession !== state.imageEditSession) return
  if (!dataUrl) {
    state.showToast(st('imageStudio.store.imageUnavailable'), 'info')
    return
  }
  // 编辑器使用独立会话；取消时不改动当前提示词、参数和参考图。
  const inputImages = [{ id: imageId, dataUrl }, ...state.inputImages.filter((image) => image.id !== imageId)]
  useStore.setState({
    maskEditorImageId: imageId,
    imageEditSession: {
      targetImageId: imageId,
      sourceDataUrl: dataUrl,
      prompt: '',
      params: normalizeParamsForSettings(task.params, state.settings, state.modelCatalog),
      inputImages,
      selectedModelId: model.id,
      maskDraft: null,
    },
  })
}

export async function retryTask(task: TaskRecord) {
  const state = useStore.getState()
  const session = captureGallerySession()
  if (state.tasks.some((item) => item.status === 'running')) {
    state.showToast(st('imageStudio.store.waitOrStop'), 'info')
    return
  }
  if (state.settings.miloRouter.connectionStatus !== 'connected' || state.catalogStatus !== 'ready') {
    state.showToast(st('imageStudio.store.needConnectedModel'), 'info')
    return
  }
  const modelId = task.modelId ?? getImageModel(task.apiModel ?? '')?.id
  if (!modelId || !state.modelCatalog.some((model) => model.id === modelId)) {
    state.showToast(st('imageStudio.store.modelUnavailableReuse'), 'error')
    return
  }
  const retry = async () => {
    if (!isGallerySessionCurrent(session)) return
    await reuseConfig(task)
    if (!isGallerySessionCurrent(session)) return
    await submitTask()
  }
  if (task.execution === 'cloud' && (state.studioJobStatus[task.id] === 'unknown' || task.cloudJobStatus === 'unknown')) {
    state.setConfirmDialog({
      title: st('imageStudio.store.unknownRetryTitle'),
      message: st('imageStudio.store.unknownRetryMessage'),
      tone: 'warning',
      confirmText: st('imageStudio.store.unknownRetryConfirm'),
      action: retry,
    })
    return
  }
  await retry()
}

export async function removeTask(task: TaskRecord) {
  await removeMultipleTasks([task.id])
}

export async function removeMultipleTasks(taskIds: string[]) {
  const state = useStore.getState()
  const session = captureGallerySession()
  const ids = new Set(taskIds)
  const removed = state.tasks.filter((task) => ids.has(task.id))
  if (!removed.length) return
  if (removed.some((task) => task.status === 'running' || taskControllers.has(task.id))) {
    state.showToast(st('imageStudio.store.waitGeneration'), 'info')
    return
  }
  const deletedIds = new Set(removed.map((task) => task.id))
  const keys = [...deletedIds].map((id) => session.scope + ':' + id)
  keys.forEach((key) => deletingGalleryTasks.add(key))
  try {
    for (const task of removed) stopStudioJobPoller(task.id, session)
    await Promise.all(keys.map((key) => applyingStudioJobs.get(key)).filter((job): job is Promise<void> => Boolean(job)))
    // An already-started upload must finish or stop before DELETE, or its POST can resurrect the task.
    await Promise.all(keys.map((key) => syncingGalleryTasks.get(key)).filter((job): job is Promise<void> => Boolean(job)))
    if (!isGallerySessionCurrent(session)) return
    if (state.galleryInitialized && state.galleryUserId) {
      for (const task of removed) {
        if (!isGallerySessionCurrent(session)) return
        try {
          if (task.execution === 'cloud' && session.token) {
            try {
              await deleteStudioJob(task.id, session.token)
            } catch (error) {
              if (!(error instanceof GalleryApiError && error.status === 404)) throw error
              // An older archived task can outlive its execution record.
              await deleteGalleryTask(task.id)
            }
            stopStudioJobPoller(task.id, session)
            cloudCancellationRequested.delete(task.id)
          } else await deleteGalleryTask(task.id)
        } catch (error) {
          if (!(error instanceof GalleryApiError && error.status === 404)) throw error
        }
      }
    }
    if (!isGallerySessionCurrent(session)) return
    // A failed transaction must leave the visible history and image caches intact.
    await commitTaskDeletion([...deletedIds], [], session.scope)
    await Promise.all([...deletedIds].map((id) => clearTaskPendingSync(id, session.scope)))
    if (!isGallerySessionCurrent(session)) return
  useStore.setState((current) => {
    const tasks = current.tasks.filter((task) => !deletedIds.has(task.id))
    const referenced = collectReferencedImageIds(tasks, current.inputImages, current)
    const detailTask = tasks.find((task) => task.id === current.detailTaskId)
    const lightboxImageId = current.lightboxImageId && referenced.has(current.lightboxImageId)
      ? current.lightboxImageId
      : null
    return {
      tasks,
      gallerySyncStatus: Object.fromEntries(Object.entries(current.gallerySyncStatus).filter(([id]) => !deletedIds.has(id))),
      gallerySyncErrorCode: Object.fromEntries(Object.entries(current.gallerySyncErrorCode).filter(([id]) => !deletedIds.has(id))),
      studioJobStatus: Object.fromEntries(Object.entries(current.studioJobStatus).filter(([id]) => !deletedIds.has(id))),
      streamPreviews: Object.fromEntries(Object.entries(current.streamPreviews).filter(([taskId]) => !deletedIds.has(taskId))),
      latestSubmittedTaskId: current.latestSubmittedTaskId && deletedIds.has(current.latestSubmittedTaskId) ? null : current.latestSubmittedTaskId,
      detailTaskId: detailTask?.id ?? null,
      detailImageId: detailTask && current.detailImageId && collectTaskImageIds(detailTask).includes(current.detailImageId) ? current.detailImageId : null,
      lightboxImageId,
      lightboxImageList: lightboxImageId ? current.lightboxImageList.filter((id) => referenced.has(id)) : [],
    }
  })
  const candidates = new Set(removed.flatMap(collectTaskImageIds))
  await Promise.all([...candidates].map((id) => deleteImageIfUnreferenced(id, session)))
  void refreshGalleryStats()
  } finally {
    keys.forEach((key) => deletingGalleryTasks.delete(key))
  }
}

export async function clearFailedTasks(taskIds: string[]) {
  await removeMultipleTasks(taskIds.filter((id) => useStore.getState().tasks.find((task) => task.id === id)?.status === 'error'))
}

function canWriteStudioImage(session: ReturnType<typeof captureGallerySession>): boolean {
  if (!session.token) return true
  return Boolean(useStore.getState().galleryInitialized && session.userId
    && captureStudioIdentity().userId === session.userId
    && session.scope === 'milorouter-ai-image-user-' + session.userId)
}

export async function addImageFromFile(file: File) {
  const session = captureGallerySession()
  if (!canWriteStudioImage(session)) return
  if (!file.type.startsWith('image/')) throw new Error(st('imageStudio.store.pickImageFile'))
  const dataUrl = await fileToDataUrl(file)
  if (!isGallerySessionCurrent(session)) return
  const image = await storeImageWithSize(dataUrl, 'upload', session.scope)
  if (!isGallerySessionCurrent(session)) return
  useStore.getState().addInputImage({ id: image.id, dataUrl })
}

export async function createInputImageFromFile(file: File) {
  const session = captureGallerySession()
  if (!canWriteStudioImage(session)) return null
  if (!file.type.startsWith('image/')) return null
  const dataUrl = await fileToDataUrl(file)
  if (!isGallerySessionCurrent(session)) return null
  const image = await storeImageWithSize(dataUrl, 'upload', session.scope)
  if (!isGallerySessionCurrent(session)) return null
  return { id: image.id, dataUrl }
}

export async function addImageFromUrl(url: string) {
  const session = captureGallerySession()
  if (!canWriteStudioImage(session)) return
  const dataUrl = await blobToDataUrl(await imageSourceToBlob(url), 'image/png')
  if (!isGallerySessionCurrent(session)) return
  // A successful HTTP response can still contain HTML or invalid image bytes.
  // Decode before persistence so neither the draft nor its saved copy gets a broken image.
  const decoded = await loadImage(dataUrl).catch(() => {
    throw new Error(st('imageStudio.store.imageReadFailed'))
  })
  if (!decoded.naturalWidth || !decoded.naturalHeight) {
    throw new Error(st('imageStudio.store.imageReadFailed'))
  }
  if (!isGallerySessionCurrent(session)) return
  const image = await storeImageWithSize(dataUrl, 'upload', session.scope)
  if (!isGallerySessionCurrent(session)) return
  useStore.getState().addInputImage({ id: image.id, dataUrl })
}

export async function deleteImageIfUnreferenced(id: string, session = captureGallerySession()) {
  if (!isGallerySessionCurrent(session)) return
  const state = useStore.getState()
  if (collectReferencedImageIds(state.tasks, state.inputImages, state).has(id)) return
  await dbDeleteImage(id, session.scope)
  if (!isGallerySessionCurrent(session)) return
  deleteImageCacheEntry(id)
}

export async function clearData(options: { clearConfig?: boolean; clearTasks?: boolean }) {
  const state = useStore.getState()
  const session = captureGallerySession()
  if (options.clearTasks && (!state.galleryInitialized || !state.galleryUserId
    || !isVerifiedGalleryAuth(localStorage.getItem('auth_token') ?? ''))) {
    state.showToast(st('imageStudio.gallery.cacheClearBlocked'), 'info')
    return
  }
  if (submitInProgress > 0 || hasActiveDataOperations(state.tasks)) {
    state.showToast(st('imageStudio.store.waitGeneration'), 'info')
    return
  }
  if (options.clearTasks && cacheClearInProgress) return
  if (options.clearTasks) cacheClearInProgress = true
  try {
    if (options.clearTasks) {
      if (state.galleryInitialized && state.galleryUserId) {
        const pending = await getPendingSyncTaskIds(session.scope)
        if (!isGallerySessionCurrent(session)) return
        const syncing = [...syncingGalleryTasks.keys()].some((key) => key.startsWith(session.scope + ':'))
        if (pending.length || syncing || submitInProgress > 0 || hasActiveDataOperations(state.tasks)
          || state.inputImages.length || state.maskDraft || state.imageEditSession) {
          state.showToast(st('imageStudio.gallery.cacheClearBlocked'), 'info')
          return
        }
        await dbClearTasks(session.scope)
        await dbClearImages(session.scope)
        if (!isGallerySessionCurrent(session)) return
        await clearImageVariantCaches(session)
        if (!isGallerySessionCurrent(session)) return
        clearImageCaches()
        state.showToast(st('imageStudio.store.dataCleared'), 'success')
        return
      }
      await dbClearTasks(session.scope)
      await dbClearImages(session.scope)
      if (!isGallerySessionCurrent(session)) return
      await clearImageVariantCaches(session)
      if (!isGallerySessionCurrent(session)) return
      clearImageCaches()
      useStore.setState({
        tasks: [],
        streamPreviews: {},
        studioJobStatus: {},
        studioSubmissionStage: {},
        prompt: remapImageMentionsForOrder(state.prompt, state.inputImages, []),
        inputImages: [],
        maskDraft: null,
        maskEditorImageId: null,
        imageEditSession: null,
        detailTaskId: null,
        detailImageId: null,
        lightboxImageId: null,
        lightboxImageList: [],
      })
    }
    if (options.clearConfig) {
      if (!isGallerySessionCurrent(session)) return
      saveMiloRouterApiKey('', false)
      useStore.setState({
        settings: defaultSettings(),
        prompt: '',
        params: { ...DEFAULT_PARAMS },
        modelCatalog: [],
        catalogStatus: 'idle',
        catalogError: undefined,
        maskEditorImageId: null,
        imageEditSession: null,
      })
    }
    state.showToast(st('imageStudio.store.dataCleared'), 'success')
  } finally {
    if (options.clearTasks) cacheClearInProgress = false
  }
}

export async function exportData(options: { exportConfig?: boolean; exportTasks?: boolean }) {
  const state = useStore.getState()
  if (hasActiveDataOperations(state.tasks)) {
    state.showToast(st('imageStudio.store.waitBeforeBackup'), 'info')
    return
  }
  const userId = state.galleryUserId
  const exportSession = getBrowserNotificationSession()
  const exportToken = localStorage.getItem('auth_token')
  const assertExportSession = () => {
    if (useStore.getState().galleryUserId !== userId || getBrowserNotificationSession() !== exportSession
      || localStorage.getItem('auth_token') !== exportToken) {
      throw new Error('账号已切换，请重新导出')
    }
  }
  try {
    const tasksById = new Map((options.exportTasks ? state.tasks : []).map((task) => [task.id, task]))
    if (options.exportTasks && userId) {
      let cursor: string | null = null
      const seenCursors = new Set<string>()
      do {
        assertExportSession()
        const page = await listGalleryTasks(cursor, 100)
        assertExportSession()
        for (const item of page.items) {
          const task = normalizeTask(item)
          if (task) tasksById.set(task.id, task)
        }
        cursor = page.next_cursor
        if (cursor && seenCursors.has(cursor)) throw new Error('云端历史分页异常，请稍后重试')
        if (cursor) seenCursors.add(cursor)
      } while (cursor)
    }

    const tasks = [...tasksById.values()].sort((a, b) => b.createdAt - a.createdAt)
    assertExportSession()
    const localIds = options.exportTasks ? await getAllImageIds() : []
    assertExportSession()
    const imageIds = [...new Set([...localIds, ...tasks.flatMap(collectTaskImageIds)])]
    const remoteMeta = new Map<string, { bytes: number; width: number; height: number }>()
    const sizes: Array<{ id: string; bytes: number }> = []
    for (let index = 0; index < imageIds.length; index += 8) {
      assertExportSession()
      const batch = await Promise.all(imageIds.slice(index, index + 8).map(async (id) => {
        assertExportSession()
        const local = await getImage(id)
        assertExportSession()
        if (local) {
          const thumbnail = await getImageThumbnail(id)
          assertExportSession()
          return { id, bytes: getExportImageEstimatedBytes(local, thumbnail) }
        }
        const meta = await getGalleryAssetUrl(id, 'original')
        assertExportSession()
        remoteMeta.set(id, meta)
        return { id, bytes: meta.bytes + 1024 }
      }))
      assertExportSession()
      sizes.push(...batch)
    }

    const params = {
      options,
      exportedAt: Date.now(),
      settings: stripApiKeysFromSettings(state.settings),
      tasks,
    }
    const plan = getExportZipPlan(params, sizes, { partBytes: 64 * 1024 * 1024 })
    for (const [index, part] of plan.entries()) {
      assertExportSession()
      const images: StoredImage[] = []
      const thumbnails = new Map<string, StoredImageThumbnail>()
      for (const id of part.imageIds) {
        assertExportSession()
        const local = await getImage(id)
        assertExportSession()
        if (local) {
          images.push(local)
          const thumbnail = await getImageThumbnail(id)
          assertExportSession()
          if (thumbnail) thumbnails.set(id, thumbnail)
          continue
        }
        const meta = remoteMeta.get(id)
        const dataUrl = await fetchGalleryAssetDataUrl(id, 'original')
        assertExportSession()
        images.push({ id, dataUrl, width: meta?.width, height: meta?.height })
      }
      const result = await buildExportZip({
        ...params,
        tasks: part.tasks,
        images,
        thumbnailsByImageId: thumbnails,
        includeManifestData: part.includeBaseData,
        backupPart: plan.length > 1 ? { id: String(params.exportedAt), index: index + 1, total: plan.length } : undefined,
      })
      assertExportSession()
      const url = URL.createObjectURL(createExportBlob(result.bytes))
      const link = document.createElement('a')
      link.href = url
      link.download = 'milorouter-backup-' + (index + 1) + '.zip'
      link.click()
      URL.revokeObjectURL(url)
    }
    state.showToast(st('imageStudio.store.backupExported'), 'success')
  } catch (error) {
    if (useStore.getState().galleryUserId !== userId || getBrowserNotificationSession() !== exportSession
      || localStorage.getItem('auth_token') !== exportToken) return
    state.showToast(getUserFacingApiErrorMessage(error), 'error')
  }
}

export async function importData(file: File, options: { importConfig?: boolean; importTasks?: boolean }) {
  const state = useStore.getState()
  if (options.importTasks && (!state.galleryInitialized || !state.galleryUserId
    || !isVerifiedGalleryAuth(localStorage.getItem('auth_token') ?? ''))) {
    state.showToast(st('imageStudio.gallery.importFailed'), 'error')
    return
  }
  if (hasActiveDataOperations(state.tasks)) {
    state.showToast(st('imageStudio.store.waitBeforeRestore'), 'info')
    return
  }
  const importSession = captureGallerySession()
  const importUserId = state.galleryUserId
  const assertImportSession = () => {
    if (!isGallerySessionCurrent(importSession) || useStore.getState().galleryUserId !== importUserId) {
      throw new Error('账号已切换，请重新导入')
    }
  }
  try {
    const contents = await readExportZip(new Uint8Array(await file.arrayBuffer()))
    assertImportSession()
    const manifest = contents.manifest
    if (options.importConfig && manifest.settings) {
      const imported = normalizeSettings(manifest.settings)
      assertImportSession()
      useStore.setState((current) => ({
        settings: {
          ...imported,
          miloRouter: {
            ...imported.miloRouter,
            apiKey: current.settings.miloRouter.apiKey,
            connectionStatus: current.settings.miloRouter.connectionStatus,
            billing: current.settings.miloRouter.billing,
          },
        },
      }))
    }
    if (options.importTasks) {
      for (const [id, info] of Object.entries(manifest.imageFiles ?? {})) {
        assertImportSession()
        const dataUrl = readExportZipFileAsDataUrl(contents.files, info.path)
        if (!dataUrl) continue
        const existing = await getImage(id, importSession.scope)
        assertImportSession()
        if (existing && existing.dataUrl !== dataUrl) throw new Error('备份图片与当前账号已有图片冲突')
        if (!existing) await putImage({ id, dataUrl, createdAt: info.createdAt, source: info.source, width: info.width, height: info.height }, importSession.scope)
      }
      const importedTasks = (manifest.tasks ?? []).map(normalizeTask).filter((task): task is TaskRecord => Boolean(task))
      for (const task of importedTasks) {
        assertImportSession()
        if (importUserId) await putTaskWithPendingSync(task, importSession.scope)
        else await dbPutTask(task, importSession.scope)
      }
      assertImportSession()
      useStore.setState((current) => ({
        tasks: [...current.tasks.filter((task) => !importedTasks.some((item) => item.id === task.id)), ...importedTasks],
        gallerySyncStatus: importUserId
          ? { ...current.gallerySyncStatus, ...Object.fromEntries(importedTasks.map((task) => [task.id, 'pending' as const])) }
          : current.gallerySyncStatus,
      }))
      if (useStore.getState().galleryEnabled) void retryPendingGalleryTasks()
    }
    state.showToast(st('imageStudio.store.backupRestored'), 'success')
  } catch (error) {
    if (!isGallerySessionCurrent(importSession)) return
    console.error(error)
    state.showToast(st('imageStudio.store.backupInvalid'), 'error')
  }
}

export function taskMatchesFilterStatus(task: TaskRecord, filterStatus: AppState['filterStatus']) {
  return filterStatus === 'all' || task.status === filterStatus
}

export function taskMatchesSearchQuery(task: TaskRecord, query: string) {
  if (!query) return true
  const modelName = task.modelName || getImageModel(task.apiModel ?? '')?.name || st('imageStudio.store.legacyModel')
  const normalized = query.trim().toLowerCase()
  return stripImageMentionMarkers(task.prompt).toLowerCase().includes(normalized) || modelName.toLowerCase().includes(normalized)
}

export function getErrorToastMessage(message: string) {
  const text = message.trim()
  if (!text) return st('imageStudio.store.operationFailed')
  const firstLine = text.split(/\r?\n/)[0]?.trim() ?? ''
  return firstLine.length > ERROR_TOAST_MAX_LENGTH ? st('imageStudio.store.operationFailedDetail') : firstLine
}

function getToastMessage(message: string, type: ToastType) {
  return type === 'error' ? getErrorToastMessage(message) : message
}

function createId() {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

function normalizeSettings(value: unknown): AppSettings {
  const record = value && typeof value === 'object' ? value as Record<string, unknown> : {}
  const rawMiloRouter = record.miloRouter && typeof record.miloRouter === 'object'
    ? record.miloRouter as Record<string, unknown>
    : {}
  const stored = readStoredMiloRouterConfig()
  const apiKey = typeof rawMiloRouter.apiKey === 'string' && rawMiloRouter.apiKey.trim()
    ? rawMiloRouter.apiKey.trim()
    : stored.apiKey
  const rawSelectedModelId = typeof record.selectedModelId === 'string' && record.selectedModelId.trim()
    ? record.selectedModelId.trim()
    : GPT_IMAGE_2_MODEL_ID
  const selectedModelId = migrateLegacyImageModelId(rawSelectedModelId)
  return {
    miloRouter: createMiloRouterConfig({
      apiKey,
      rememberApiKey: rawMiloRouter.rememberApiKey === true || stored.rememberApiKey,
      connectionStatus: apiKey && rawMiloRouter.connectionStatus === 'connected' ? 'connected' : 'idle',
      billing: rawMiloRouter.billing && typeof rawMiloRouter.billing === 'object' ? rawMiloRouter.billing as AppSettings['miloRouter']['billing'] : undefined,
    }),
    selectedModelId,
    connectionOnboardingSeen: record.connectionOnboardingSeen === true,
    timeout: typeof record.timeout === 'number' && Number.isFinite(record.timeout)
      ? Math.min(3600, Math.max(30, record.timeout))
      : DEFAULT_API_TIMEOUT,
    clearInputAfterSubmit: record.clearInputAfterSubmit === true,
    persistInputOnRestart: record.persistInputOnRestart !== false,
    taskCompletionNotification: record.taskCompletionNotification === true,
    enterSubmit: record.enterSubmit === true,
  }
}

function normalizeTask(value: unknown): TaskRecord | null {
  if (!value || typeof value !== 'object') return null
  const record = value as Record<string, unknown>
  if (typeof record.id !== 'string' || typeof record.prompt !== 'string') return null
  const params = normalizeTaskParams(record.params)
  const status = record.status === 'running' || record.status === 'error' ? record.status : 'done'
  const inputImageIds = Array.isArray(record.inputImageIds) ? record.inputImageIds.map(String).filter(Boolean) : []
  const outputImages = Array.isArray(record.outputImages) ? record.outputImages.map(String).filter(Boolean) : []
  const rawModelId = typeof record.modelId === 'string'
    ? record.modelId
    : getImageModel(typeof record.apiModel === 'string' ? record.apiModel : '')?.id
  const modelId = rawModelId ? migrateLegacyImageModelId(rawModelId) : undefined
  const rawApiModel = typeof record.apiModel === 'string' ? record.apiModel : GPT_IMAGE_2_MODEL_ID
  const apiModel = migrateLegacyImageModelId(rawApiModel)
  return {
    id: record.id,
    execution: record.execution === 'cloud' ? 'cloud' : undefined,
    cloudCancelRequested: record.cloudCancelRequested === true,
    cloudJobStatus: record.cloudJobStatus === 'queued' || record.cloudJobStatus === 'generating' || record.cloudJobStatus === 'saving'
      || record.cloudJobStatus === 'completed' || record.cloudJobStatus === 'partial' || record.cloudJobStatus === 'failed'
      || record.cloudJobStatus === 'unknown' || record.cloudJobStatus === 'cancelled'
      ? record.cloudJobStatus : undefined,
    prompt: record.prompt,
    params,
    apiKind: record.apiKind === 'gemini' ? 'gemini' : 'openai',
    modelId,
    modelName: modelId ? getImageModel(modelId)?.name ?? (typeof record.modelName === 'string' ? record.modelName : undefined) : typeof record.modelName === 'string' ? record.modelName : undefined,
    apiModel,
    actualParams: isRecord(record.actualParams) ? record.actualParams as TaskRecord['actualParams'] : undefined,
    actualParamsByImage: isRecord(record.actualParamsByImage) ? record.actualParamsByImage as TaskRecord['actualParamsByImage'] : undefined,
    revisedPromptByImage: isRecord(record.revisedPromptByImage) ? record.revisedPromptByImage as TaskRecord['revisedPromptByImage'] : undefined,
    transparentOutput: record.transparentOutput === true,
    transparentPrompt: typeof record.transparentPrompt === 'string' ? record.transparentPrompt : undefined,
    transparentOriginalImages: Array.isArray(record.transparentOriginalImages) ? record.transparentOriginalImages.map(String) : undefined,
    inputImageIds,
    editMode: record.editMode === 'erase' ? 'erase' : record.editMode === 'region' ? 'region' : undefined,
    maskTargetImageId: typeof record.maskTargetImageId === 'string' ? record.maskTargetImageId : null,
    maskImageId: typeof record.maskImageId === 'string' ? record.maskImageId : null,
    outputImages,
    outputErrors: Array.isArray(record.outputErrors)
      ? record.outputErrors
          .filter((item): item is Record<string, unknown> => isRecord(item) && typeof item.requestIndex === 'number' && typeof item.error === 'string')
          .map((item) => ({ requestIndex: item.requestIndex as number, error: item.error as string }))
      : undefined,
    status,
    error: typeof record.error === 'string' ? record.error : null,
    createdAt: typeof record.createdAt === 'number' ? record.createdAt : Date.now(),
    finishedAt: typeof record.finishedAt === 'number' ? record.finishedAt : null,
    elapsed: typeof record.elapsed === 'number' ? record.elapsed : null,
  }
}

function normalizeTaskParams(value: unknown): TaskParams {
  const record = value && typeof value === 'object' ? value as Record<string, unknown> : {}
  const size = typeof record.size === 'string' ? record.size : DEFAULT_PARAMS.size
  return {
    resolution: record.resolution === 'auto' || record.resolution === '0.5K' || record.resolution === '1K' || record.resolution === '2K' || record.resolution === '4K'
      ? record.resolution
      : getModelResolution(null, size),
    size,
    aspect_ratio: typeof record.aspect_ratio === 'string' ? record.aspect_ratio : undefined,
    complexity: record.complexity === 'low' || record.complexity === 'medium' || record.complexity === 'high' ? record.complexity : undefined,
    quality: record.quality === 'low' || record.quality === 'medium' || record.quality === 'high' || record.quality === 'xhigh' || record.quality === 'max' ? record.quality : DEFAULT_PARAMS.quality,
    output_format: record.output_format === 'jpeg' || record.output_format === 'webp' ? record.output_format : DEFAULT_PARAMS.output_format,
    output_compression: typeof record.output_compression === 'number' ? record.output_compression : null,
    moderation: record.moderation === 'low' ? 'low' : DEFAULT_PARAMS.moderation,
    n: typeof record.n === 'number' && Number.isFinite(record.n) ? Math.max(1, Math.min(MAX_OUTPUT_IMAGES, Math.round(record.n))) : DEFAULT_PARAMS.n,
    transparent_output: record.transparent_output === true,
    partial_images: record.partial_images === 1 || record.partial_images === 2 || record.partial_images === 3 ? record.partial_images : 0,
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function collectTaskImageIds(task: TaskRecord) {
  return [
    ...task.inputImageIds,
    ...(task.maskTargetImageId ? [task.maskTargetImageId] : []),
    ...(task.maskImageId ? [task.maskImageId] : []),
    ...task.outputImages,
    ...(task.transparentOriginalImages ?? []),
  ]
}

function collectReferencedImageIds(
  tasks: TaskRecord[],
  inputImages: InputImage[] = [],
  draft?: Pick<AppState, 'maskDraft' | 'maskEditorImageId' | 'imageEditSession'>,
) {
  return new Set([
    ...inputImages.map((image) => image.id),
    ...tasks.flatMap(collectTaskImageIds),
    ...(draft?.maskDraft ? [draft.maskDraft.targetImageId] : []),
    ...(draft?.maskEditorImageId ? [draft.maskEditorImageId] : []),
    ...(draft?.imageEditSession ? [
      draft.imageEditSession.targetImageId,
      ...draft.imageEditSession.inputImages.map((image) => image.id),
      ...(draft.imageEditSession.maskDraft ? [draft.imageEditSession.maskDraft.targetImageId] : []),
    ] : []),
  ])
}

/**
 * 把「拉取模型列表」的错误翻译成用户能直接照做的提示。
 *
 * 之前这里无条件 st('catalogFailed')，把余额不足、Key 失效、超时全部抹平成
 * 「加载失败，请稍后重试」——用户重试多少次都没用，因为问题不在重试。
 */
function describeCatalogError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error ?? '')
  // 优先按类型判断：ApiInsufficientBalanceError 的 message 已经是本地化文案，
  // 不再包含 INSUFFICIENT_BALANCE 字样，靠字符串匹配会漏掉。
  if (error instanceof ApiInsufficientBalanceError) {
    return st('imageStudio.errors.insufficientBalance')
  }
  if (/INSUFFICIENT_BALANCE|insufficient\s+(account\s+)?balance/i.test(message)) {
    return st('imageStudio.errors.insufficientBalance')
  }
  if (/USAGE_LIMIT_EXCEEDED/i.test(message)) {
    return st('imageStudio.errors.rateLimit')
  }
  if (error instanceof ApiAuthError || /API Key|\b401\b/i.test(message)) {
    return st('imageStudio.errors.auth')
  }
  if (/timeout|timed out|超时/i.test(message)) {
    return st('imageStudio.errors.timeout')
  }
  if (/\b5\d\d\b|暂时不可用|service unavailable/i.test(message)) {
    return st('imageStudio.errors.unavailable')
  }
  return st('imageStudio.store.catalogFailed')
}
