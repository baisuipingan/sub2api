<template>
  <div
    v-if="imageId && imageEditSession"
    data-no-drag-select
    role="dialog"
    aria-modal="true"
    aria-labelledby="image-edit-title"
    class="fixed inset-0 z-[80] flex flex-col bg-gray-50 dark:bg-dark-900"
  >
    <StudioImageEditHeader
      :busy="isSaving"
      :has-saved-mask="Boolean(imageEditSession.maskDraft)"
      @close="close"
      @remove-mask="handleRemoveMask"
    />

    <!-- 画布工作区 -->
    <div
      ref="stageRef"
      class="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-gray-100/50 p-0 dark:bg-black/50 sm:p-6"
      style="container-type: size"
    >
      <div
        v-if="isLoading"
        class="absolute inset-0 z-20 flex items-center justify-center gap-3 bg-white/50 text-sm text-gray-500 backdrop-blur-sm dark:bg-dark-900/50 dark:text-dark-300"
      >
        <LoadingSpinner size="sm" />
        {{ t('imageStudio.mask.loading') }}
      </div>

      <div
        ref="baseFrameRef"
        class="relative max-h-full max-w-full touch-none shadow-inner ring-black/5 dark:bg-black/50 dark:ring-white/5 sm:rounded-xl sm:ring-1"
        :style="{
          aspectRatio: size ? `${size.width} / ${size.height}` : '1 / 1',
          width: size ? `min(100%, 100cqh * ${size.width / size.height})` : '520px',
          maxHeight: '100%',
        }"
        @wheel="handleWheel"
      >
        <div
          class="absolute inset-0 will-change-transform"
          :style="{
            transform: `matrix(${viewTransform.scale}, 0, 0, ${viewTransform.scale}, ${viewTransform.x}, ${viewTransform.y})`,
            transformOrigin: '0 0',
          }"
        >
          <canvas ref="imageCanvasRef" class="absolute inset-0 h-full w-full" />
          <canvas
            ref="previewCanvasRef"
            class="pointer-events-none absolute inset-0 h-full w-full"
            :class="isMaskMode ? '' : 'hidden'"
          />
          <canvas
            ref="maskCanvasRef"
            class="absolute inset-0 h-full w-full touch-none select-none opacity-0"
            :class="isMaskMode ? '' : 'pointer-events-none'"
            :style="{ cursor: canvasCursor }"
            @pointerdown="handlePointerDown"
            @pointermove="handlePointerMove"
            @pointerup="finishStroke"
            @pointercancel="finishStroke"
            @lostpointercapture="finishStroke"
            @pointerleave="handlePointerLeave"
          />
        </div>
      </div>

      <canvas ref="cursorCanvasRef" class="pointer-events-none absolute inset-0 h-full w-full" />

      <StudioMaskToolControls
        v-if="isMaskMode"
        :tool="tool"
        :brush-size="brushSize"
        :show-brush-controls="showBrushControls"
        :slider-anchor="sliderAnchor"
        :ready="isReady"
        :busy="isSaving"
        :can-undo="canUndo"
        :can-redo="canRedo"
        :can-reset-view="isReady && !isSaving && isZoomed"
        @tool-change="tool = $event"
        @toggle-brush-controls="toggleBrushControls"
        @brush-size-change="onBrushSizeChange"
        @undo="handleUndo"
        @redo="handleRedo"
        @reset-view="resetViewTransform"
        @clear="handleClear"
      />
    </div>

    <StudioImageEditPromptBar
      :mode="editMode"
      :prompt="editPrompt"
      :params="editParams"
      :models="editableModels"
      :selected-model-id="selectedModelId"
      :busy="isSaving"
      :can-use-mask="supportsMask"
      :can-remove-background="supportsTransparentOutput"
      @mode-change="handleModeChange"
      @prompt-change="editPrompt = $event"
      @model-change="handleModelChange"
      @params-change="editParams = $event"
      @save="handleSave(false)"
      @generate="handleSave(true)"
    />
  </div>
</template>

