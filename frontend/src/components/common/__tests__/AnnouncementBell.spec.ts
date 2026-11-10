import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AnnouncementBell from '../AnnouncementBell.vue'
import { useAnnouncementStore } from '@/stores/announcements'

const { summary, center, markRead, markAllRead, showError } = vi.hoisted(() => ({ summary: vi.fn(), center: vi.fn(), markRead: vi.fn(), markAllRead: vi.fn(), showError: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { summary, center, markRead, markAllRead } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
enableAutoUnmount(afterEach)
const item = (id: number, title: string, content: string) => ({ id, title, content, notify_mode: 'silent', display_mode: 'timeline', created_at: '2026-09-30', updated_at: '2026-09-30' })
beforeEach(() => {
  setActivePinia(createPinia()); vi.clearAllMocks(); vi.spyOn(console, 'error').mockImplementation(() => {})
  summary.mockResolvedValue({ timeline_unread_count: 45, notice_count: 1, timeline_count: 45, snapshot: 'snapshot', popup_candidates: [] })
  center.mockImplementation(async (tab: string) => ({ items: tab === 'notice'
    ? [{ ...item(9, 'Permanent notice', 'Contact **support**'), display_mode: 'notice' }]
    : [item(2, 'New platform update', 'First full update'), item(1, 'Older update', 'Second full update')], total: tab === 'notice' ? 1 : 45, page: 1, pages: 1, page_size: 20 }))
})
afterEach(() => { vi.restoreAllMocks(); document.body.classList.remove('modal-open') })
const mountBell = () => mount(AnnouncementBell, { global: { stubs: { Teleport: true, Transition: true, Icon: true } } })

describe('announcement center', () => {
  it('defaults to full notices and only renders timeline bodies when explicitly expanded', async () => {
    const wrapper = mountBell(); await wrapper.get('button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('Permanent notice'); expect(wrapper.html()).toContain('<strong>support</strong>')
    expect(wrapper.text()).not.toContain('First full update'); expect(wrapper.get('#announcements-notice-tab').attributes('aria-selected')).toBe('true')
    await wrapper.get('#announcements-timeline-tab').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('45'); expect(wrapper.text()).toContain('New platform update'); expect(wrapper.text()).not.toContain('First full update')
    const toggles = () => wrapper.findAll('button[aria-expanded]')
    await toggles()[0].trigger('click'); await toggles()[1].trigger('click')
    expect(wrapper.text()).toContain('First full update'); expect(wrapper.text()).toContain('Second full update'); expect(markRead).not.toHaveBeenCalled()
  })
  it('keeps failed reads retryable, confirms success without closing the center or moving the item', async () => {
    const wrapper = mountBell(); await wrapper.get('button').trigger('click'); await flushPromises(); await wrapper.get('#announcements-timeline-tab').trigger('click'); await flushPromises()
    await wrapper.findAll('button[aria-expanded]')[0].trigger('click')
    const confirm = () => wrapper.findAll('button').find(button => button.text().includes('announcements.markRead'))!
    markRead.mockRejectedValueOnce(new Error('offline')); await confirm().trigger('click'); await flushPromises()
    expect(showError).toHaveBeenCalledWith('announcements.readFailed'); expect(wrapper.text()).toContain('First full update')
    markRead.mockResolvedValueOnce(undefined); await confirm().trigger('click'); await flushPromises()
    expect(useAnnouncementStore().announcements.map(item => item.id)).toEqual([2, 1]); expect(useAnnouncementStore().centerOpen).toBe(true)
    expect(wrapper.text()).toContain('First full update'); expect(wrapper.findAll('button').filter(button => button.text().includes('announcements.markRead'))).toHaveLength(0)
  })
  it('does not clear unread items on tab changes or closing and supports keyboard tabs', async () => {
    const wrapper = mountBell(); await wrapper.get('button').trigger('click'); await flushPromises()
    await wrapper.get('#announcements-notice-tab').trigger('keydown', { key: 'ArrowRight' }); await flushPromises()
    expect(wrapper.get('#announcements-timeline-tab').attributes('aria-selected')).toBe('true')
    const close = wrapper.findAll('button').find(button => button.text() === 'common.close')!; await close.trigger('click')
    expect(useAnnouncementStore().centerOpen).toBe(false); expect(markRead).not.toHaveBeenCalled(); expect(markAllRead).not.toHaveBeenCalled()
  })
})
