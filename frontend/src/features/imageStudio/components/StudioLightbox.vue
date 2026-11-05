<template>
  <div
    v-if="imageId"
    ref="dialogRef"
    role="dialog"
    aria-modal="true"
    :aria-label="t('imageStudio.lightbox.aria')"
    tabindex="-1"
    class="fixed inset-0 z-[60] flex h-[100dvh] w-full flex-col bg-black/90 text-white outline-none"
  >
    <header class="flex shrink-0 flex-wrap items-center justify-between gap-2 px-3 py-3 sm:px-5">
      <span class="min-w-16 text-sm tabular-nums" aria-live="polite">{{ currentIndex + 1 }} / {{ Math.max(1, imageList.length) }}</span>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <template v-if="isInputImage">
          <input ref="fileRef" type="file" accept="image/*" class="hidden" @change="handleReplace" />
          <button type="button" class="btn btn-sm btn-ghost !text-white hover:!bg-white/15" @click="fileRef?.click()">
            <StudioIcon name="RefreshIcon" class="h-4 w-4" />{{ t('imageStudio.lightbox.replace') }}
          </button>
          <button v-if="supportsMask" type="button" class="btn btn-sm btn-primary" @click="openMaskEditor">
            <StudioIcon name="EditIcon" class="h-4 w-4" />{{ t('imageStudio.lightbox.editMask') }}
          </button>
        </template>
        <button
          v-else
          type="button"
          class="btn btn-sm btn-ghost !text-white hover:!bg-white/15"
          :disabled="!ready || downloading"
          :aria-label="t('imageStudio.lightbox.download')"
          :title="t('imageStudio.lightbox.download')"
          @click="handleDownload"
        >
          <StudioIcon name="DownloadIcon" class="h-4 w-4" />{{ t('imageStudio.lightbox.download') }}
        </button>
        <button
          type="button"
          class="btn btn-sm btn-ghost !text-white hover:!bg-white/15"
          :aria-label="t('imageStudio.lightbox.close')"
          :title="t('imageStudio.common.close')"
          @click="close"
        >
          <StudioIcon name="CloseIcon" class="h-5 w-5" />
        </button>
      </div>
    </header>

    <div
      ref="viewportRef"
      data-testid="lightbox-viewport"
      class="relative min-h-0 flex-1 touch-none select-none overflow-hidden overscroll-none"
      :class="ready ? view.scale > 1 ? dragging ? 'cursor-grabbing' : 'cursor-grab' : 'cursor-zoom-in' : ''"
      @wheel.prevent="onWheel"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="finishPointer"
      @pointercancel="finishPointer"
      @lostpointercapture="finishPointer"
      @touchstart.prevent="onTouchStart"
      @touchmove.prevent="onTouchMove"
      @touchend="onTouchEnd"
      @touchcancel="onTouchEnd"
      @dblclick.prevent="onDoubleClick"
    >
      <div v-if="src" class="absolute left-1/2 top-1/2 will-change-transform" :style="imageStyle">
        <img
          :key="`${imageId}-${imageVersion}`"
          :src="src"
          :data-image-id="imageId"
          :data-image-version="imageVersion"
          :alt="t('imageStudio.result.imageAlt')"
          draggable="false"
          class="h-full w-full max-w-none object-contain"
          :class="ready ? '' : 'invisible'"
          @load="onImageLoad"
          @error="onImageError"
          @dragstart.prevent
        />
        <img v-if="maskPreview && ready" :src="maskPreview" alt="" class="pointer-events-none absolute inset-0 h-full w-full object-contain" draggable="false" />
      </div>
      <div v-if="!ready" class="absolute inset-0 flex flex-col items-center justify-center gap-3" role="status">
        <template v-if="loadFailed">
          <p class="text-sm text-white/75">{{ t('imageStudio.lightbox.loadFailed') }}</p>
          <button type="button" class="btn btn-secondary btn-sm" @touchstart.stop @touchmove.stop @touchend.stop @click="reloadImage">{{ t('imageStudio.lightbox.retry') }}</button>
        </template>
        <LoadingSpinner v-else size="lg" color="white" :aria-label="t('imageStudio.result.loadingImage')" />
      </div>
      <template v-if="canNavigate">
        <button
          type="button"
          data-testid="lightbox-previous"
          class="btn btn-ghost absolute left-2 top-1/2 z-10 h-16 w-12 -translate-y-1/2 rounded-xl border border-white/35 !bg-black/65 !text-white shadow-lg hover:!bg-black/90 disabled:!opacity-25 sm:left-5 sm:h-20 sm:w-14"
          :disabled="currentIndex <= 0"
          :aria-label="t('imageStudio.result.prevImage')"
          :title="t('imageStudio.result.prevImage')"
          @pointerdown.stop
          @touchstart.stop
          @touchmove.stop
          @touchend.stop
          @dblclick.stop
          @click.stop="goTo(currentIndex - 1)"
        ><StudioIcon name="ChevronLeftIcon" class="h-6 w-6" /></button>
        <button
          type="button"
          data-testid="lightbox-next"
          class="btn btn-ghost absolute right-2 top-1/2 z-10 h-16 w-12 -translate-y-1/2 rounded-xl border border-white/35 !bg-black/65 !text-white shadow-lg hover:!bg-black/90 disabled:!opacity-25 sm:right-5 sm:h-20 sm:w-14"
          :disabled="currentIndex >= imageList.length - 1"
          :aria-label="t('imageStudio.result.nextImage')"
          :title="t('imageStudio.result.nextImage')"
          @pointerdown.stop
          @touchstart.stop
          @touchmove.stop
          @touchend.stop
          @dblclick.stop
          @click.stop="goTo(currentIndex + 1)"
        ><StudioIcon name="ChevronRightIcon" class="h-6 w-6" /></button>
      </template>
    </div>

    <footer class="flex shrink-0 flex-col items-center gap-2 px-3 py-3 sm:py-4">
      <div class="flex flex-wrap items-center justify-center gap-1 rounded-xl border border-white/15 bg-black/50 p-1">
        <button type="button" data-testid="lightbox-zoom-out" class="btn btn-sm btn-ghost !text-white hover:!bg-white/15" :disabled="!ready || view.scale <= 1" :aria-label="t('imageStudio.lightbox.zoomOut')" :title="t('imageStudio.lightbox.zoomOut')" @click="zoomBy(1 / 1.25)">−</button>
        <output class="min-w-16 px-1 text-center text-sm tabular-nums" :aria-label="t('imageStudio.lightbox.zoomPercent', { percent })" aria-live="polite">{{ percent }}%</output>
        <button type="button" data-testid="lightbox-zoom-in" class="btn btn-sm btn-ghost !text-white hover:!bg-white/15" :disabled="!ready || view.scale >= maxScale" :aria-label="t('imageStudio.lightbox.zoomIn')" :title="t('imageStudio.lightbox.zoomIn')" @click="zoomBy(1.25)">+</button>
        <span class="mx-1 h-5 border-l border-white/20" aria-hidden="true" />
        <button type="button" data-testid="lightbox-fit" class="btn btn-sm btn-ghost !text-white hover:!bg-white/15" :disabled="!ready" :aria-pressed="view.scale === 1" @click="resetView">{{ t('imageStudio.lightbox.fit') }}</button>
        <button type="button" data-testid="lightbox-original" class="btn btn-sm btn-ghost !text-white hover:!bg-white/15" :disabled="!ready" :title="t('imageStudio.lightbox.actualSize')" :aria-label="t('imageStudio.lightbox.actualSize')" :aria-pressed="percent === 100" @click="originalSize">100%</button>
      </div>
      <p class="hidden text-xs text-white/50 sm:block">{{ t('imageStudio.lightbox.gestureHint') }}</p>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { storeState as state, createInputImageFromFile, deleteImageIfUnreferenced } from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'
