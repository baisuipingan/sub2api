import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAnnouncementStore } from '../announcements'
import type { UserAnnouncement } from '@/types'
import type { AnnouncementSummary } from '@/api/announcements'

const { summary, center, markRead } = vi.hoisted(() => ({ summary: vi.fn(), center: vi.fn(), markRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { summary, center, markRead } }))
const notice = (id: number): UserAnnouncement => ({ id, title: `Notice ${id}`, content: 'Content', notify_mode: 'popup', display_mode: 'timeline', created_at: '2026-09-18T00:00:00Z', updated_at: '2026-09-18T00:00:00Z' })
const result = (items: UserAnnouncement[] = [], unread = items.length): AnnouncementSummary => ({ notice_count: 0, timeline_count: unread, timeline_unread_count: unread, snapshot: `snapshot-${unread}`, popup_candidates: items })
function pending<T>() { let resolve!: (value: T) => void; let reject!: (error: Error) => void; const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }

beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks()
  vi.spyOn(console, 'error').mockImplementation(() => {})
  center.mockResolvedValue({ items: [], total: 0, page: 1, pages: 0, page_size: 20 })
})
afterEach(() => vi.restoreAllMocks())

describe('announcement summary ownership and complete counts', () => {
  it('does not restore old popups after logout', async () => {
    const store = useAnnouncementStore(); const old = pending<AnnouncementSummary>()
    summary.mockReturnValueOnce(old.promise)
    const request = store.fetchAnnouncements(); store.reset(); old.resolve(result([notice(1)])); await request
    expect(store.currentPopup).toBeNull(); expect(store.unreadCount).toBe(0); expect(store.loading).toBe(false)
  })
  it('keeps the new session loading while an old request finishes', async () => {
    const store = useAnnouncementStore(); const old = pending<AnnouncementSummary>(); const next = pending<AnnouncementSummary>()
    summary.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const first = store.fetchAnnouncements(); store.reset(); const second = store.fetchAnnouncements()
    old.resolve(result()); await first; expect(store.loading).toBe(true)
    next.resolve(result([notice(2)], 82)); await second
    expect(store.currentPopup?.id).toBe(2); expect(store.unreadCount).toBe(82); expect(store.loading).toBe(false)
  })
  it('keeps forced refreshes and retries failed requests without breaking throttling', async () => {
    const store = useAnnouncementStore(); const old = pending<AnnouncementSummary>()
    summary.mockReturnValueOnce(old.promise).mockResolvedValue(result([notice(2)], 100))
    const first = store.fetchAnnouncements(); await store.fetchAnnouncements(true); old.resolve(result([notice(1)])); await first
    await store.fetchAnnouncements(); expect(summary).toHaveBeenCalledTimes(2); expect(store.currentPopup?.id).toBe(2); expect(store.unreadCount).toBe(100)
    summary.mockRejectedValueOnce(new Error('offline')); await store.fetchAnnouncements(true)
    summary.mockResolvedValueOnce(result([], 101)); await store.fetchAnnouncements(); expect(store.unreadCount).toBe(101)
  })
  it('suspends active and arriving popups while the center is open and resumes without marking them read', async () => {
    const store = useAnnouncementStore(); summary.mockResolvedValue(result([notice(1), notice(2)], 28))
    await store.fetchAnnouncements(); expect(store.currentPopup?.id).toBe(1)
    await store.openCenter(); expect(store.centerOpen).toBe(true); expect(markRead).not.toHaveBeenCalled()
    expect(store.currentPopup?.id).toBe(1); expect(store.unreadCount).toBe(28)
    store.closeCenter(); expect(store.currentPopup?.id).toBe(1)
  })
  it('refreshes notices on opening and invalidates timeline pages for the next visit', async () => {
    const store = useAnnouncementStore(); summary.mockResolvedValue(result([], 45))
    center.mockImplementation(async (tab: string, page: number) => ({ items: tab === 'timeline' ? [notice(page)] : [], total: 45, page, pages: 3, page_size: 20 }))
    await store.openCenter(); expect(store.centerTab).toBe('notice')
    await store.selectCenterTab('timeline'); await store.loadCenterPage('timeline', true)
    expect(store.announcements.map(item => item.id)).toEqual([1, 2]); expect(store.unreadCount).toBe(45)
    store.closeCenter(); await store.openCenter(); await store.selectCenterTab('timeline')
    expect(center.mock.calls.filter(call => call[0] === 'timeline').map(call => call[1])).toEqual([1, 2, 1])
  })
  it('ignores stale pages after a new refresh and after switching accounts', async () => {
    const store = useAnnouncementStore(); const old = pending<{ items: UserAnnouncement[]; total: number; page: number; pages: number; page_size: number }>()
    center.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ items: [notice(2)], total: 1, page: 1, pages: 1, page_size: 20 })
    const first = store.loadCenterPage('timeline'); await store.loadCenterPage('timeline')
    old.resolve({ items: [notice(1)], total: 1, page: 1, pages: 1, page_size: 20 }); await first
    expect(store.announcements.map(item => item.id)).toEqual([2])
    const previous = pending<{ items: UserAnnouncement[]; total: number; page: number; pages: number; page_size: number }>(); center.mockReturnValueOnce(previous.promise)
    const request = store.loadCenterPage('notice'); store.reset(); previous.resolve({ items: [notice(1)], total: 1, page: 1, pages: 1, page_size: 20 }); await request
    expect(store.notices).toEqual([]); expect(store.noticeLoading).toBe(false)
  })
  it('invalidates a previous pending next page before opening a fresh center', async () => {
    const store = useAnnouncementStore(); summary.mockResolvedValue(result([], 45))
    center.mockImplementation(async (tab: string) => ({ items: tab === 'timeline' ? [notice(1)] : [], total: 45, page: 1, pages: 3, page_size: 20 }))
    await store.openCenter(); await store.selectCenterTab('timeline')
    const old = pending<{ items: UserAnnouncement[]; total: number; page: number; pages: number; page_size: number }>()
    center.mockReturnValueOnce(old.promise)
    const append = store.loadCenterPage('timeline', true)
    store.closeCenter(); await store.openCenter()
    old.resolve({ items: [notice(22)], total: 45, page: 2, pages: 3, page_size: 20 }); await append
    expect(store.announcements).toEqual([]); expect(store.timelinePage).toBe(0)
    await store.selectCenterTab('timeline')
    expect(store.announcements.map(item => item.id)).toEqual([1]); expect(store.timelinePage).toBe(1)
  })

})
