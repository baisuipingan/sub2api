import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'

/** Vue port of ChatView's workspace resize boundary; no file/viewer remounts. */
export function useWorkbenchResize(surface: Ref<HTMLElement | null>) {
  const width = ref<number | null>(null)
  const dragging = ref(false)
  let activePointer: number | null = null
  let target: HTMLElement | null = null
  const surfaceBox = ref({ width: 1200, height: 0, top: 0, left: 0 })
  let observer: ResizeObserver | undefined
  function measure() {
    const rect = surface.value?.getBoundingClientRect()
    if (rect) surfaceBox.value = { width: rect.width, height: rect.height, top: rect.top, left: rect.left }
    if (width.value !== null) width.value = clamp(width.value)
  }
  const bounds = () => {
    const max = Math.max(240, surfaceBox.value.width - 360)
    return { min: Math.min(440, max), max }
  }
  const clamp = (value: number) => {
    const { min, max } = bounds()
    return Math.round(Math.max(min, Math.min(max, value)))
  }
  const panelStyle = computed(() => ({ width: `${clamp(width.value ?? Math.min(720, surfaceBox.value.width * .46))}px` }))
  const fullscreenStyle = computed(() => ({ top: `${surfaceBox.value.top}px`, left: `${surfaceBox.value.left}px`, width: `${surfaceBox.value.width}px`, height: `${surfaceBox.value.height}px`, right: 'auto', bottom: 'auto' }))
  const limits = computed(bounds)
  function stop() {
    if (target && activePointer !== null && target.hasPointerCapture?.(activePointer)) target.releasePointerCapture(activePointer)
    activePointer = null
    target = null
    dragging.value = false
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', stop)
    window.removeEventListener('pointercancel', stop)
  }
  function move(event: PointerEvent) {
    if (event.pointerId !== activePointer || !surface.value) return
    width.value = clamp(surface.value.getBoundingClientRect().right - event.clientX)
  }
  function start(event: PointerEvent) {
    if (event.button !== 0 || !surface.value) return
    event.preventDefault()
    stop()
    target = event.currentTarget as HTMLElement
    activePointer = event.pointerId
    target.setPointerCapture?.(event.pointerId)
    dragging.value = true
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
    window.addEventListener('pointercancel', stop)
  }
  function keydown(event: KeyboardEvent) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const { min, max } = bounds()
    const initial = width.value ?? clamp(Math.min(720, surfaceBox.value.width * .46))
    width.value = event.key === 'Home' ? min : event.key === 'End' ? max : clamp(initial + (event.key === 'ArrowLeft' ? 24 : -24))
  }
  const reset = () => { stop(); width.value = null }
  watch(surface, element => {
    observer?.disconnect()
    if (!element) return
    measure()
    if (typeof ResizeObserver !== 'undefined') { observer = new ResizeObserver(measure); observer.observe(element) }
  }, { flush: 'post' })
  window.addEventListener('resize', measure)
  onBeforeUnmount(() => { stop(); observer?.disconnect(); window.removeEventListener('resize', measure) })
  return { width, panelStyle, fullscreenStyle, limits, dragging, start, keydown, reset, stop }
}