<script setup lang="ts">
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioImageEditHeader from './imageEdit/StudioImageEditHeader.vue'
import StudioImageEditPromptBar from './imageEdit/StudioImageEditPromptBar.vue'
import StudioMaskToolControls, {
  type MaskSliderAnchor,
  type MaskTool,
} from './imageEdit/StudioMaskToolControls.vue'
import type { ImageEditMode } from '../types'
import { storeState as state, submitTask } from '../stores/store'
import { loadImage } from '../lib/canvasImage'
import { ensureImageCached } from '../lib/imageCache'
import { prepareMaskTargetDataUrl } from '../lib/maskPreprocess'
import {
  createOriginalSizeMaskDataUrl,
  drawMaskImageToCanvas,
  fillWhiteMask,
  hasEditableMaskPixels,
} from '../lib/maskCanvas'
import { getImageModel } from '../lib/modelRegistry'
import { normalizeParamsForSettings } from '../lib/paramCompatibility'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'
import {
  clampViewTransform,
  clientPointToCanvasPoint,
  getComfortableInitialTransform,
  getPinchTransform,
  zoomAtPoint,
  type Point,
  type ViewTransform,
} from '../lib/viewportTransform'

interface CanvasSize {
  width: number
  height: number
}
interface PinchGesture {
  startTransform: ViewTransform
  startCentroid: Point
  startDistance: number
}
interface PanGesture {
  pointerId: number
  startPoint: Point
  startTransform: ViewTransform
}

const { t } = useI18n()

const DEFAULT_VIEW_TRANSFORM: ViewTransform = { scale: 1, x: 0, y: 0 }
const MAX_HISTORY_STEPS = 12

/* ── 依赖 store ─────────────────────────────────────── */
const imageId = computed(() => state.maskEditorImageId)
const imageEditSession = computed(() => state.imageEditSession)
const modelCatalog = computed(() => state.modelCatalog)

/* ── DOM 引用 ───────────────────────────────────────── */
const imageCanvasRef = ref<HTMLCanvasElement | null>(null)
const previewCanvasRef = ref<HTMLCanvasElement | null>(null)
const maskCanvasRef = ref<HTMLCanvasElement | null>(null)
const cursorCanvasRef = ref<HTMLCanvasElement | null>(null)
const stageRef = ref<HTMLDivElement | null>(null)
const baseFrameRef = ref<HTMLDivElement | null>(null)

/* ── 非响应式的手势/历史状态（用普通变量避免无谓渲染） ── */
let activePointerId: number | null = null
let lastPoint: Point | null = null
const pointerPositions = new Map<number, Point>()
let pinchGesture: PinchGesture | null = null
let panGesture: PanGesture | null = null
let undoStack: ImageData[] = []
let redoStack: ImageData[] = []
let previewFrame: number | null = null
let saveToken = 0
let sessionId = 0
let activeSessionId = 0
let viewTransformState: ViewTransform = { ...DEFAULT_VIEW_TRANSFORM }

/* ── 响应式 UI 状态 ─────────────────────────────────── */
const sourceDataUrl = ref('')
const size = ref<CanvasSize | null>(null)
const originalSize = ref<CanvasSize | null>(null)
const editMode = ref<ImageEditMode>('region')
const editPrompt = ref('')
const editParams = ref({ ...state.params })
const selectedModelId = ref(state.settings.selectedModelId)
const tool = ref<MaskTool>('brush')
const brushSize = ref(64)
const showBrushControls = ref(false)
const viewTransform = ref<ViewTransform>({ ...DEFAULT_VIEW_TRANSFORM })
const isLoading = ref(false)
const isSaving = ref(false)
const historyState = ref({ undo: 0, redo: 0 })
const hoverPoint = ref<Point | null>(null)
const isPointerOverCanvas = ref(false)
const isAltKeyPressed = ref(false)
const isPanning = ref(false)
const sliderAnchor = ref<MaskSliderAnchor | null>(null)

/* ── 派生状态 ───────────────────────────────────────── */
const editReferenceCount = computed(() => imageEditSession.value?.inputImages.length ?? 0)

const editableModels = computed(() =>
  modelCatalog.value.filter(
    (model) =>
      model.capabilities.referenceImages >= editReferenceCount.value &&
      (model.capabilities.mask || model.capabilities.transparentOutput),
  ),
)

const selectedModel = computed(() => getImageModel(selectedModelId.value, modelCatalog.value))
const supportsMask = computed(() => Boolean(selectedModel.value?.capabilities.mask))
const supportsTransparentOutput = computed(() =>
  Boolean(selectedModel.value?.capabilities.transparentOutput),
)

const isReady = computed(() => Boolean(sourceDataUrl.value && size.value && !isLoading.value))
const isMaskMode = computed(() => editMode.value === 'region' || editMode.value === 'erase')
const canUndo = computed(() => historyState.value.undo > 0 && isReady.value && !isSaving.value)
const canRedo = computed(() => historyState.value.redo > 0 && isReady.value && !isSaving.value)