import { createMaskPreviewDataUrl } from '../lib/canvasImage'
import { downloadImageSource } from '../lib/downloadImage'
import { ensureImageCached, getCachedImage } from '../lib/imageCache'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'
import { clampLightboxTransform, FIT_TRANSFORM, fittedImage, maximumLightboxScale, pinchLightbox, zoomLightboxAt, type LightboxPoint, type LightboxTransform } from '../lib/lightboxTransform'

const { t } = useI18n()
const dialogRef = ref<HTMLDivElement | null>(null)
const viewportRef = ref<HTMLDivElement | null>(null)
const fileRef = ref<HTMLInputElement | null>(null)
const src = ref('')
const sourceImageId = ref<string | null>(null)
const maskPreview = ref('')
const loadFailed = ref(false)
const ready = ref(false)
const imageVersion = ref(0)
const downloading = ref(false)
const dragging = ref(false)
const view = ref<LightboxTransform>({ ...FIT_TRANSFORM })
const viewportSize = ref({ width: 0, height: 0 })
const naturalSize = ref({ width: 0, height: 0 })
const geometry = computed(() => ({ viewport: viewportSize.value, image: naturalSize.value }))
const fitted = computed(() => fittedImage(geometry.value))
const maxScale = computed(() => maximumLightboxScale(geometry.value))
const percent = computed(() => Math.round(fitted.value.ratio * view.value.scale * 100))
const imageStyle = computed(() => ({
  width: `${fitted.value.width}px`, height: `${fitted.value.height}px`,
  transform: `translate(-50%, -50%) translate(${view.value.x}px, ${view.value.y}px) scale(${view.value.scale})`,
  transformOrigin: 'center',
}))
const imageId = computed(() => state.lightboxImageId)
const imageList = computed(() => state.lightboxImageList)
const isInputImage = computed(() => Boolean(imageId.value && state.inputImages.some((image) => image.id === imageId.value)))
const supportsMask = computed(() => getImageModelCapabilities(state.settings.selectedModelId, state.modelCatalog).mask)
const currentIndex = computed(() => Math.max(0, imageId.value ? imageList.value.indexOf(imageId.value) : -1))
const canNavigate = computed(() => imageList.value.length > 1)
const open = computed(() => Boolean(imageId.value))
useCloseOnEscape(() => open.value, close)
usePreventBackgroundScroll(() => open.value)

