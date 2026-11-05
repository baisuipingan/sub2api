import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { InputImage, MaskDraft, TaskRecord } from '../../types'

const { rawState, ensureImageCached, getCachedImage, createMaskPreviewDataUrl, downloadImageSource, createInputImageFromFile, deleteImageIfUnreferenced } = vi.hoisted(() => ({
  rawState: {
    lightboxImageId: 'image-one' as string | null,
    lightboxImageList: ['image-one', 'image-two'],
    inputImages: [] as InputImage[], tasks: [] as TaskRecord[], maskDraft: null as MaskDraft | null,
    settings: { selectedModelId: 'gpt-image-2' }, modelCatalog: [] as unknown[],
    setLightboxImageId: vi.fn(), setMaskEditorImageId: vi.fn(), replaceInputImage: vi.fn(), showToast: vi.fn(),
  },
  ensureImageCached: vi.fn(), getCachedImage: vi.fn(), createMaskPreviewDataUrl: vi.fn(),
  downloadImageSource: vi.fn(), createInputImageFromFile: vi.fn(), deleteImageIfUnreferenced: vi.fn(),
}))
vi.mock('../../stores/store', async () => ({
  storeState: (await import('vue')).reactive(rawState), createInputImageFromFile, deleteImageIfUnreferenced,
}))
vi.mock('../../lib/imageCache', () => ({ ensureImageCached, getCachedImage }))
vi.mock('../../lib/canvasImage', () => ({ createMaskPreviewDataUrl }))
vi.mock('../../lib/downloadImage', () => ({ downloadImageSource }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import { storeState as state } from '../../stores/store'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import StudioLightbox from '../StudioLightbox.vue'

enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((finish) => { resolve = finish })
  return { promise, resolve }
}
function mountViewer() {
  return mount(StudioLightbox, { attachTo: document.body, global: { stubs: { StudioIcon: true } } })
}
type Viewer = ReturnType<typeof mountViewer>
async function loaded(wrapper: Viewer, width = 2000, height = 1000) {
  await flushPromises()
  const image = wrapper.get<HTMLImageElement>('img[data-image-id]')
  Object.defineProperties(image.element, { naturalWidth: { configurable: true, value: width }, naturalHeight: { configurable: true, value: height } })
  await image.trigger('load')
  await flushPromises()
}
function transform(wrapper: Viewer) {
  const style = wrapper.get('img[data-image-id]').element.parentElement!.style.transform
  const match = style.match(/translate\((-?[\d.]+)px, (-?[\d.]+)px\) scale\(([\d.]+)\)/)!
  return { x: Number(match[1]), y: Number(match[2]), scale: Number(match[3]) }
}
async function touch(wrapper: Viewer, type: string, points: Array<[number, number]>) {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'touches', { value: points.map(([clientX, clientY]) => ({ clientX, clientY })) })
  wrapper.get('[data-testid="lightbox-viewport"]').element.dispatchEvent(event)
  await wrapper.vm.$nextTick()
  return event
}

beforeEach(() => {
  vi.clearAllMocks()
  getCachedImage.mockReset().mockImplementation((id: string) => `data:image/png;base64,${id}`)
  ensureImageCached.mockReset().mockResolvedValue('data:image/png;base64,loaded')
  downloadImageSource.mockReset().mockResolvedValue(undefined)
  createMaskPreviewDataUrl.mockReset().mockResolvedValue('data:image/png;base64,mask')
  state.lightboxImageId = 'image-one'
  state.lightboxImageList = ['image-one', 'image-two']
  state.inputImages = []; state.tasks = []; state.maskDraft = null
  state.modelCatalog = createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', api: 'openai' }] })
  vi.mocked(state.setLightboxImageId).mockImplementation((id, list = []) => {
    state.lightboxImageId = id
    state.lightboxImageList = id ? list : []
  })
  vi.mocked(state.replaceInputImage).mockImplementation((index, image) => { state.inputImages[index] = image })
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0, width: 1000, height: 600, right: 1000, bottom: 600, x: 0, y: 0, toJSON: () => ({}) })
})