const isZoomed = computed(
  () =>
    viewTransform.value.scale > 1.01 ||
    Math.abs(viewTransform.value.x) > 1 ||
    Math.abs(viewTransform.value.y) > 1,
)

const canvasCursor = computed(() => {
  if (isPanning.value) return 'grabbing'
  if (isAltKeyPressed.value) return 'grab'
  if (hoverPoint.value) return 'none'
  return 'crosshair'
})

/* ── 关闭 / 遮罩移除 ────────────────────────────────── */
function close() {
  if (isSaving.value) return
  state.setMaskEditorImageId(null)
}

useCloseOnEscape(() => Boolean(imageId.value && imageEditSession.value), close)
usePreventBackgroundScroll(() => Boolean(imageId.value && imageEditSession.value))

function handleRemoveMask() {
  state.setConfirmDialog({
    title: t('imageStudio.mask.removeTitle'),
    message: t('imageStudio.mask.removeMessage'),
    tone: 'danger',
    confirmText: t('imageStudio.mask.removeConfirm'),
    cancelText: t('imageStudio.common.cancel'),
    action: () => {
      if (!imageEditSession.value) return
      state.commitImageEditSession({
        mode: 'full',
        prompt: editPrompt.value,
        params: editParams.value,
        selectedModelId: selectedModelId.value,
        maskDataUrl: null,
      })
      state.showToast(t('imageStudio.mask.removed'), 'success')
    },
  })
}

/* ── 视图变换 ───────────────────────────────────────── */
function commitViewTransform(next: ViewTransform) {
  const frame = baseFrameRef.value
  const clamped = frame
    ? clampViewTransform(next, { width: frame.clientWidth, height: frame.clientHeight })
    : next
  viewTransformState = clamped
  viewTransform.value = clamped
}

function resetViewTransform() {
  const frame = baseFrameRef.value
  const stage = stageRef.value
  const isCompact = window.matchMedia('(max-width: 1023px)').matches
  if (!frame || !stage) {
    commitViewTransform({ ...DEFAULT_VIEW_TRANSFORM })
    return
  }

  commitViewTransform(
    getComfortableInitialTransform(
      { width: frame.clientWidth, height: frame.clientHeight },
      { width: stage.clientWidth, height: stage.clientHeight },
      isCompact,
    ),
  )
}

/* ── 手势 ───────────────────────────────────────────── */
function getCanvasPoint(canvas: HTMLCanvasElement, event: PointerEvent): Point {
  return clientPointToCanvasPoint(
    canvas.getBoundingClientRect(),
    { x: event.clientX, y: event.clientY },
    { width: canvas.width, height: canvas.height },
  )
}

function distance(a: Point, b: Point) {
  return Math.hypot(a.x - b.x, a.y - b.y)
}

function centroid(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
}

function firstTwoPointers(): [Point, Point] | null {
  const values = Array.from(pointerPositions.values())
  return values.length >= 2 ? [values[0], values[1]] : null
}

function beginPinchGesture() {
  const pointers = firstTwoPointers()
  const frame = baseFrameRef.value
  if (!pointers || !frame) return

  const rect = frame.getBoundingClientRect()
  const start = centroid(pointers[0], pointers[1])
  pinchGesture = {
    startTransform: viewTransformState,
    startCentroid: { x: start.x - rect.left, y: start.y - rect.top },
    startDistance: distance(pointers[0], pointers[1]),
  }
}

function updatePinchGesture() {
  const pointers = firstTwoPointers()
  const gesture = pinchGesture
  const frame = baseFrameRef.value
  if (!pointers || !gesture || !frame) return

  const rect = frame.getBoundingClientRect()
  const next = centroid(pointers[0], pointers[1])
  commitViewTransform(
    getPinchTransform({
      startTransform: gesture.startTransform,
      startCentroid: gesture.startCentroid,
      nextCentroid: { x: next.x - rect.left, y: next.y - rect.top },
      startDistance: gesture.startDistance,
      nextDistance: distance(pointers[0], pointers[1]),
      viewportSize: { width: frame.clientWidth, height: frame.clientHeight },
    }),
  )
}

function cancelActiveStroke() {
  if (activePointerId == null) return
  const previous = undoStack.pop()
  if (previous) restoreMask(previous)
  activePointerId = null
  lastPoint = null
  syncHistoryState()
}

