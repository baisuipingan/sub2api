import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import StudioSquarePreview from '../StudioSquarePreview.vue'
import type { TaskRecord } from '../../types'

const { ensureImagePreviewUrl, getCachedPreviewUrl, setLightboxImageId } = vi.hoisted(() => ({ ensureImagePreviewUrl: vi.fn(), getCachedPreviewUrl: vi.fn(), setLightboxImageId: vi.fn() }))
vi.mock('../../lib/imageCache', () => ({ ensureImagePreviewUrl, getCachedPreviewUrl }))
vi.mock('../../stores/store', () => ({ storeState: { streamPreviews: {}, setLightboxImageId } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const images = ['image-1', 'image-2', 'image-3', 'image-4', 'image-5']
const task = { id: 'batch-task', status: 'done', outputImages: images } as TaskRecord
beforeEach(() => {
  vi.clearAllMocks()
  getCachedPreviewUrl.mockReturnValue(undefined)
  ensureImagePreviewUrl.mockResolvedValue('data:image/png;base64,aW1hZ2U=')
})

describe('StudioSquarePreview batch navigation', () => {
  it('keeps both edge navigation buttons visible, including a disabled first/last position', async () => {
    const wrapper = mount(StudioSquarePreview, { props: { task, imageId: images[0] } })
    await flushPromises()
    const previous = wrapper.get('button[aria-label="imageStudio.result.prevImage"]')
    const next = wrapper.get('button[aria-label="imageStudio.result.nextImage"]')
    expect(previous.attributes('disabled')).toBeDefined()
    expect(next.attributes('disabled')).toBeUndefined()
    expect(previous.classes()).not.toContain('disabled:opacity-0')
    await next.trigger('click')
    expect(wrapper.emitted('selectImage')).toEqual([[1]])
    await wrapper.setProps({ imageId: images[4] })
    expect(previous.attributes('disabled')).toBeUndefined()
    expect(next.attributes('disabled')).toBeDefined()
  })

  it('keeps the full batch when opening the currently selected image in the lightbox', async () => {
    const wrapper = mount(StudioSquarePreview, { props: { task, imageId: images[3] } })
    await flushPromises()
    await wrapper.get('button[aria-label="imageStudio.result.zoomAria"]').trigger('click')
    expect(setLightboxImageId).toHaveBeenCalledWith(images[3], images)
  })

  it('never lets an older asynchronous preview read overwrite the selected image', async () => {
    const reads = new Map<string, (source: string) => void>()
    ensureImagePreviewUrl.mockImplementation((id: string) => new Promise<string>((resolve) => reads.set(id, resolve)))
    const wrapper = mount(StudioSquarePreview, { props: { task, imageId: images[0] } })
    await wrapper.setProps({ imageId: images[1] })
    reads.get(images[1])!('data:image/png;base64,newer')
    await flushPromises()
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,newer')
    reads.get(images[0])!('data:image/png;base64,older')
    await flushPromises()
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,newer')
    expect(wrapper.get('img').attributes('data-image-id')).toBe(images[1])
  })

  it('does not reserve navigation gutters for a single image', async () => {
    const wrapper = mount(StudioSquarePreview, { props: { task: { ...task, outputImages: [images[0]] }, imageId: images[0] } })
    await flushPromises()
    expect(wrapper.classes()).toContain('studio-preview-single')
    expect(wrapper.find('.studio-preview-arrow').exists()).toBe(false)
  })
})