let imageRequest = 0
let maskRequest = 0
let resizeObserver: ResizeObserver | null = null
let detachGestures: (() => void) | null = null
let pointer: { id: number; point: LightboxPoint; transform: LightboxTransform } | null = null
let touchGesture: { center: LightboxPoint; distance: number; transform: LightboxTransform } | null = null
let macGesture: { center: LightboxPoint; transform: LightboxTransform } | null = null
let previousFocus: HTMLElement | null = null

function releasePointer() {
  const id = pointer?.id
  pointer = null
  dragging.value = false
  if (id !== undefined && viewportRef.value?.hasPointerCapture?.(id)) {
    viewportRef.value.releasePointerCapture(id)
  }
}

function resetView() {
  view.value = { ...FIT_TRANSFORM }
  releasePointer()
  touchGesture = null
  macGesture = null
  dragging.value = false
}
function measureViewport() {
  const rect = viewportRef.value?.getBoundingClientRect()
  viewportSize.value = { width: rect?.width || window.innerWidth, height: rect?.height || Math.max(1, window.innerHeight - 160) }
  resetView()
}
function pointAt(clientX: number, clientY: number): LightboxPoint {
  const rect = viewportRef.value?.getBoundingClientRect()
  return {
    x: Number.isFinite(clientX) ? clientX - (rect?.left ?? 0) - viewportSize.value.width / 2 : 0,
    y: Number.isFinite(clientY) ? clientY - (rect?.top ?? 0) - viewportSize.value.height / 2 : 0,
  }
}
function zoomBy(factor: number, point: LightboxPoint = { x: 0, y: 0 }) {
  if (!ready.value) return
  view.value = zoomLightboxAt(view.value, point, view.value.scale * factor, geometry.value)
}
function originalSize() {
  if (ready.value) view.value = zoomLightboxAt(view.value, { x: 0, y: 0 }, 1 / fitted.value.ratio, geometry.value)
}
function onWheel(event: WheelEvent) {
  if (!ready.value) return
  const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewportSize.value.height : 1
  zoomBy(Math.exp(-Math.max(-200, Math.min(200, event.deltaY * unit)) * 0.002), pointAt(event.clientX, event.clientY))
}
function onDoubleClick(event: MouseEvent) {
  if (!ready.value) return
  if (view.value.scale > 1) resetView()
  else zoomBy(Math.max(2, 1 / fitted.value.ratio), pointAt(event.clientX, event.clientY))
}
function onPointerDown(event: PointerEvent) {
  if (!ready.value || event.pointerType === 'touch' || event.button !== 0 || view.value.scale <= 1) return
  event.preventDefault()
  releasePointer()
  pointer = { id: event.pointerId, point: pointAt(event.clientX, event.clientY), transform: { ...view.value } }
  dragging.value = true
  viewportRef.value?.setPointerCapture?.(event.pointerId)
}
function onPointerMove(event: PointerEvent) {
  if (!pointer || event.pointerId !== pointer.id) return
  const point = pointAt(event.clientX, event.clientY)
  view.value = clampLightboxTransform({ ...pointer.transform, x: pointer.transform.x + point.x - pointer.point.x, y: pointer.transform.y + point.y - pointer.point.y }, geometry.value)
}
function finishPointer(event: PointerEvent) {
  if (pointer?.id !== event.pointerId) return
  releasePointer()
}
function touchPoints(event: TouchEvent) {
  return Array.from(event.touches).slice(0, 2).map((touch) => pointAt(touch.clientX, touch.clientY))
}
function beginTouches(event: TouchEvent) {
  const points = touchPoints(event)
  if (!points.length) { touchGesture = null; dragging.value = false; return }
  const center = points.length > 1 ? { x: (points[0].x + points[1].x) / 2, y: (points[0].y + points[1].y) / 2 } : points[0]
  touchGesture = { center, distance: points.length > 1 ? Math.hypot(points[1].x - points[0].x, points[1].y - points[0].y) : 0, transform: { ...view.value } }
  dragging.value = points.length === 1 && view.value.scale > 1
}
function onTouchStart(event: TouchEvent) { if (ready.value) beginTouches(event) }
function onTouchMove(event: TouchEvent) {
  if (!ready.value || !touchGesture) return
  const points = touchPoints(event)
  if (points.length > 1 && touchGesture.distance > 0) {
    const center = { x: (points[0].x + points[1].x) / 2, y: (points[0].y + points[1].y) / 2 }
    view.value = pinchLightbox({ transform: touchGesture.transform, startCenter: touchGesture.center, nextCenter: center, startDistance: touchGesture.distance, nextDistance: Math.hypot(points[1].x - points[0].x, points[1].y - points[0].y), geometry: geometry.value })
  } else if (points.length === 1) {
    view.value = clampLightboxTransform({ ...touchGesture.transform, x: touchGesture.transform.x + points[0].x - touchGesture.center.x, y: touchGesture.transform.y + points[0].y - touchGesture.center.y }, geometry.value)
  }
}
function onTouchEnd(event: TouchEvent) { beginTouches(event) }
function onImageLoad(event: Event) {
  const image = event.target as HTMLImageElement
  if (image.dataset.imageId !== imageId.value || Number(image.dataset.imageVersion) !== imageVersion.value) return
  naturalSize.value = { width: image.naturalWidth, height: image.naturalHeight }
  ready.value = image.naturalWidth > 0 && image.naturalHeight > 0
  loadFailed.value = !ready.value
  measureViewport()
}
function onImageError(event: Event) {
  const image = event.target as HTMLImageElement
  if (image.dataset.imageId !== imageId.value || Number(image.dataset.imageVersion) !== imageVersion.value) return
  ready.value = false
  loadFailed.value = true
}
async function loadCurrentImage() {
  const id = imageId.value
  const request = ++imageRequest
  imageVersion.value += 1
  maskRequest += 1
  src.value = ''; maskPreview.value = ''; ready.value = false; loadFailed.value = false
  sourceImageId.value = null
  naturalSize.value = { width: 0, height: 0 }
  resetView()
  if (!id) return
  const cached = getCachedImage(id)
  if (cached) { src.value = cached; sourceImageId.value = id; return }
  try {
    const value = await ensureImageCached(id)
    if (request !== imageRequest || imageId.value !== id) return
    src.value = value ?? ''
    sourceImageId.value = value ? id : null
    loadFailed.value = !value
  } catch {
    if (request === imageRequest && imageId.value === id) loadFailed.value = true
  }
}
function reloadImage() { void loadCurrentImage() }
watch(imageId, () => { void loadCurrentImage() }, { immediate: true })
watch([imageId, src, sourceImageId, () => state.maskDraft], async ([id, source, sourceId]) => {
  const request = ++maskRequest
  maskPreview.value = ''
  if (!id || !source || id !== sourceId) return
  try {
    const taskMaskId = [...state.tasks].reverse().find((task) => task.maskTargetImageId === id && task.maskImageId)?.maskImageId
    const mask = state.maskDraft?.targetImageId === id ? state.maskDraft.maskDataUrl : taskMaskId ? getCachedImage(taskMaskId) ?? await ensureImageCached(taskMaskId) ?? '' : ''
    if (!mask || request !== maskRequest) return
    const preview = await createMaskPreviewDataUrl(source, mask)
    if (request === maskRequest && imageId.value === id && src.value === source) maskPreview.value = preview
  } catch { if (request === maskRequest) maskPreview.value = '' }
}, { immediate: true })
watch(viewportRef, (element) => {
  resizeObserver?.disconnect(); detachGestures?.()
  if (!element) return
  measureViewport()
  resizeObserver = new ResizeObserver(measureViewport)
  resizeObserver.observe(element)
  const start = (raw: Event) => {
    if (!ready.value) return
    raw.preventDefault()
    const event = raw as Event & { clientX: number; clientY: number }
    macGesture = { center: pointAt(event.clientX, event.clientY), transform: { ...view.value } }
  }
  const change = (raw: Event) => {
    raw.preventDefault()
    const event = raw as Event & { scale: number; clientX: number; clientY: number }
    if (!macGesture || !ready.value || !Number.isFinite(event.scale) || event.scale <= 0) return
    view.value = pinchLightbox({ transform: macGesture.transform, startCenter: macGesture.center, nextCenter: pointAt(event.clientX, event.clientY), startDistance: 1, nextDistance: event.scale, geometry: geometry.value })
  }
  const end = () => { macGesture = null }
  element.addEventListener('gesturestart', start, { passive: false })
  element.addEventListener('gesturechange', change, { passive: false })
  element.addEventListener('gestureend', end)
  detachGestures = () => { element.removeEventListener('gesturestart', start); element.removeEventListener('gesturechange', change); element.removeEventListener('gestureend', end) }
}, { flush: 'post' })
watch(open, async (value) => {
  if (value) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    await nextTick()
    if (open.value) dialogRef.value?.focus()
  } else {
    if (previousFocus?.isConnected) previousFocus.focus()
    previousFocus = null
  }
}, { immediate: true })
function onKeyDown(event: KeyboardEvent) {
  if (!open.value) return
  if (event.key === 'Tab') {
    const focusable = Array.from(dialogRef.value?.querySelectorAll<HTMLElement>('button:not([disabled]), [href], input:not([type="file"]):not([disabled]), [tabindex="0"]') ?? [])
    const first = focusable[0]
    const last = focusable.at(-1)
    if (!first) { event.preventDefault(); dialogRef.value?.focus(); return }
    if (event.shiftKey && (document.activeElement === first || document.activeElement === dialogRef.value)) {
      event.preventDefault(); last?.focus()
    } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialogRef.value)) {
      event.preventDefault(); first.focus()
    }
    return
  }
  if (event.target instanceof HTMLElement && event.target.closest('input, textarea, select, [contenteditable="true"]')) return
  if (['ArrowLeft', 'ArrowRight', '+', '=', '-', '0'].includes(event.key)) event.preventDefault()
  if (event.key === 'ArrowLeft') goTo(currentIndex.value - 1)
  else if (event.key === 'ArrowRight') goTo(currentIndex.value + 1)
  else if (event.key === '+' || event.key === '=') zoomBy(1.25)
  else if (event.key === '-') zoomBy(1 / 1.25)
  else if (event.key === '0') resetView()
}
onMounted(() => {
  window.addEventListener('resize', measureViewport)
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('mouseup', releasePointer)
})
onUnmounted(() => {
  imageRequest += 1; maskRequest += 1
  resizeObserver?.disconnect(); detachGestures?.()
  releasePointer()
  window.removeEventListener('resize', measureViewport); window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('mouseup', releasePointer)
  if (previousFocus?.isConnected) previousFocus.focus()
})
function goTo(index: number) {
  if (!canNavigate.value || index < 0 || index >= imageList.value.length) return
  state.setLightboxImageId(imageList.value[index], imageList.value)
}
function close() { state.setLightboxImageId(null) }
function openMaskEditor() {
  if (!imageId.value) return
  const id = imageId.value
  close()
  state.setMaskEditorImageId(id)
}
async function handleReplace(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const targetId = imageId.value
  input.value = ''
  if (!file || !targetId) return
  try {
    const image = await createInputImageFromFile(file)
    if (!image) { state.showToast(t('imageStudio.lightbox.invalidImage'), 'error'); return }
    const targetIndex = state.inputImages.findIndex((item) => item.id === targetId)
    if (targetIndex < 0) return
    state.replaceInputImage(targetIndex, image)
    const nextList = imageList.value.map((id) => id === targetId ? image.id : id)
    state.setLightboxImageId(imageId.value === targetId ? image.id : imageId.value, nextList)
    void deleteImageIfUnreferenced(targetId)
    state.showToast(t('imageStudio.lightbox.replaced'), 'success')
  } catch (error) { state.showToast(error instanceof Error ? error.message : t('imageStudio.lightbox.replaceFailed'), 'error') }
}
async function handleDownload() {
  if (!ready.value || downloading.value) return
  downloading.value = true
  try { await downloadImageSource(src.value); state.showToast(t('imageStudio.lightbox.downloaded'), 'success') }
  catch { state.showToast(t('imageStudio.lightbox.downloadFailed'), 'error') }
  finally { downloading.value = false }
}
</script>