/* ── 遮罩绘制 ───────────────────────────────────────── */
function syncHistoryState() {
  historyState.value = { undo: undoStack.length, redo: redoStack.length }
}

function renderPreviewNow() {
  const maskCanvas = maskCanvasRef.value
  const previewCanvas = previewCanvasRef.value
  if (!maskCanvas || !previewCanvas) return

  const ctx = previewCanvas.getContext('2d')
  if (!ctx) return

  previewFrame = null
  ctx.save()
  ctx.clearRect(0, 0, previewCanvas.width, previewCanvas.height)
  ctx.globalCompositeOperation = 'source-over'
  ctx.fillStyle = 'rgba(59, 130, 246, 0.58)'
  ctx.fillRect(0, 0, previewCanvas.width, previewCanvas.height)
  // 遮罩画的是"白色=保留"，这里反向擦除，得到蓝色=选中区域
  ctx.globalCompositeOperation = 'destination-out'
  ctx.drawImage(maskCanvas, 0, 0)
  ctx.restore()
}

function renderPreview() {
  if (previewFrame != null) return
  previewFrame = window.requestAnimationFrame(renderPreviewNow)
}

function updateCursor(point: Point | null) {
  const cursorCanvas = cursorCanvasRef.value
  const stage = stageRef.value
  const frame = baseFrameRef.value
  const maskCanvas = maskCanvasRef.value
  const ctx = cursorCanvas?.getContext('2d')
  if (!cursorCanvas || !ctx || !stage || !frame || !maskCanvas) return

  const dpr = window.devicePixelRatio || 1
  const width = stage.clientWidth
  const height = stage.clientHeight
  if (
    cursorCanvas.width !== Math.round(width * dpr) ||
    cursorCanvas.height !== Math.round(height * dpr)
  ) {
    cursorCanvas.width = Math.round(width * dpr)
    cursorCanvas.height = Math.round(height * dpr)
  }

  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, width, height)
  if (!point) return

  const scale = viewTransformState.scale
  const stageRect = stage.getBoundingClientRect()
  const frameRect = frame.getBoundingClientRect()
  const x =
    frameRect.left -
    stageRect.left +
    (point.x / maskCanvas.width) * frame.clientWidth * scale +
    viewTransformState.x
  const y =
    frameRect.top -
    stageRect.top +
    (point.y / maskCanvas.height) * frame.clientHeight * scale +
    viewTransformState.y
  const radius = (brushSize.value / 2 / maskCanvas.width) * frame.clientWidth * scale

  ctx.save()
  ctx.lineWidth = 1
  // 内外双描边，保证在任何底色上都看得清
  ctx.beginPath()
  ctx.arc(x, y, radius, 0, Math.PI * 2)
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.9)'
  ctx.stroke()

  ctx.strokeStyle = 'rgba(0, 0, 0, 0.4)'
  ctx.beginPath()
  ctx.arc(x, y, radius + 1, 0, Math.PI * 2)
  ctx.stroke()

  ctx.beginPath()
  ctx.arc(x, y, Math.max(0, radius - 1), 0, Math.PI * 2)
  ctx.stroke()

  // 中心十字，便于精确定位
  const crosshairSize = 5
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.95)'
  ctx.beginPath()
  ctx.moveTo(x - crosshairSize, y)
  ctx.lineTo(x + crosshairSize, y)
  ctx.moveTo(x, y - crosshairSize)
  ctx.lineTo(x, y + crosshairSize)
  ctx.stroke()

  ctx.strokeStyle = 'rgba(0, 0, 0, 0.55)'
  ctx.beginPath()
  ctx.moveTo(x - crosshairSize, y)
  ctx.lineTo(x + crosshairSize, y)
  ctx.moveTo(x, y - crosshairSize)
  ctx.lineTo(x, y + crosshairSize)
  ctx.stroke()
  ctx.restore()
}

function getViewportCenterCanvasPoint(): Point | null {
  const frame = baseFrameRef.value
  const maskCanvas = maskCanvasRef.value
  if (!frame || !maskCanvas) return null

  const transform = viewTransformState
  return {
    x:
      ((frame.clientWidth / 2 - transform.x) / transform.scale / frame.clientWidth) *
      maskCanvas.width,
    y:
      ((frame.clientHeight / 2 - transform.y) / transform.scale / frame.clientHeight) *
      maskCanvas.height,
  }
}

