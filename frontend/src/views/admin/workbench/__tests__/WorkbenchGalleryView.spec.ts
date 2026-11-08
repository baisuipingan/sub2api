import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import WorkbenchGalleryView from '../WorkbenchGalleryView.vue'
import { listWorkbenchGallery } from '@/api/admin/workbench'
import i18n from '@/i18n'

vi.mock('@/api/admin/workbench', () => ({ listWorkbenchGallery: vi.fn() }))
enableAutoUnmount(afterEach)

describe('administrator cloud gallery filters', () => {
  it('opens task links with exact user and task scope, including imported results', async () => {
    vi.mocked(listWorkbenchGallery).mockResolvedValue({ items: [{ user_id: 7, user_email: 'user@example.test', task_id: 'imported-one', model: 'gpt-image-2', created_at: '2026-11-08T08:00:00Z', output_count: 1, image_ids: ['output-one'] }], page: 1, page_size: 20, total: 1, pages: 1 })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/workbench/gallery', component: WorkbenchGalleryView }] })
    await router.push('/admin/workbench/gallery?user_id=7&task_id=imported-one')
    await router.isReady()
    const wrapper = mount(WorkbenchGalleryView, { global: { plugins: [router, i18n], stubs: { AppLayout: { template: '<div><slot /></div>' }, WorkbenchGalleryImage: { template: '<div data-testid="thumbnail" />' } } } })
    await flushPromises()
    expect(listWorkbenchGallery).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 7, task_id: 'imported-one' }, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('user@example.test')
    expect(wrapper.findAll('[data-testid="thumbnail"]')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('prompt')
  })
})
