import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import WorkbenchGalleryImage from '../WorkbenchGalleryImage.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getWorkbenchGalleryImage } from '@/api/admin/workbench'
import { loadPrivateImageVariant, deletePrivateImageVariants } from '@/lib/privateImageVariantCache'

vi.mock('@/api/admin/workbench', () => ({ getWorkbenchGalleryImage: vi.fn() }))
vi.mock('@/lib/privateImageVariantCache', () => ({
  loadPrivateImageVariant: vi.fn(async (opts: { authorize: () => Promise<unknown> }) => opts.authorize()),
  deletePrivateImageVariants: vi.fn(async () => undefined),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('auth_token', 'test-viewer-session')
  localStorage.setItem('auth_user', JSON.stringify({ id: 9, role: 'admin' }))
})
afterEach(() => { vi.unstubAllGlobals(); localStorage.removeItem('auth_token'); localStorage.removeItem('auth_user') })

describe('administrator gallery image variants', () => {
  it('loads only a thumbnail until opened and never requests an original', async () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    vi.mocked(getWorkbenchGalleryImage).mockResolvedValue({ url: 'https://private-store.test/signed', expires_at: '2030-01-01', width: 100, height: 100, bytes: 100, mime: 'image/webp' })
    const task = { user_id: 7, user_email: 'user@example.test', task_id: 'same-task', model: 'gpt-image-2', created_at: '', output_count: 1, image_ids: ['image-one'] }
    const wrapper = mount(WorkbenchGalleryImage, { props: { task, imageId: 'image-one', number: 1 }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, LoadingSpinner: true } } })
    await flushPromises()
    expect(getWorkbenchGalleryImage).toHaveBeenCalledTimes(1)
    expect(loadPrivateImageVariant).toHaveBeenCalledWith(expect.objectContaining({ viewerId: 9, ownerId: 7, imageId: 'image-one', variant: 'thumbnail' }))
    const authorization = vi.mocked(loadPrivateImageVariant).mock.calls[0]![0]
    expect(authorization.isCurrent()).toBe(true)
    localStorage.setItem('auth_user', JSON.stringify({ id: 10, role: 'admin' }))
    expect(authorization.isCurrent()).toBe(false)
    localStorage.setItem('auth_user', JSON.stringify({ id: 9, role: 'admin' }))
    expect(getWorkbenchGalleryImage).toHaveBeenLastCalledWith(task, 'image-one', 'thumbnail', expect.any(AbortSignal))
    expect(wrapper.find('img').attributes('loading')).toBe('lazy')
    expect(wrapper.find('img').attributes('referrerpolicy')).toBe('no-referrer')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(getWorkbenchGalleryImage).toHaveBeenLastCalledWith(task, 'image-one', 'preview', expect.any(AbortSignal))
    expect(vi.mocked(getWorkbenchGalleryImage).mock.calls.some(call => String(call[2]) === 'original')).toBe(false)
  })

  it('aborts a closed preview and ignores stale responses after reopening', async () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    let resolveFirst: (value: { url: string; expires_at: string; width: number; height: number; bytes: number; mime: string }) => void = () => {}
    const first = new Promise<{ url: string; expires_at: string; width: number; height: number; bytes: number; mime: string }>((resolve) => { resolveFirst = resolve })
    const result = { url: 'https://private-store.test/thumbnail', expires_at: '2030-01-01', width: 100, height: 100, bytes: 100, mime: 'image/webp' }
    vi.mocked(getWorkbenchGalleryImage).mockReset()
    vi.mocked(getWorkbenchGalleryImage).mockResolvedValueOnce(result).mockReturnValueOnce(first).mockResolvedValueOnce({ ...result, url: 'https://private-store.test/fresh-preview' })
    const task = { user_id: 7, user_email: '', task_id: 'same-task', model: '', created_at: '', output_count: 1, image_ids: ['image-one'] }
    const wrapper = mount(WorkbenchGalleryImage, { props: { task, imageId: 'image-one', number: 1 }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, LoadingSpinner: true } } })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    const firstSignal = vi.mocked(getWorkbenchGalleryImage).mock.calls[1]![3]!
    wrapper.findComponent(BaseDialog).vm.$emit('close')
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('img').at(-1)!.attributes('src')).toBe('https://private-store.test/fresh-preview')
    resolveFirst({ ...result, url: 'https://private-store.test/stale-preview' })
    await flushPromises()
    expect(wrapper.findAll('img').at(-1)!.attributes('src')).toBe('https://private-store.test/fresh-preview')
    wrapper.unmount()
    expect(vi.mocked(getWorkbenchGalleryImage).mock.calls[2]![3]!.aborted).toBe(true)
  })

  it('re-signs a failed preview once without requesting the original', async () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    const result = { url: 'https://private-store.test/signed', expires_at: '2030-01-01', width: 100, height: 100, bytes: 100, mime: 'image/webp' }
    vi.mocked(getWorkbenchGalleryImage).mockReset()
    vi.mocked(getWorkbenchGalleryImage).mockResolvedValue(result)
    const task = { user_id: 7, user_email: '', task_id: 'same-task', model: '', created_at: '', output_count: 1, image_ids: ['image-one'] }
    const wrapper = mount(WorkbenchGalleryImage, { props: { task, imageId: 'image-one', number: 1 }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, LoadingSpinner: true } } })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    await wrapper.findAll('img').at(-1)!.trigger('error')
    await flushPromises()
    expect(vi.mocked(getWorkbenchGalleryImage).mock.calls.map(call => call[2])).toEqual(['thumbnail', 'preview', 'preview'])
    await wrapper.findAll('img').at(-1)!.trigger('error')
    await flushPromises()
    expect(vi.mocked(getWorkbenchGalleryImage)).toHaveBeenCalledTimes(3)
    expect(deletePrivateImageVariants).toHaveBeenCalledWith(9, 7, ['image-one'])
    expect(wrapper.text()).toContain('common.tryAgain')
  })
})