function pushUndoSnapshot() {
  const canvas = maskCanvasRef.value
  const ctx = canvas?.getContext('2d', { willReadFrequently: true })
  if (!canvas || !ctx) return

  undoStack.push(ctx.getImageData(0, 0, canvas.width, canvas.height))
  if (undoStack.length > MAX_HISTORY_STEPS) undoStack.shift()
  redoStack = []
  syncHistoryState()
}

function restoreMask(imageData: ImageData) {
  const canvas = maskCanvasRef.value
  const ctx = canvas?.getContext('2d', { willReadFrequently: true })
  if (!canvas || !ctx) return

  ctx.putImageData(imageData, 0, 0)
  renderPreview()
}

function drawAt(point: Point, nextTool = tool.value) {
  const canvas = maskCanvasRef.value
  const ctx = canvas?.getContext('2d')
  if (!canvas || !ctx) return

  ctx.save()
  // 笔刷=擦除遮罩（露出可编辑区域），橡皮=补回遮罩
  ctx.globalCompositeOperation = nextTool === 'brush' ? 'destination-out' : 'source-over'
  ctx.fillStyle = '#fff'
  ctx.beginPath()
  ctx.arc(point.x, point.y, brushSize.value / 2, 0, Math.PI * 2)
  ctx.fill()
  ctx.restore()
  renderPreview()
}

function drawStroke(from: Point, to: Point, nextTool = tool.value) {
  const canvas = maskCanvasRef.value
  const ctx = canvas?.getContext('2d')
  if (!canvas || !ctx) return

  ctx.save()
  ctx.globalCompositeOperation = nextTool === 'brush' ? 'destination-out' : 'source-over'
  ctx.strokeStyle = '#fff'
  ctx.lineWidth = brushSize.value
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  ctx.moveTo(from.x, from.y)
  ctx.lineTo(to.x, to.y)
  ctx.stroke()
  ctx.restore()
  renderPreview()
}

/* ── 指针事件 ───────────────────────────────────────── */
function handlePointerDown(event: PointerEvent) {
  if (!isReady.value || isSaving.value) return
  if (event.pointerType !== 'touch' && event.button !== 0) return

  event.preventDefault()
  showBrushControls.value = false
  sliderAnchor.value = null
  const canvas = event.currentTarget as HTMLCanvasElement

  // Alt + 拖拽 = 平移
  if (event.altKey) {
    if (!canvas.hasPointerCapture(event.pointerId)) canvas.setPointerCapture(event.pointerId)
    panGesture = {
      pointerId: event.pointerId,
      startPoint: { x: event.clientX, y: event.clientY },
      startTransform: viewTransformState,
    }
    isPanning.value = true
    updateCursor(null)
    return
  }

  pointerPositions.set(event.pointerId, { x: event.clientX, y: event.clientY })
  if (!canvas.hasPointerCapture(event.pointerId)) canvas.setPointerCapture(event.pointerId)

  // 双指 → 捏合缩放
  if (pointerPositions.size >= 2) {
    cancelActiveStroke()
    beginPinchGesture()
    return
  }

  activePointerId = event.pointerId
  pushUndoSnapshot()
  const point = getCanvasPoint(canvas, event)
  lastPoint = point
  drawAt(point)
}

function handlePointerMove(event: PointerEvent) {
  const canvas = event.currentTarget as HTMLCanvasElement
  const point = getCanvasPoint(canvas, event)

  if (event.pointerType !== 'touch') {
    isPointerOverCanvas.value = true
    hoverPoint.value = point
    updateCursor(event.altKey || isAltKeyPressed.value ? null : point)
  }

  if (panGesture?.pointerId === event.pointerId) {
    event.preventDefault()
    commitViewTransform({
      scale: panGesture.startTransform.scale,
      x: panGesture.startTransform.x + event.clientX - panGesture.startPoint.x,
      y: panGesture.startTransform.y + event.clientY - panGesture.startPoint.y,
    })
    return
  }

  if (pointerPositions.has(event.pointerId)) {
    pointerPositions.set(event.pointerId, { x: event.clientX, y: event.clientY })
  }
  if (pinchGesture && pointerPositions.size >= 2) {
    event.preventDefault()
    updatePinchGesture()
    return
  }

  if (activePointerId !== event.pointerId || !lastPoint || !isReady.value || isSaving.value) return
  event.preventDefault()
  drawStroke(lastPoint, point)
  lastPoint = point
}

function handlePointerLeave() {
  isPointerOverCanvas.value = false
  hoverPoint.value = null
  updateCursor(null)
}

