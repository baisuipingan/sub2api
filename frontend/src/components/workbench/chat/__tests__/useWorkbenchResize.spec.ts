import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useWorkbenchResize } from '../useWorkbenchResize'

describe('workspace panel geometry', () => {
  afterEach(() => vi.restoreAllMocks())
  it('keeps space for chat, reclamps on a sidebar/viewport change and cleans listeners', async () => {
    let available = 1200
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({ width: available, height: 900, top: 48, left: 240, right: available + 240, bottom: 948, x: 240, y: 48, toJSON: () => ({}) }))
    let controls!: ReturnType<typeof useWorkbenchResize>
    const remove = vi.spyOn(window, 'removeEventListener')
    const wrapper = mount(defineComponent({ setup() { const surface = ref<HTMLElement | null>(null); controls = useWorkbenchResize(surface); return () => h('div', { ref: surface }) } }))
    await nextTick()
    controls.keydown(new KeyboardEvent('keydown', { key: 'End' }))
    expect(controls.panelStyle.value.width).toBe('840px')
    available = 784
    window.dispatchEvent(new Event('resize'))
    expect(controls.panelStyle.value.width).toBe('424px')
    expect(controls.fullscreenStyle.value).toMatchObject({ width: '784px', height: '900px', top: '48px', left: '240px' })
    controls.keydown(new KeyboardEvent('keydown', { key: 'Home' }))
    expect(controls.width.value).toBe(424)
    controls.reset()
    expect(controls.width.value).toBeNull()
    wrapper.unmount()
    expect(remove).toHaveBeenCalledWith('resize', expect.any(Function))
    expect(remove).toHaveBeenCalledWith('pointermove', expect.any(Function))
  })
  it('ignores other pointers and releases a drag when the panel closes', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({ width: 1200, right: 1440, top: 0, left: 240, height: 1000, bottom: 1000, x: 240, y: 0, toJSON: () => ({}) }))
    let controls!: ReturnType<typeof useWorkbenchResize>
    const wrapper = mount(defineComponent({ setup() { const surface = ref<HTMLElement | null>(null); controls = useWorkbenchResize(surface); return () => h('div', { ref: surface }) } }))
    await nextTick()
    const handle = document.createElement('div')
    handle.setPointerCapture = vi.fn(); handle.hasPointerCapture = vi.fn(() => true); handle.releasePointerCapture = vi.fn()
    controls.start({ button: 0, pointerId: 1, currentTarget: handle, preventDefault: vi.fn() } as unknown as PointerEvent)
    window.dispatchEvent(Object.assign(new Event('pointermove'), { pointerId: 2, clientX: 700 }))
    expect(controls.width.value).toBeNull()
    window.dispatchEvent(Object.assign(new Event('pointermove'), { pointerId: 1, clientX: 800 }))
    expect(controls.width.value).toBe(640)
    controls.stop()
    expect(controls.dragging.value).toBe(false)
    expect(handle.releasePointerCapture).toHaveBeenCalledWith(1)
    window.dispatchEvent(Object.assign(new Event('pointermove'), { pointerId: 1, clientX: 700 }))
    expect(controls.width.value).toBe(640)
    wrapper.unmount()
  })
})
