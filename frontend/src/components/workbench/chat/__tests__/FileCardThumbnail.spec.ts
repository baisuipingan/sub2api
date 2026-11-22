import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import FileCardThumbnail from '../FileCardThumbnail.vue'

const { access } = vi.hoisted(() => ({ access: vi.fn() }))
const session = reactive<{ user: { id: number } | null }>({ user: { id: 7 } })
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: { access } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => session }))

const original = 'https://private-storage.test/original'
const preview = 'https://private-storage.test/thumbnail'
let intersect: () => void

beforeEach(() => {
  session.user = { id: 7 }
  access.mockReset()
  vi.stubGlobal('IntersectionObserver', class {
    constructor(callback: IntersectionObserverCallback) {
      intersect = () => callback([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
    }
    observe() {}
    disconnect() {}
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('private file card thumbnail', () => {
  it('loads only the image preview when the card reaches the viewport', async () => {
    access.mockResolvedValue({ url: original, preview_url: preview, preview_kind: 'image' })
    const wrapper = mount(FileCardThumbnail, { props: { versionId: 'image-version' } })
    expect(access).not.toHaveBeenCalled()
    intersect()
    await flushPromises()
    expect(access).toHaveBeenCalledWith('image-version', expect.any(AbortSignal))
    expect(wrapper.get('img').attributes('src')).toBe(preview)
    wrapper.unmount()
  })

  it('keeps an icon when a small preview is missing', async () => {
    access.mockResolvedValue({ url: original, preview_kind: 'image' })
    const wrapper = mount(FileCardThumbnail, { props: { versionId: 'no-thumbnail' } })
    intersect()
    await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores a signing response that arrives after logout', async () => {
    let finish!: (value: object) => void
    access.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(FileCardThumbnail, { props: { versionId: 'image-version' } })
    intersect()
    session.user = null
    finish({ url: original, preview_url: preview, preview_kind: 'image' })
    await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false)
    wrapper.unmount()
  })
})