function handleWheel(event: WheelEvent) {
  if (!event.altKey || !isReady.value || isSaving.value) return
  const frame = baseFrameRef.value
  if (!frame) return

  event.preventDefault()
  const rect = frame.getBoundingClientRect()
  const point = { x: event.clientX - rect.left, y: event.clientY - rect.top }
  const scaleFactor = Math.exp(-event.deltaY * 0.002)
  commitViewTransform(
    zoomAtPoint(
      viewTransformState,
      point,
      viewTransformState.scale * scaleFactor,
      { width: frame.clientWidth, height: frame.clientHeight },
    ),
  )
}

function finishStroke(event: PointerEvent) {
  const canvas = event.currentTarget as HTMLCanvasElement
  if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId)
  pointerPositions.delete(event.pointerId)

  if (pinchGesture) {
    if (pointerPositions.size >= 2) beginPinchGesture()
    else pinchGesture = null
  }
  if (panGesture?.pointerId === event.pointerId) {
    panGesture = null
    isPanning.value = false
  }
  if (activePointerId === event.pointerId) {
    activePointerId = null
    lastPoint = null
    if (hoverPoint.value) updateCursor(hoverPoint.value)
  }
}

/* ── 撤销 / 重做 / 清空 ─────────────────────────────── */
function handleUndo() {
  const canvas = maskCanvasRef.value
  const ctx = canvas?.getContext('2d', { willReadFrequently: true })
  const previous = undoStack.pop()
  if (!canvas || !ctx || !previous) return

  redoStack.push(ctx.getImageData(0, 0, canvas.width, canvas.height))
  restoreMask(previous)
  syncHistoryState()
}

function handleRedo() {
  const canvas = maskCanvasRef.value
  const ctx = canvas?.getContext('2d', { willReadFrequently: true })
  const next = redoStack.pop()
  if (!canvas || !ctx || !next) return

  undoStack.push(ctx.getImageData(0, 0, canvas.width, canvas.height))
  restoreMask(next)
  syncHistoryState()
}

function handleClear() {
  const canvas = maskCanvasRef.value
  if (!canvas || !isReady.value || isSaving.value) return
  pushUndoSnapshot()
  fillWhiteMask(canvas)
  renderPreview()
}

/* ── 模式 / 模型切换 ────────────────────────────────── */
function handleModeChange(mode: ImageEditMode) {
  editMode.value = mode
  if (mode !== 'background') {
    editParams.value = { ...editParams.value, transparent_output: false }
  }
  if (mode === 'erase') editPrompt.value = t('imageStudio.mask.erasePrompt')
  else if (mode === 'background') editPrompt.value = t('imageStudio.mask.backgroundPrompt')
  else if (
    editPrompt.value === t('imageStudio.mask.erasePrompt') ||
    editPrompt.value === t('imageStudio.mask.backgroundPrompt')
  ) {
    editPrompt.value = ''
  }
}

function handleModelChange(modelId: string) {
  const model = getImageModel(modelId, modelCatalog.value)
  if (!model) return

  selectedModelId.value = model.id
  editParams.value = normalizeParamsForSettings(
    editParams.value,
    { ...state.settings, selectedModelId: model.id },
    modelCatalog.value,
  )
  if (
    (editMode.value === 'region' || editMode.value === 'erase') &&
    !model.capabilities.mask
  ) {
    editMode.value = 'full'
  }
  if (editMode.value === 'background' && !model.capabilities.transparentOutput) {
    editMode.value = 'full'
  }
}

/* ── 笔刷大小 ───────────────────────────────────────── */
function onBrushSizeChange(next: number) {
  brushSize.value = next
  if (!isPointerOverCanvas.value && size.value) updateCursor(getViewportCenterCanvasPoint())
}

function toggleBrushControls() {
  const button = document.querySelector<HTMLButtonElement>('[data-brush-size-button]')
  const rect = button?.getBoundingClientRect()
  showBrushControls.value = !showBrushControls.value
  if (!rect) return

  isPointerOverCanvas.value = false
  hoverPoint.value = null
  if (size.value) updateCursor(getViewportCenterCanvasPoint())
  sliderAnchor.value = {
    left: rect.left + rect.width / 2,
    bottom: window.innerHeight - rect.top + 8,
  }
}

