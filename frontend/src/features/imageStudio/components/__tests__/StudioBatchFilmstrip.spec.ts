import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import StudioBatchFilmstrip from '../StudioBatchFilmstrip.vue'

const { thumbnail, original, subscribe } = vi.hoisted(() => ({
  thumbnail: vi.fn(), original: vi.fn(), subscribe: vi.fn(() => () => {}),
}))
vi.mock('../../lib/imageCache', () => ({ ensureImageThumbnailCached: thumbnail, ensureImageCached: original, subscribeImageThumbnail: subscribe }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))
enableAutoUnmount(afterEach)

const images = ['image-1', 'image-2', 'image-3', 'image-4', 'image-5']
beforeEach(() => {
  vi.clearAllMocks()
  thumbnail.mockResolvedValue({ dataUrl: 'data:image/webp;base64,dGh1bWI=' })
})
afterEach(() => vi.restoreAllMocks())

describe('StudioBatchFilmstrip', () => {
  it('renders only this batch with thumbnail cache reads and an accessible selected image', async () => {
    const wrapper = mount(StudioBatchFilmstrip, { props: { images, selectedImageId: images[2] } })
    await flushPromises()

    expect(wrapper.findAll('[role="option"]')).toHaveLength(5)
    expect(wrapper.findAll('[role="option"]').map((button) => button.attributes('aria-selected'))).toEqual(['false', 'false', 'true', 'false', 'false'])
    expect(wrapper.findAll('[role="option"]')[2].attributes('tabindex')).toBe('0')
    expect(wrapper.text()).toContain('"index":3,"total":5')
    expect(thumbnail.mock.calls.map(([id]) => id)).toEqual(images)
    expect(original).not.toHaveBeenCalled()
    expect(wrapper.get('[role="listbox"]').element.scrollLeft).toBe(0)
  })

  it('selects the clicked batch image and scrolls changes into view', async () => {
    const wrapper = mount(StudioBatchFilmstrip, { props: { images, selectedImageId: images[0] } })
    await wrapper.findAll('[role="option"]')[4].trigger('click')
    expect(wrapper.emitted('selectImage')).toEqual([[4]])
    vi.spyOn(wrapper.get('[role="listbox"]').element, 'getBoundingClientRect').mockReturnValue({ left: 0, right: 100 } as DOMRect)
    vi.spyOn(wrapper.findAll('[role="option"]')[4].element, 'getBoundingClientRect').mockReturnValue({ left: 200, right: 260 } as DOMRect)
    await wrapper.setProps({ selectedImageId: images[4] })
    await flushPromises()
    expect(wrapper.findAll('[role="option"]')[4].attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[role="listbox"]').element.scrollLeft).toBe(160)
  })

  it('supports arrows, Home and End with roving focus and bounded navigation', async () => {
    const wrapper = mount(StudioBatchFilmstrip, { attachTo: document.body, props: { images, selectedImageId: images[0] } })
    await wrapper.findAll('button')[0].trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('selectImage')?.[0]).toEqual([1])
    expect(document.activeElement).toBe(wrapper.findAll('button')[1].element)
    await wrapper.setProps({ selectedImageId: images[1] })
    await wrapper.findAll('button')[1].trigger('keydown', { key: 'End' })
    expect(wrapper.emitted('selectImage')?.[1]).toEqual([4])
    await wrapper.setProps({ selectedImageId: images[4] })
    await wrapper.findAll('button')[4].trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('selectImage')?.[2]).toEqual([4])
    await wrapper.findAll('button')[4].trigger('keydown', { key: 'Home' })
    expect(wrapper.emitted('selectImage')?.[3]).toEqual([0])
  })

  it.each([{ ids: [] }, { ids: ['only-image'] }])('does not occupy a rail for a single or empty result', ({ ids }) => {
    const wrapper = mount(StudioBatchFilmstrip, { props: { images: ids, selectedImageId: ids[0] ?? null } })
    expect(wrapper.find('[data-testid="batch-filmstrip"]').exists()).toBe(false)
    expect(thumbnail).not.toHaveBeenCalled()
  })
})
