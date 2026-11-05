import type {
  AppSettings,
  ImageEditMode,
  InputImage,
  MaskDraft,
  MiloRouterConfig,
  StoredImageThumbnail,
  TaskParams,
  TaskRecord,
  TaskStatus,
} from '../types'
import { DEFAULT_PARAMS } from '../types'
import i18n from '@/i18n'
import { callImageApi } from '../lib/api'
import { fetchMiloRouterImageModels } from '../lib/miloRouterApi'
import { createMiloRouterConfig, readStoredMiloRouterConfig, saveMiloRouterApiKey, stripApiKeysFromSettings } from '../lib/miloRouterConfig'
import { getImageModel, getModelResolution, getRequestModelId, GPT_IMAGE_2_MODEL_ID, migrateLegacyImageModelId, type ImageModelDefinition } from '../lib/modelRegistry'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'
import { normalizeParamsForSettings, MAX_OUTPUT_IMAGES, isRequestedImageSizeAvailable } from '../lib/paramCompatibility'
import {
  clearTasks as dbClearTasks,
  commitTaskDeletion,
  deleteImage as dbDeleteImage,
  getAllImages,
  getAllTasks,
  getImageThumbnail,
  putImage,
  putTask as dbPutTask,
  clearImages as dbClearImages,
  storeImage,
  storeImageWithSize,
} from '../lib/db'
import { cacheImage, clearImageCaches, deleteImageCacheEntry, ensureImageCached } from '../lib/imageCache'
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
import { showBrowserNotification } from '../lib/browserNotification'
import { ApiAuthError, ApiInsufficientBalanceError, getUserFacingApiErrorMessage } from '../lib/apiErrors'
import { remapImageMentionsForOrder, replaceImageMentionsForApi, stripImageMentionMarkers } from '../lib/promptImageMentions'
import { GPT_IMAGE_MIN_PIXELS, normalizeImageSize } from '../lib/size'
const MAX_INPUT_IMAGES = 16
const DEFAULT_API_TIMEOUT = 600
const ERROR_TOAST_MAX_LENGTH = 80
const taskControllers = new Map<string, AbortController>()
const stoppedTaskIds = new Set<string>()

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
  streamPreviews: Record<string, { image: string; partialIndex: number; requestIndex?: number }>
  latestSubmittedTaskId: string | null
  setTasks: (tasks: TaskRecord[]) => void

  searchQuery: string
  setSearchQuery: (query: string) => void
  filterStatus: TaskStatus | 'all'
  setFilterStatus: (status: TaskStatus | 'all') => void

  detailTaskId: string | null
  setDetailTaskId: (id: string | null) => void
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
            ...(keyChanged ? { modelCatalog: [], catalogStatus: 'idle' as const, catalogError: undefined } : {}),
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
          const request = ++catalogRequest
          set({ catalogStatus: 'loading', catalogError: undefined })
          try {
            const modelCatalog = await fetchMiloRouterImageModels(apiKey)
            if (request !== catalogRequest || get().settings.miloRouter.apiKey.trim() !== apiKey) return false
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
            if (request !== catalogRequest || get().settings.miloRouter.apiKey.trim() !== apiKey) return false
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
        streamPreviews: {},
        latestSubmittedTaskId: null,
        setTasks: (tasks) => set({ tasks }),

        searchQuery: '',
        setSearchQuery: (searchQuery) => set({ searchQuery }),
        filterStatus: 'all',
        setFilterStatus: (filterStatus) => set({ filterStatus }),

        detailTaskId: null,
        setDetailTaskId: (detailTaskId) => set({ detailTaskId }),
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

export function initStore(): Promise<void> {
  if (initPromise) return initPromise

  initPromise = (async () => {
    try {
      const loadedTasks = (await getAllTasks())
        .map(normalizeTask)
        .filter((task): task is TaskRecord => Boolean(task))

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
        task.status === 'running'
          ? { ...task, ...createTaskErrorPatch(task, st('imageStudio.store.interrupted'), Date.now()) }
          : task,
      )
      await Promise.all(
        interrupted
          .filter((task) => task.status === 'error')
          .map((task) => dbPutTask(task)),
      )

      // 参考图只存了元数据，需要把 dataUrl 从缓存补回来
      const inputImages = await Promise.all(
        current.inputImages.map(async (image) => ({
          id: image.id,
          dataUrl: image.dataUrl || (await ensureImageCached(image.id)) || '',
        })),
      )
      const usableInputImages = inputImages.filter((image) => image.dataUrl)

      useStore.setState({
        tasks: interrupted,
        prompt: remapImageMentionsForOrder(
          current.prompt,
          current.inputImages,
          usableInputImages,
        ),
        inputImages: usableInputImages,
        params: normalizeParamsForSettings(current.params, current.settings),
      })
    } catch (error) {
      console.warn('初始化本地数据失败', error)
      useStore.getState().showToast(st('imageStudio.store.localLoadFailed'), 'error')
    }
  })()

  return initPromise
}