/* ── 保存 ───────────────────────────────────────────── */
async function handleSave(generate: boolean) {
  const canvas = maskCanvasRef.value
  const savingSessionId = activeSessionId
  if (
    !canvas ||
    !sourceDataUrl.value ||
    !originalSize.value ||
    !imageId.value ||
    !imageEditSession.value ||
    !isReady.value ||
    isSaving.value ||
    !savingSessionId
  ) {
    return
  }

  const needsMask = editMode.value === 'region' || editMode.value === 'erase'
  if (needsMask && !supportsMask.value) {
    state.showToast(t('imageStudio.mask.modelNoMask'), 'error')
    return
  }
  if (needsMask && !hasEditableMaskPixels(canvas)) {
    state.showToast(t('imageStudio.mask.emptyMask'), 'info')
    return
  }
  if (
    (editMode.value === 'region' || editMode.value === 'full') &&
    !editPrompt.value.trim()
  ) {
    state.showToast(t('imageStudio.mask.promptRequired'), 'info')
    return
  }
  if (editMode.value === 'background' && !supportsTransparentOutput.value) {
    state.showToast(t('imageStudio.mask.modelNoBackground'), 'error')
    return
  }

  const token = ++saveToken
  const savingImageId = imageId.value
  const isStale = () =>
    saveToken !== token ||
    activeSessionId !== savingSessionId ||
    state.maskEditorImageId !== savingImageId

  try {
    isSaving.value = true
    const maskDataUrl = needsMask
      ? await createOriginalSizeMaskDataUrl(canvas, originalSize.value)
      : null
    if (isStale()) return

    const params =
      editMode.value === 'background'
        ? {
            ...editParams.value,
            transparent_output: true,
            output_format: 'png' as const,
            output_compression: null,
          }
        : { ...editParams.value, transparent_output: false }

    const committed = state.commitImageEditSession({
      mode: editMode.value,
      prompt: editPrompt.value.trim(),
      params,
      selectedModelId: selectedModelId.value,
      maskDataUrl,
    })
    if (!committed) {
      state.showToast(t('imageStudio.mask.sessionChanged'), 'info')
      return
    }

    if (generate) await submitTask()
    else {
      state.showToast(
        needsMask ? t('imageStudio.mask.savedWithMask') : t('imageStudio.mask.savedPrompt'),
        'success',
      )
    }
  } catch (err) {
    if (isStale()) return
    state.showToast(err instanceof Error ? err.message : String(err), 'error')
  } finally {
    if (saveToken === token) isSaving.value = false
  }
}

/* ── 会话初始化 ─────────────────────────────────────── */
watch(
  imageEditSession,
  (session) => {
    if (!session) return
    editMode.value = 'region'
    editPrompt.value = session.prompt
    editParams.value = session.params
    selectedModelId.value = session.selectedModelId
  },
  { immediate: true },
)

/** 打开时递增会话号，防止旧会话的异步结果写回 */
watch(
  imageId,
  (id) => {
    if (!id) {
      activeSessionId = 0
      return
    }
    sessionId += 1
    activeSessionId = sessionId
  },
  { immediate: true },
)

/** 会话缺失时自动关闭，避免空态 */
watch(
  [imageId, imageEditSession],
  ([id, session]) => {
    if (id && !session) state.setMaskEditorImageId(null)
  },
  { immediate: true },
)

