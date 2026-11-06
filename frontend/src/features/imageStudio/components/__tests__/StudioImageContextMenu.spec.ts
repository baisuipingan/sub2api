import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import type { TaskRecord } from '../../types'
import { useCloseOnEscape } from '../../composables/useCloseOnEscape'

const { state, addImageFromUrl, ensureImageCached, downloadImageSource, confirmTaskDeletion } = vi.hoisted(() => ({
  state: {
    tasks: [] as TaskRecord[],
    inputImages: [] as { id: string }[],
    showToast: vi.fn(),
    setDetailTaskId: vi.fn(),
    setLightboxImageId: vi.fn(),
    setMaskEditorImageId: vi.fn(),
  },
  addImageFromUrl: vi.fn(),
  ensureImageCached: vi.fn(),
  downloadImageSource: vi.fn(),
  confirmTaskDeletion: vi.fn(),
}))

vi.mock('../../stores/store', () => ({ storeState: state, addImageFromUrl }))
vi.mock('../../lib/imageCache', () => ({ ensureImageCached }))
vi.mock('../../lib/downloadImage', () => ({ downloadImageSource }))
vi.mock('../../lib/taskDeletion', () => ({ confirmTaskDeletion }))
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
  state.tasks = []
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
  it.each(['done', 'error', 'running'] as const)('opens task actions on a %s history placeholder without selecting the task', async (status) => {
    state.tasks = [{ id: 'batch-task', status, outputImages: ['one', 'two'] } as TaskRecord]
    const wrapper = mount(StudioImageContextMenu, { global: { stubs: { StudioIcon: true } } })
    const history = document.createElement('button')
    history.dataset.studioHistoryTaskId = 'batch-task'
    const placeholder = document.createElement('span')
    history.append(placeholder)
    document.body.append(history)
    placeholder.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 100, clientY: 100 }))
    await wrapper.vm.$nextTick()
    const remove = wrapper.get('[data-testid="delete-history-task"]')
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(remove.attributes('disabled') !== undefined).toBe(status === 'running')
    await remove.trigger('click')
    if (status === 'running') expect(confirmTaskDeletion).not.toHaveBeenCalled()
    else {
      expect(confirmTaskDeletion).toHaveBeenCalledWith('batch-task')
      expect(wrapper.find('[role="menu"]').exists()).toBe(false)
    }
    history.remove()
  })

  it('adds task deletion to history image actions while keeping other image menus unchanged', async () => {
    state.tasks = [{ id: 'task-one', status: 'done', outputImages: ['generated-image'] } as TaskRecord]
    const wrapper = mount(StudioImageContextMenu, { global: { stubs: { StudioIcon: true } } })
    const history = document.createElement('button')
    history.dataset.studioHistoryTaskId = 'task-one'
    const img = document.createElement('img')
    img.src = 'data:image/png;base64,thumbnail'
    history.append(img)
    document.body.append(history)
    img.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="delete-history-task"]').exists()).toBe(true)
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()
    expect(ensureImageCached).toHaveBeenCalledWith('generated-image')
    history.remove()
  })

  it('dismisses the menu with Escape', async () => {
    const { wrapper } = await openMenu('generated-image')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
  })

  it('closes the menu before an underlying lightbox when Escape is pressed', async () => {
    const closeLightbox = vi.fn()
    mount(defineComponent({
      setup() {
        const open = ref(true)
        useCloseOnEscape(() => open.value, () => { open.value = false; closeLightbox() })
        return () => null
      },
    }))
    const { wrapper } = await openMenu('generated-image')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
    expect(closeLightbox).not.toHaveBeenCalled()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(closeLightbox).toHaveBeenCalledTimes(1)
  })

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
