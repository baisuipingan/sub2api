import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

const { state, addImageFromUrl, ensureImageCached, downloadImageSource } = vi.hoisted(() => ({
  state: {
    inputImages: [] as { id: string }[],
    showToast: vi.fn(),
    setDetailTaskId: vi.fn(),
    setLightboxImageId: vi.fn(),
    setMaskEditorImageId: vi.fn(),
  },
  addImageFromUrl: vi.fn(),
  ensureImageCached: vi.fn(),
  downloadImageSource: vi.fn(),
}))

vi.mock('../../stores/store', () => ({ storeState: state, addImageFromUrl }))
vi.mock('../../lib/imageCache', () => ({ ensureImageCached }))
vi.mock('../../lib/downloadImage', () => ({ downloadImageSource }))
vi.mock('../../lib/clipboard', () => ({
  canCopyImageToClipboard: () => false,
  copyImageSourceToClipboard: vi.fn(),
  getClipboardFailureMessage: vi.fn(),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import StudioImageContextMenu from '../StudioImageContextMenu.vue'

enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())
const original = 'data:image/png;base64,original-image'

beforeEach(() => {
  vi.clearAllMocks()
  state.inputImages = []
  ensureImageCached.mockReset().mockResolvedValue(original)
  addImageFromUrl.mockReset().mockResolvedValue(undefined)
})

async function openMenu(imageId?: string) {
  const wrapper = mount(StudioImageContextMenu, { global: { stubs: { StudioIcon: true } } })
  const img = document.createElement('img')
  img.src = 'data:image/png;base64,thumbnail'
  if (imageId) img.dataset.imageId = imageId
  document.body.appendChild(img)
  img.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 100, clientY: 100 }))
  img.remove()
  await wrapper.vm.$nextTick()
  return { wrapper, src: img.src }
}

describe('StudioImageContextMenu', () => {
  it('adds the original image after closing the menu, even when its cache read is pending', async () => {
    let resolve!: (src: string) => void
    ensureImageCached.mockReturnValueOnce(new Promise<string>((done) => { resolve = done }))
    const { wrapper } = await openMenu('generated-image')
    await wrapper.get('button:last-child').trigger('click')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(ensureImageCached).toHaveBeenCalledWith('generated-image')
    resolve(original)
    await flushPromises()

    expect(addImageFromUrl).toHaveBeenCalledTimes(1)
    expect(addImageFromUrl).toHaveBeenCalledWith(original)
    expect(state.showToast).toHaveBeenCalledWith('imageStudio.contextMenu.addedToReference', 'success')
  })

  it('preserves the displayed image source when the image has no saved ID', async () => {
    const { wrapper, src } = await openMenu()
    await wrapper.get('button:last-child').trigger('click')
    await flushPromises()
    expect(ensureImageCached).not.toHaveBeenCalled()
    expect(addImageFromUrl).toHaveBeenCalledWith(src)
  })

  it('uses the displayed source when the stored original is missing', async () => {
    ensureImageCached.mockResolvedValueOnce(undefined)
    const { wrapper, src } = await openMenu('missing-image')
    await wrapper.get('button:last-child').trigger('click')
    await flushPromises()
    expect(addImageFromUrl).toHaveBeenCalledWith(src)
  })

  it('reports read errors without claiming a reference was added', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    ensureImageCached.mockRejectedValueOnce(new Error('Image unavailable'))
    const { wrapper } = await openMenu('missing-image')
    await wrapper.get('button:last-child').trigger('click')
    await flushPromises()
    expect(addImageFromUrl).not.toHaveBeenCalled()
    expect(state.showToast).toHaveBeenCalledWith('imageStudio.contextMenu.addFailed', 'error')
  })

  it('downloads the original after closing the menu', async () => {
    const { wrapper } = await openMenu('generated-image')
    await wrapper.get('button:first-child').trigger('click')
    await flushPromises()
    expect(downloadImageSource).toHaveBeenCalledWith(original)
  })
})