/* ── 载入画布 ───────────────────────────────────────── */
watch(
  [imageId, imageEditSession],
  async ([id, session]) => {
    if (!id) {
      if (previewFrame != null) {
        window.cancelAnimationFrame(previewFrame)
        previewFrame = null
      }
      sourceDataUrl.value = ''
      size.value = null
      originalSize.value = null
      isLoading.value = false
      pointerPositions.clear()
      pinchGesture = null
      panGesture = null
      isPanning.value = false
      viewTransformState = { ...DEFAULT_VIEW_TRANSFORM }
      viewTransform.value = { ...DEFAULT_VIEW_TRANSFORM }
      undoStack = []
      redoStack = []
      syncHistoryState()
      return
    }

    const targetImageId = id
    let cancelled = false
    isLoading.value = true
    sourceDataUrl.value = ''
    size.value = null
    undoStack = []
    redoStack = []
    syncHistoryState()

    try {
      const dataUrl = session?.sourceDataUrl || (await ensureImageCached(targetImageId))
      if (cancelled) return
      if (!dataUrl) {
        state.showToast(t('imageStudio.mask.imageMissing'), 'error')
        state.setMaskEditorImageId(null)
        return
      }

      // 移动端用小尺寸预览，降低 canvas 内存占用
      const maxEdge = window.matchMedia('(max-width: 767px)').matches ? 1280 : 1920
      const prepared = await prepareMaskTargetDataUrl(dataUrl, maxEdge)
      const image = await loadImage(prepared.dataUrl)
      if (cancelled) return

      const nextSize = { width: prepared.width, height: prepared.height }
      const imageCanvas = imageCanvasRef.value
      const previewCanvas = previewCanvasRef.value
      const maskCanvas = maskCanvasRef.value
      if (!imageCanvas || !previewCanvas || !maskCanvas) return

      for (const canvas of [imageCanvas, previewCanvas, maskCanvas]) {
        canvas.width = nextSize.width
        canvas.height = nextSize.height
      }

      const imageCtx = imageCanvas.getContext('2d')
      if (!imageCtx) throw new Error(t('imageStudio.mask.noCanvas'))
      imageCtx.clearRect(0, 0, imageCanvas.width, imageCanvas.height)
      imageCtx.drawImage(image, 0, 0)

      fillWhiteMask(maskCanvas)

      // 恢复已有遮罩草稿
      if (session?.maskDraft?.targetImageId === targetImageId) {
        try {
          const draftImage = await loadImage(session.maskDraft.maskDataUrl)
          if (cancelled) return
          drawMaskImageToCanvas(draftImage, maskCanvas)
        } catch (err) {
          fillWhiteMask(maskCanvas)
          state.showToast(
            t('imageStudio.mask.draftLoadFailed', {
              error: err instanceof Error ? err.message : String(err),
            }),
            'error',
          )
        }
      }

      renderPreview()
      sourceDataUrl.value = prepared.dataUrl
      size.value = nextSize
      originalSize.value = {
        width: prepared.originalWidth,
        height: prepared.originalHeight,
      }
      if (prepared.wasResized) {
        state.showToast(
          t('imageStudio.mask.resizedHint', {
            width: prepared.originalWidth,
            height: prepared.originalHeight,
          }),
          'info',
        )
      }
      await nextTick()
      requestAnimationFrame(() => resetViewTransform())
    } catch (err) {
      if (!cancelled) {
        state.showToast(err instanceof Error ? err.message : String(err), 'error')
        state.setMaskEditorImageId(null)
      }
    } finally {
      if (!cancelled) isLoading.value = false
    }
  },
  { immediate: true },
)

/* ── 光标重绘 ───────────────────────────────────────── */
watch([brushSize, viewTransform, hoverPoint, isPointerOverCanvas, showBrushControls, size, isAltKeyPressed], () => {
  if (isAltKeyPressed.value) {
    updateCursor(null)
  } else if (showBrushControls.value && !isPointerOverCanvas.value && size.value) {
    updateCursor(getViewportCenterCanvasPoint())
  } else {
    updateCursor(hoverPoint.value)
  }
})

/* ── Alt 键平移提示 ─────────────────────────────────── */
function onKeyDown(event: KeyboardEvent) {
  if (event.altKey) isAltKeyPressed.value = true
}
function onKeyUp(event: KeyboardEvent) {
  if (event.key === 'Alt') isAltKeyPressed.value = false
}
function onWindowBlur() {
  isAltKeyPressed.value = false
}

window.addEventListener('keydown', onKeyDown)
window.addEventListener('keyup', onKeyUp)
window.addEventListener('blur', onWindowBlur)

/* ── 笔刷面板点外部关闭 ─────────────────────────────── */
function onDocumentPointerDown(event: PointerEvent) {
  if (!showBrushControls.value) return
  const target = event.target as Node
  const panel = document.querySelector<HTMLElement>('[data-brush-panel]')
  if (panel?.contains(target)) return
  if ((event.target as HTMLElement)?.closest?.('[data-brush-size-button]')) return
  showBrushControls.value = false
  sliderAnchor.value = null
}

document.addEventListener('pointerdown', onDocumentPointerDown, true)

/* ── 容器尺寸变化时重新夹取变换 ─────────────────────── */
let resizeObserver: ResizeObserver | null = null

watch(size, async (next) => {
  if (!next || typeof ResizeObserver === 'undefined') return
  await nextTick()
  const frame = baseFrameRef.value
  if (!frame) return

  resizeObserver?.disconnect()
  resizeObserver = new ResizeObserver(() => {
    commitViewTransform(viewTransformState)
  })
  resizeObserver.observe(frame)
})

onUnmounted(() => {
  if (previewFrame != null) window.cancelAnimationFrame(previewFrame)
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
  window.removeEventListener('blur', onWindowBlur)
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  resizeObserver?.disconnect()
})
</script>
