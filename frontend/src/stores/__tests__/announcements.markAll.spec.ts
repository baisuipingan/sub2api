import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAnnouncementStore } from '../announcements'
import type { UserAnnouncement } from '@/types'

const { summary, markRead, markAllRead } = vi.hoisted(() => ({ summary: vi.fn(), markRead: vi.fn(), markAllRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { summary, markRead, markAllRead } }))
const announcement = (id: number, display_mode: 'notice' | 'timeline' = 'timeline'): UserAnnouncement => ({ id, title: `Notice ${id}`, content: 'Content', notify_mode: 'silent', display_mode, created_at: '2026-09-15T00:00:00Z', updated_at: '2026-09-15T00:00:00Z' })
const state = (count: number, token = 'snapshot') => ({ timeline_unread_count: count, timeline_count: count, notice_count: 1, snapshot: token, popup_candidates: [] })
beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks(); summary.mockResolvedValue(state(2)); vi.spyOn(console, 'error').mockImplementation(() => {}) })
afterEach(() => vi.restoreAllMocks())

describe('announcement read confirmations', () => {
  it('uses one server snapshot request and only marks confirmed IDs, leaving new items unread', async () => {
    const store = useAnnouncementStore(); await store.fetchAnnouncements()
    store.announcements = [announcement(1)]; store.notices = [announcement(9, 'notice')]
    let finish!: (value: unknown) => void
    markAllRead.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const action = store.markAllAsRead(); store.announcements.push(announcement(2))
    finish({ marked_ids: [1], summary: state(1, 'new-snapshot') }); await action
    expect(markAllRead.mock.calls).toEqual([['snapshot']]); expect(markRead).not.toHaveBeenCalled()
    expect(store.announcements[0].read_at).toBeTruthy(); expect(store.announcements[1].read_at).toBeFalsy(); expect(store.notices[0].read_at).toBeFalsy()
    expect(store.unreadCount).toBe(1); expect(store.snapshot).toBe('new-snapshot'); expect(store.markingAll).toBe(false)
  })
  it('does not change read state after a failed bulk request and allows a retry', async () => {
    const store = useAnnouncementStore(); await store.fetchAnnouncements(); store.announcements = [announcement(1)]
    const error = new Error('offline'); markAllRead.mockRejectedValueOnce(error)
    await expect(store.markAllAsRead()).rejects.toBe(error)
    expect(store.announcements[0].read_at).toBeFalsy(); expect(store.unreadCount).toBe(2); expect(store.markingAll).toBe(false)
    markAllRead.mockResolvedValueOnce({ marked_ids: [1], summary: state(0) }); await store.markAllAsRead(); expect(store.unreadCount).toBe(0)
  })
  it('does not let an old user bulk response change the new session', async () => {
    const store = useAnnouncementStore(); await store.fetchAnnouncements()
    let finish!: (value: unknown) => void; markAllRead.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const action = store.markAllAsRead(); store.reset(); store.announcements = [announcement(2)]
    finish({ marked_ids: [2], summary: state(20) }); await action
    expect(store.announcements[0].read_at).toBeFalsy(); expect(store.unreadCount).toBe(0)
  })
  it('confirms single reads only after success and never sorts the timeline after reading', async () => {
    const store = useAnnouncementStore(); await store.fetchAnnouncements(); store.announcements = [announcement(2), announcement(1)]
    markRead.mockRejectedValueOnce(new Error('offline')); expect(await store.markAsRead(2)).toBe(false)
    expect(store.announcements[0].read_at).toBeFalsy()
    summary.mockResolvedValue(state(1)); markRead.mockResolvedValueOnce(undefined)
    expect(await store.markAsRead(2)).toBe(true)
    expect(store.announcements.map(item => item.id)).toEqual([2, 1]); expect(store.announcements[0].read_at).toBeTruthy(); expect(store.unreadCount).toBe(1)
  })
  it('keeps an unconfirmed popup visible on failure and removes it from later queues after a read', async () => {
    const store = useAnnouncementStore(); const popup = { ...announcement(1), notify_mode: 'popup' as const }
    summary.mockResolvedValue({ ...state(1), popup_candidates: [popup] }); await store.fetchAnnouncements()
    markRead.mockRejectedValueOnce(new Error('offline')); expect(await store.dismissPopup()).toBe(false); expect(store.currentPopup?.id).toBe(1)
    markRead.mockResolvedValueOnce(undefined); expect(await store.dismissPopup()).toBe(true); expect(store.currentPopup).toBeNull()
    await store.fetchAnnouncements(true); expect(store.currentPopup).toBeNull()
  })
  it('refreshes an expired snapshot but waits for a second explicit confirmation before marking anything', async () => {
    const store = useAnnouncementStore(); await store.fetchAnnouncements(); store.announcements = [announcement(1)]
    const error = { response: { data: { reason: 'ANNOUNCEMENT_SNAPSHOT_INVALID' } } }
    markAllRead.mockRejectedValueOnce(error)
    summary.mockResolvedValueOnce(state(3, 'refreshed-snapshot'))
    await expect(store.markAllAsRead()).rejects.toBe(error)
    expect(markAllRead).toHaveBeenCalledOnce(); expect(store.snapshot).toBe('refreshed-snapshot')
    expect(store.announcements[0].read_at).toBeFalsy()
    markAllRead.mockResolvedValueOnce({ marked_ids: [1], summary: state(0, 'confirmed') })
    await store.markAllAsRead()
    expect(markAllRead.mock.calls).toEqual([['snapshot'], ['refreshed-snapshot']])
    expect(store.announcements[0].read_at).toBeTruthy()
  })

})
