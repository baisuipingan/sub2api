import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import WorkbenchGalleryView from '../WorkbenchGalleryView.vue'
import { listWorkbenchGallery } from '@/api/admin/workbench'
import i18n from '@/i18n'
import { searchUsers } from '@/api/admin/usage'
import { getById } from '@/api/admin/users'
import UserSearchSelect from '@/components/admin/UserSearchSelect.vue'

vi.mock('@/api/admin/usage', async importOriginal => ({ ...await importOriginal<typeof import('@/api/admin/usage')>(), searchUsers: vi.fn() }))
vi.mock('@/api/admin/users', async importOriginal => ({ ...await importOriginal<typeof import('@/api/admin/users')>(), getById: vi.fn() }))

vi.mock('@/api/admin/workbench', () => ({ listWorkbenchGallery: vi.fn() }))
enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())

describe('administrator cloud gallery filters', () => {
  it('opens task links with exact user and task scope, including imported results', async () => {
    vi.mocked(getById).mockResolvedValue({ id: 7, username: '图库昵称', email: 'user@example.test' } as Awaited<ReturnType<typeof getById>>)
    vi.mocked(searchUsers).mockResolvedValue([])
    vi.mocked(listWorkbenchGallery).mockResolvedValue({ items: [{ user_id: 7, user_email: 'user@example.test', task_id: 'imported-one', model: 'gpt-image-2', created_at: '2026-11-08T08:00:00Z', output_count: 1, image_ids: ['output-one'] }], page: 1, page_size: 20, total: 1, pages: 1 })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/workbench/gallery', component: WorkbenchGalleryView }] })
    await router.push('/admin/workbench/gallery?user_id=7&task_id=imported-one')
    await router.isReady()
    const wrapper = mount(WorkbenchGalleryView, { global: { plugins: [router, i18n], stubs: { AppLayout: { template: '<div><slot /></div>' }, WorkbenchGalleryImage: { template: '<div data-testid="thumbnail" />' } } } })
    await flushPromises()
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 7, task_id: 'imported-one' }, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('user@example.test')
    expect(getById).toHaveBeenCalledWith(7, true)
    expect((wrapper.get('#gallery-user').element as HTMLInputElement).value).toContain('user@example.test')
    expect(wrapper.findAll('[data-testid="thumbnail"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('prompt')
  })

  it('selects a searched nickname as an exact user filter and clears or resets only once', async () => {
    vi.useFakeTimers()
    vi.mocked(listWorkbenchGallery).mockReset().mockResolvedValue({ items: [], page: 1, page_size: 20, total: 0, pages: 0 })
    vi.mocked(searchUsers).mockReset().mockResolvedValue([{ id: 42, username: '图库昵称', email: 'nickname@example.test', deleted: false }])
    vi.mocked(getById).mockReset()
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/workbench/gallery', component: WorkbenchGalleryView }] })
    await router.push('/admin/workbench/gallery')
    await router.isReady()
    const wrapper = mount(WorkbenchGalleryView, { global: { plugins: [router, i18n], stubs: { AppLayout: { template: '<div><slot /></div>' }, WorkbenchGalleryImage: true } } })
    await flushPromises()
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(1)

    const picker = wrapper.findComponent(UserSearchSelect)
    await picker.get('input').setValue('图库昵称')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(searchUsers).toHaveBeenLastCalledWith('图库昵称')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(1)

    await picker.get('[role="option"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ user_id: '42' })
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(2)
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 42 }, expect.any(AbortSignal))
    expect(picker.get('input').element.value).toBe('图库昵称 · nickname@example.test')

    await picker.get('button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(3)
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20 }, expect.any(AbortSignal))

    await picker.get('input').setValue('图库昵称')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    await picker.get('[role="option"]').trigger('click')
    await flushPromises()
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(4)
    const resetButton = wrapper.findAll('form button').find(button => button.text() === i18n.global.t('common.reset'))
    expect(resetButton).toBeDefined()
    await resetButton!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    expect(picker.get('input').element.value).toBe('')
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(5)
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20 }, expect.any(AbortSignal))
  })

  it('restores browser history scope instead of letting an unfinished user query block it', async () => {
    vi.useFakeTimers()
    vi.mocked(listWorkbenchGallery).mockReset().mockResolvedValue({ items: [], page: 1, page_size: 20, total: 0, pages: 0 })
    vi.mocked(searchUsers).mockReset().mockResolvedValue([])
    vi.mocked(getById).mockReset().mockResolvedValue({ id: 7, username: '图库昵称', email: 'user@example.test' } as Awaited<ReturnType<typeof getById>>)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/workbench/gallery', component: WorkbenchGalleryView }] })
    await router.push('/admin/workbench/gallery')
    await router.isReady()
    const wrapper = mount(WorkbenchGalleryView, { global: { plugins: [router, i18n], stubs: { AppLayout: { template: '<div><slot /></div>' }, WorkbenchGalleryImage: true } } })
    await flushPromises()
    await router.push('/admin/workbench/gallery?user_id=7&task_id=old-task')
    await flushPromises()
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(2)
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 7, task_id: 'old-task' }, expect.any(AbortSignal))

    const picker = wrapper.findComponent(UserSearchSelect)
    await picker.get('input').setValue('未选择的新昵称')
    expect(picker.vm.hasUnselectedQuery()).toBe(true)
    router.back()
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    expect(picker.get('input').element.value).toBe('')
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(3)
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20 }, expect.any(AbortSignal))

    router.forward()
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ user_id: '7', task_id: 'old-task' })
    expect(picker.get('input').element.value).toBe('图库昵称 · user@example.test')
    expect(listWorkbenchGallery).toHaveBeenCalledTimes(4)
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 7, task_id: 'old-task' }, expect.any(AbortSignal))
    await vi.advanceTimersByTimeAsync(300)
    expect(searchUsers).not.toHaveBeenCalled()
  })
})
