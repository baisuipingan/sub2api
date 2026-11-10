import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AnnouncementsView from '../AnnouncementsView.vue'
import type { Announcement } from '@/types'

const { list, create, update, getAll, showSuccess, showError } = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), getAll: vi.fn(), showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { announcements: { list, create, update }, groups: { getAll } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('@/composables/usePersistedPageSize', () => ({ getPersistedPageSize: () => 20 }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const row = (displayMode?: 'notice' | 'timeline'): Announcement => ({
  id: 7, title: 'Announcement', content: 'Existing details', status: 'active', notify_mode: 'silent',
  ...(displayMode ? { display_mode: displayMode } : {}),
  targeting: { any_of: [] }, created_at: '2026-11-10T00:00:00Z', updated_at: '2026-11-10T00:00:00Z'
})
const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  DataTable: {
    props: ['data', 'columns'],
    template: '<div><section v-for="item in data" :key="item.id"><slot name="cell-display_mode" :row="item" /><slot name="cell-actions" :row="item" /></section></div>'
  },
  Select: {
    props: ['modelValue', 'options'], emits: ['update:modelValue'],
    template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>'
  },
  Icon: true, Pagination: true, EmptyState: true, ConfirmDialog: true,
  AnnouncementTargetingEditor: true, AnnouncementReadStatusDialog: true, AnnouncementPopup: true
}
async function render(items: Announcement[] = []) {
  list.mockResolvedValue({ items, total: items.length, pages: 1, page: 1, page_size: 20 })
  const wrapper = mount(AnnouncementsView, { global: { stubs } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  getAll.mockResolvedValue([])
  create.mockResolvedValue(row('notice'))
  update.mockResolvedValue(row('timeline'))
})

describe('announcement display location', () => {
  it('defaults new announcements to timeline and sends notices without enabling popup reminders', async () => {
    const wrapper = await render()
    await wrapper.findAll('button').find(button => button.text().includes('admin.announcements.createAnnouncement'))!.trigger('click')
    expect((wrapper.get('#announcement-display-mode').element as HTMLSelectElement).value).toBe('timeline')
    await wrapper.get('#announcement-display-mode').setValue('notice')
    await wrapper.get('#announcement-form input[type="text"]').setValue('Support information')
    await wrapper.get('#announcement-form textarea').setValue('Contact our support team')
    await wrapper.get('#announcement-form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Support information', content: 'Contact our support team', display_mode: 'notice', notify_mode: 'silent'
    }))
    expect(update).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows the saved location and updates only the changed location', async () => {
    const wrapper = await render([row('notice')])
    expect(wrapper.text()).toContain('admin.announcements.displayModeLabels.notice')
    await wrapper.get('button[title="common.edit"]').trigger('click')
    expect((wrapper.get('#announcement-display-mode').element as HTMLSelectElement).value).toBe('notice')
    await wrapper.get('#announcement-display-mode').setValue('timeline')
    await wrapper.get('#announcement-form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(7, { display_mode: 'timeline' })
    expect(create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('treats legacy announcements as timeline without forcing a needless location update', async () => {
    const wrapper = await render([row()])
    expect(wrapper.text()).toContain('admin.announcements.displayModeLabels.timeline')
    await wrapper.get('button[title="common.edit"]').trigger('click')
    expect((wrapper.get('#announcement-display-mode').element as HTMLSelectElement).value).toBe('timeline')
    await wrapper.get('#announcement-form input[type="text"]').setValue('Corrected title')
    await wrapper.get('#announcement-form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(7, { title: 'Corrected title' })
    wrapper.unmount()
  })
})