describe('StudioLightbox image viewer', () => {
  it('fits the image and provides original-pixel size and reset controls', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    expect(transform(wrapper)).toEqual({ scale: 1, x: 0, y: 0 })
    expect(wrapper.get('output').text()).toBe('50%')
    await wrapper.get('[data-testid="lightbox-original"]').trigger('click')
    expect(wrapper.get('output').text()).toBe('100%')
    expect(transform(wrapper).scale).toBe(2)
    await wrapper.get('[data-testid="lightbox-fit"]').trigger('click')
    expect(transform(wrapper)).toEqual({ scale: 1, x: 0, y: 0 })
  })

  it.each([false, true])('zooms around the pointer for wheel with Ctrl=%s and prevents page zoom', async (ctrlKey) => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    const event = new WheelEvent('wheel', { deltaY: -100, clientX: 620, clientY: 300, ctrlKey, bubbles: true, cancelable: true })
    wrapper.get('[data-testid="lightbox-viewport"]').element.dispatchEvent(event)
    await wrapper.vm.$nextTick()
    const view = transform(wrapper)
    expect(event.defaultPrevented).toBe(true)
    expect(view.scale).toBeGreaterThan(1)
    expect((120 - view.x) / view.scale).toBeCloseTo(120)
  })

  it('pans a zoomed image and prevents dragging past its edges', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    await wrapper.get('[data-testid="lightbox-original"]').trigger('click')
    const viewport = wrapper.get('[data-testid="lightbox-viewport"]')
    await viewport.trigger('pointerdown', { pointerId: 1, pointerType: 'mouse', button: 0, clientX: 500, clientY: 300 })
    await viewport.trigger('pointermove', { pointerId: 1, clientX: 5000, clientY: -5000 })
    expect(transform(wrapper)).toEqual({ scale: 2, x: 500, y: -200 })
    await viewport.trigger('pointerup', { pointerId: 1 })
    expect(viewport.classes()).not.toContain('cursor-grabbing')
  })

  it('keeps the actual image available to the existing right-click image actions', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    const image = wrapper.get('img[data-image-id]')
    expect(image.element.parentElement!.classList.contains('pointer-events-none')).toBe(false)
    expect(image.attributes('draggable')).toBe('false')
    const target = vi.fn()
    wrapper.element.addEventListener('contextmenu', (event) => target(event.target))
    await image.trigger('contextmenu')
    expect(target).toHaveBeenCalledWith(image.element)
  })

  it('releases pointer capture on drag completion and uses mouseup as a fallback', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    await wrapper.get('[data-testid="lightbox-original"]').trigger('click')
    const viewport = wrapper.get<HTMLElement>('[data-testid="lightbox-viewport"]')
    const capture = vi.fn()
    const release = vi.fn()
    Object.assign(viewport.element, { setPointerCapture: capture, hasPointerCapture: () => true, releasePointerCapture: release })
    await viewport.trigger('pointerdown', { pointerId: 7, pointerType: 'mouse', button: 0, clientX: 500, clientY: 300 })
    expect(capture).toHaveBeenCalledWith(7)
    window.dispatchEvent(new MouseEvent('mouseup'))
    await wrapper.vm.$nextTick()
    expect(release).toHaveBeenCalledWith(7)
    expect(viewport.classes()).not.toContain('cursor-grabbing')
  })

  it('pinches from two touch points around their midpoint and then allows one-finger pan', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    await touch(wrapper, 'touchstart', [[400, 300], [600, 300]])
    const move = await touch(wrapper, 'touchmove', [[300, 300], [700, 300]])
    expect(move.defaultPrevented).toBe(true)
    expect(transform(wrapper)).toEqual({ scale: 2, x: 0, y: 0 })
    await touch(wrapper, 'touchend', [[700, 300]])
    await touch(wrapper, 'touchmove', [[750, 340]])
    expect(transform(wrapper)).toEqual({ scale: 2, x: 50, y: 40 })
    await touch(wrapper, 'touchend', [])
  })

  it('supports Safari gesture scale without invoking browser page zoom', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    const viewport = wrapper.get('[data-testid="lightbox-viewport"]').element
    for (const [type, scale] of [['gesturestart', 1], ['gesturechange', 2]] as const) {
      const event = new Event(type, { cancelable: true })
      Object.assign(event, { clientX: 500, clientY: 300, scale })
      viewport.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
    }
    await wrapper.vm.$nextTick()
    expect(transform(wrapper).scale).toBe(2)
  })

  it('uses the viewport center when a Safari gesture does not include pointer coordinates', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    const viewport = wrapper.get('[data-testid="lightbox-viewport"]').element
    viewport.dispatchEvent(new Event('gesturestart', { cancelable: true }))
    const event = new Event('gesturechange', { cancelable: true })
    Object.assign(event, { scale: 2 })
    viewport.dispatchEvent(event)
    await wrapper.vm.$nextTick()

    expect(transform(wrapper)).toEqual({ scale: 2, x: 0, y: 0 })
  })

  it('double-clicks between zoomed view and fit', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    const viewport = wrapper.get('[data-testid="lightbox-viewport"]')
    await viewport.trigger('dblclick', { clientX: 500, clientY: 300 })
    expect(transform(wrapper).scale).toBe(2)
    await viewport.trigger('dblclick', { clientX: 500, clientY: 300 })
    expect(transform(wrapper).scale).toBe(1)
  })

  it('keeps visible navigation at both edges, disables boundaries, and resets fit on image changes', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    expect(wrapper.get('[data-testid="lightbox-previous"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="lightbox-zoom-in"]').trigger('click')
    await wrapper.get('[data-testid="lightbox-next"]').trigger('click')
    await loaded(wrapper)
    expect(state.lightboxImageId).toBe('image-two')
    expect(transform(wrapper).scale).toBe(1)
    expect(wrapper.get('[data-testid="lightbox-next"]').attributes('disabled')).toBeDefined()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
    expect(state.lightboxImageId).toBe('image-two')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }))
    await loaded(wrapper)
    expect(state.lightboxImageId).toBe('image-one')
  })

  it('uses keyboard zoom and reset shortcuts, while leaving text inputs alone', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '+' }))
    await wrapper.vm.$nextTick()
    expect(transform(wrapper).scale).toBe(1.25)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '0' }))
    await wrapper.vm.$nextTick()
    expect(transform(wrapper).scale).toBe(1)
    const input = document.createElement('input')
    wrapper.element.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(transform(wrapper).scale).toBe(1)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(state.lightboxImageId).toBeNull()
  })

  it('resets to fit when the viewport is resized or the viewer is closed and reopened', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    await wrapper.get('[data-testid="lightbox-zoom-in"]').trigger('click')
    window.dispatchEvent(new Event('resize'))
    await wrapper.vm.$nextTick()
    expect(transform(wrapper).scale).toBe(1)
    await wrapper.get('[data-testid="lightbox-zoom-in"]').trigger('click')
    state.setLightboxImageId(null)
    await wrapper.vm.$nextTick()
    state.setLightboxImageId('image-one', ['image-one'])
    await loaded(wrapper)
    expect(transform(wrapper).scale).toBe(1)
  })

  it('contains Tab focus and restores the previously focused control on close', async () => {
    const opener = document.createElement('button')
    document.body.appendChild(opener)
    opener.focus()
    const wrapper = mountViewer()
    await loaded(wrapper)
    expect(document.activeElement).toBe(wrapper.element)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', cancelable: true }))
    const first = wrapper.findAll('button').filter((button) => !button.attributes('disabled'))[0]
    expect(document.activeElement).toBe(first.element)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true }))
    expect(document.activeElement).toBe(wrapper.get('[data-testid="lightbox-original"]').element)
    state.setLightboxImageId(null)
    await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(opener)
    opener.remove()
  })

  it('does not let a slower previous image request replace the current image', async () => {
    getCachedImage.mockReturnValue(undefined)
    const old = deferred<string>()
    const next = deferred<string>()
    ensureImageCached.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const wrapper = mountViewer()
    await flushPromises()
    state.setLightboxImageId('image-two', ['image-one', 'image-two'])
    await flushPromises()
    next.resolve('data:image/png;base64,next-image')
    await loaded(wrapper)
    old.resolve('data:image/png;base64,old-image')
    await flushPromises()
    expect(wrapper.get('img[data-image-id]').attributes('src')).toBe('data:image/png;base64,next-image')
  })

  it('shows a retry control for missing images and allows recovery', async () => {
    getCachedImage.mockReturnValue(undefined)
    ensureImageCached.mockResolvedValueOnce(undefined).mockResolvedValueOnce('data:image/png;base64,recovered')
    const wrapper = mountViewer()
    await flushPromises()
    expect(wrapper.text()).toContain('imageStudio.lightbox.loadFailed')
    await wrapper.findAll('button').find((button) => button.text() === 'imageStudio.lightbox.retry')!.trigger('click')
    await loaded(wrapper)
    expect(wrapper.get('img[data-image-id]').attributes('src')).toBe('data:image/png;base64,recovered')
  })

  it('reloads a cached image after a decoding failure by remounting its image element', async () => {
    const wrapper = mountViewer()
    await flushPromises()
    const oldImage = wrapper.get('img[data-image-id]').element
    await wrapper.get('img[data-image-id]').trigger('error')
    expect(wrapper.text()).toContain('imageStudio.lightbox.loadFailed')
    await wrapper.findAll('button').find((button) => button.text() === 'imageStudio.lightbox.retry')!.trigger('click')
    await loaded(wrapper)

    expect(wrapper.get('img[data-image-id]').element).not.toBe(oldImage)
    expect(wrapper.get('[data-testid="lightbox-zoom-in"]').attributes('disabled')).toBeUndefined()
  })

  it('does not apply an older mask preview to the newly selected image', async () => {
    state.maskDraft = { targetImageId: 'image-one', maskDataUrl: 'mask-one', updatedAt: 1 }
    const oldMask = deferred<string>()
    const newMask = deferred<string>()
    createMaskPreviewDataUrl.mockReturnValueOnce(oldMask.promise).mockReturnValueOnce(newMask.promise)
    const wrapper = mountViewer()
    await loaded(wrapper)
    state.maskDraft = { targetImageId: 'image-two', maskDataUrl: 'mask-two', updatedAt: 2 }
    state.setLightboxImageId('image-two', ['image-one', 'image-two'])
    await loaded(wrapper)
    expect(createMaskPreviewDataUrl.mock.calls).toEqual([
      ['data:image/png;base64,image-one', 'mask-one'],
      ['data:image/png;base64,image-two', 'mask-two'],
    ])
    newMask.resolve('data:image/png;base64,new-mask')
    await flushPromises()
    oldMask.resolve('data:image/png;base64,old-mask')
    await flushPromises()

    expect(wrapper.findAll('img').map((image) => image.attributes('src'))).toContain('data:image/png;base64,new-mask')
    expect(wrapper.findAll('img').map((image) => image.attributes('src'))).not.toContain('data:image/png;base64,old-mask')
  })

  it('keeps downloading the original source and the reference replacement/mask actions', async () => {
    const wrapper = mountViewer()
    await loaded(wrapper)
    await wrapper.get('button[aria-label="imageStudio.lightbox.download"]').trigger('click')
    await flushPromises()
    expect(downloadImageSource).toHaveBeenCalledWith('data:image/png;base64,image-one')
    state.inputImages = [{ id: 'image-one', dataUrl: 'data:image/png;base64,image-one' }]
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('imageStudio.lightbox.replace')
    expect(wrapper.text()).toContain('imageStudio.lightbox.editMask')
    await wrapper.findAll('button').find((button) => button.text().includes('imageStudio.lightbox.editMask'))!.trigger('click')
    expect(state.setMaskEditorImageId).toHaveBeenCalledWith('image-one')
    expect(state.lightboxImageId).toBeNull()
  })

  it('replaces the original reference and cleans up the old ID rather than the new one', async () => {
    state.inputImages = [{ id: 'image-one', dataUrl: 'data:image/png;base64,image-one' }]
    createInputImageFromFile.mockResolvedValueOnce({ id: 'new-reference', dataUrl: 'data:image/png;base64,new-reference' })
    const wrapper = mountViewer()
    await loaded(wrapper)
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['png'], 'reference.png', { type: 'image/png' })] })
    await input.trigger('change')
    await flushPromises()
    expect(state.lightboxImageId).toBe('new-reference')
    expect(state.inputImages[0].id).toBe('new-reference')
    expect(deleteImageIfUnreferenced).toHaveBeenCalledWith('image-one')
  })
})