export async function submitTask() {
  const state = useStore.getState()
  const prompt = state.prompt.trim()
  if (!prompt) {
    state.showToast(st('imageStudio.store.promptRequired'), 'error')
    return
  }
  const apiKey = state.settings.miloRouter.apiKey.trim()
  if (!apiKey || state.settings.miloRouter.connectionStatus !== 'connected') {
    // 密钥由系统下发，用户无法也不该手填；这里只提示去哪恢复。
    state.showToast(st('imageStudio.store.keyNotReady'), 'error')
    state.setShowSettings(true)
    return
  }
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
  if (!isRequestedImageSizeAvailable(model, state.params)) {
    state.showToast(st('imageStudio.store.channelSizeUnavailable'), 'error')
    return
  }
  const params = normalizeParamsForSettings(state.params, state.settings, state.modelCatalog)
  const capabilities = getImageModelCapabilities(modelId, state.modelCatalog)
  const requestModelId = getRequestModelId(model, params.size, params.resolution)
  if (!requestModelId) {
    state.showToast(st('imageStudio.store.resolutionUnavailable'), 'error')
    return
  }
  const inputImages = state.inputImages
  if (inputImages.length > model.capabilities.referenceImages) {
    state.showToast(`当前模型最多支持 ${model.capabilities.referenceImages} 张参考图`, 'error')
    return
  }
  const maskDraft = capabilities.mask ? state.maskDraft : null
  if (maskDraft && !inputImages.some((image) => image.id === maskDraft.targetImageId)) {
    state.clearMaskDraft()
    state.showToast(st('imageStudio.store.maskTargetMissing'), 'error')
    return
  }

  const orderedInputImages = maskDraft
    ? orderInputImagesForMask(inputImages, maskDraft.targetImageId)
    : inputImages
  const inputImageDataUrls = await Promise.all(orderedInputImages.map(async (image) => image.dataUrl || await ensureImageCached(image.id) || ''))
  if (inputImageDataUrls.some((url) => !url)) {
    state.showToast(st('imageStudio.store.referenceReadFailed'), 'error')
    return
  }
  let requestInputImageDataUrls = inputImageDataUrls
  let maskImageId: string | null = null
  let maskTargetSize: string | null = null
  if (maskDraft) {
    try {
      await validateMaskMatchesImage(maskDraft.maskDataUrl, inputImageDataUrls[0])
      const sourceImage = await loadImage(inputImageDataUrls[0])
      maskTargetSize = model.capabilities.customSize
        ? normalizeImageSize(`${sourceImage.naturalWidth}x${sourceImage.naturalHeight}`, { minPixels: GPT_IMAGE_MIN_PIXELS })
        : null
      requestInputImageDataUrls = [
        await createMaskedInputImage(inputImageDataUrls[0], maskDraft.maskDataUrl),
        ...inputImageDataUrls.slice(1),
      ]
      maskImageId = await storeImage(maskDraft.maskDataUrl, 'mask')
      cacheImage(maskImageId, maskDraft.maskDataUrl)
    } catch (error) {
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
  useStore.setState((current) => ({ tasks: [...current.tasks, task], latestSubmittedTaskId: task.id }))
  await dbPutTask(task)

  const controller = new AbortController()
  taskControllers.set(task.id, controller)
  try {
    const result = await callImageApi({
      settings: state.settings,
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
        return active ? { streamPreviews: { ...current.streamPreviews, [task.id]: partial } } : current
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
        const original = await storeImageWithSize(image, 'generated')
        originalImages.push(original.id)
        const processed = await removeKeyedBackgroundFromDataUrl(
          image,
          undefined,
          requestParams.output_format === 'webp' ? 'webp' : 'png',
          requestParams.output_compression,
        )
        const output = await storeImageWithSize(processed, 'generated')
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
        const output = await storeImageWithSize(finalImage, 'generated')
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
    const taskList = useStore.getState().tasks.map((item) => item.id === task.id ? finished : item)
    useStore.setState((current) => {
      const { [task.id]: _, ...streamPreviews } = current.streamPreviews
      return { tasks: taskList, streamPreviews }
    })
    await dbPutTask(finished)
    state.showToast(st('imageStudio.store.taskDone'), 'success')
    if (state.settings.taskCompletionNotification) showBrowserNotification(st('imageStudio.store.notificationTitle'), { body: prompt })
    if (state.settings.clearInputAfterSubmit) {
      useStore.setState({ prompt: '', inputImages: [], maskDraft: null })
    }
  } catch (error) {
    if (stoppedTaskIds.has(task.id)) return
    const message = getUserFacingApiErrorMessage(error)
    const failed = { ...task, ...createTaskErrorPatch(task, message, Date.now()) }
    useStore.setState((current) => {
      const { [task.id]: _, ...streamPreviews } = current.streamPreviews
      return { tasks: current.tasks.map((item) => item.id === task.id ? failed : item), streamPreviews }
    })
    await dbPutTask(failed)
    state.showToast(message, 'error')
  } finally {
    useStore.setState((current) => {
      if (!current.streamPreviews[task.id]) return current
      const { [task.id]: _, ...streamPreviews } = current.streamPreviews
      return { streamPreviews }
    })
    taskControllers.delete(task.id)
    stoppedTaskIds.delete(task.id)
  }
}

export function stopTaskGeneration(taskId: string) {
  const controller = taskControllers.get(taskId)
  if (!controller) return
  stoppedTaskIds.add(taskId)
  controller.abort()
  const task = useStore.getState().tasks.find((item) => item.id === taskId)
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
  const images = await Promise.all(task.inputImageIds.map(async (id) => ({
    id,
    dataUrl: await ensureImageCached(id) ?? '',
  })))
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
  await reuseConfig(task)
  await submitTask()
}

export async function removeTask(task: TaskRecord) {
  await removeMultipleTasks([task.id])
}

export async function removeMultipleTasks(taskIds: string[]) {
  const state = useStore.getState()
  const ids = new Set(taskIds)
  const remaining = state.tasks.filter((task) => !ids.has(task.id))
  const removed = state.tasks.filter((task) => ids.has(task.id))
  const referenced = collectReferencedImageIds(remaining)
  const deleteIds = new Set<string>()
  for (const task of removed) {
    for (const id of collectTaskImageIds(task)) if (!referenced.has(id)) deleteIds.add(id)
  }
  useStore.setState({
    tasks: remaining,
    streamPreviews: Object.fromEntries(Object.entries(state.streamPreviews).filter(([taskId]) => !ids.has(taskId))),
    detailTaskId: state.detailTaskId && ids.has(state.detailTaskId) ? null : state.detailTaskId,
  })
  await commitTaskDeletion([...ids], [])
  await Promise.all([...deleteIds].map(async (id) => {
    await dbDeleteImage(id)
    deleteImageCacheEntry(id)
  }))
}

export async function clearFailedTasks(taskIds: string[]) {
  await removeMultipleTasks(taskIds.filter((id) => useStore.getState().tasks.find((task) => task.id === id)?.status === 'error'))
}

export async function addImageFromFile(file: File) {
  if (!file.type.startsWith('image/')) throw new Error(st('imageStudio.store.pickImageFile'))
  const dataUrl = await fileToDataUrl(file)
  const image = await storeImageWithSize(dataUrl, 'upload')
  useStore.getState().addInputImage({ id: image.id, dataUrl })
}

export async function createInputImageFromFile(file: File) {
  if (!file.type.startsWith('image/')) return null
  const dataUrl = await fileToDataUrl(file)
  const image = await storeImageWithSize(dataUrl, 'upload')
  return { id: image.id, dataUrl }
}

export async function addImageFromUrl(url: string) {
  const dataUrl = await blobToDataUrl(await imageSourceToBlob(url), 'image/png')
  // A successful HTTP response can still contain HTML or invalid image bytes.
  // Decode before persistence so neither the draft nor its saved copy gets a broken image.
  const decoded = await loadImage(dataUrl).catch(() => {
    throw new Error(st('imageStudio.store.imageReadFailed'))
  })
  if (!decoded.naturalWidth || !decoded.naturalHeight) {
    throw new Error(st('imageStudio.store.imageReadFailed'))
  }
  const image = await storeImageWithSize(dataUrl, 'upload')
  useStore.getState().addInputImage({ id: image.id, dataUrl })
}

export async function deleteImageIfUnreferenced(id: string) {
  const state = useStore.getState()
  if (collectReferencedImageIds(state.tasks, state.inputImages).has(id)) return
  await dbDeleteImage(id)
  deleteImageCacheEntry(id)
}

export async function clearData(options: { clearConfig?: boolean; clearTasks?: boolean }) {
  const state = useStore.getState()
  if (hasActiveDataOperations(state.tasks)) {
    state.showToast(st('imageStudio.store.waitGeneration'), 'info')
    return
  }
  if (options.clearTasks) {
    await dbClearTasks()
    await dbClearImages()
    clearImageCaches()
    useStore.setState({
      tasks: [],
      streamPreviews: {},
      prompt: remapImageMentionsForOrder(state.prompt, state.inputImages, []),
      inputImages: [],
      maskDraft: null,
      maskEditorImageId: null,
      imageEditSession: null,
      detailTaskId: null,
      lightboxImageId: null,
      lightboxImageList: [],
    })
  }
  if (options.clearConfig) {
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
}

export async function exportData(options: { exportConfig?: boolean; exportTasks?: boolean }) {
  const state = useStore.getState()
  if (hasActiveDataOperations(state.tasks)) {
    state.showToast(st('imageStudio.store.waitBeforeBackup'), 'info')
    return
  }
  const images = options.exportTasks ? await getAllImages() : []
  const thumbnails = new Map<string, StoredImageThumbnail>()
  await Promise.all(images.map(async (image) => {
    const thumbnail = await getImageThumbnail(image.id)
    if (thumbnail) thumbnails.set(image.id, thumbnail)
  }))
  const params = {
    options,
    exportedAt: Date.now(),
    settings: stripApiKeysFromSettings(state.settings),
    tasks: options.exportTasks ? state.tasks : [],
  }
  const plan = getExportZipPlan(params, images.map((image) => ({
    id: image.id,
    bytes: getExportImageEstimatedBytes(image, thumbnails.get(image.id)),
  })))
  for (const [index, part] of plan.entries()) {
    const result = await buildExportZip({
      ...params,
      tasks: part.tasks,
      images: images.filter((image) => part.imageIds.includes(image.id)),
      thumbnailsByImageId: thumbnails,
      includeManifestData: part.includeBaseData,
      backupPart: plan.length > 1 ? { id: String(params.exportedAt), index: index + 1, total: plan.length } : undefined,
    })
    const url = URL.createObjectURL(createExportBlob(result.bytes))
    const link = document.createElement('a')
    link.href = url
    link.download = `milorouter-backup-${index + 1}.zip`
    link.click()
    URL.revokeObjectURL(url)
  }
  state.showToast(st('imageStudio.store.backupExported'), 'success')
}

export async function importData(file: File, options: { importConfig?: boolean; importTasks?: boolean }) {
  const state = useStore.getState()
  if (hasActiveDataOperations(state.tasks)) {
    state.showToast(st('imageStudio.store.waitBeforeRestore'), 'info')
    return
  }
  try {
    const contents = await readExportZip(new Uint8Array(await file.arrayBuffer()))
    const manifest = contents.manifest
    if (options.importConfig && manifest.settings) {
      const imported = normalizeSettings(manifest.settings)
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
    if (options.importTasks && manifest.tasks) {
      for (const [id, info] of Object.entries(manifest.imageFiles ?? {})) {
        const dataUrl = readExportZipFileAsDataUrl(contents.files, info.path)
        if (dataUrl) await putImage({ id, dataUrl, createdAt: info.createdAt, source: info.source, width: info.width, height: info.height })
      }
      const importedTasks = manifest.tasks.map(normalizeTask).filter((task): task is TaskRecord => Boolean(task))
      await Promise.all(importedTasks.map((task) => dbPutTask(task)))
      const mergedTasks = [...state.tasks.filter((task) => !importedTasks.some((item) => item.id === task.id)), ...importedTasks]
      useStore.setState({ tasks: mergedTasks })
    }
    state.showToast(st('imageStudio.store.backupRestored'), 'success')
  } catch (error) {
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
    ...(task.maskImageId ? [task.maskImageId] : []),
    ...task.outputImages,
    ...(task.transparentOriginalImages ?? []),
  ]
}

function collectReferencedImageIds(tasks: TaskRecord[], inputImages: InputImage[] = []) {
  return new Set([
    ...inputImages.map((image) => image.id),
    ...tasks.flatMap(collectTaskImageIds),
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
