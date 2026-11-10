import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get, post } }))
import announcementsAPI from '../announcements'
beforeEach(() => { vi.resetAllMocks() })
describe('announcement center API', () => {
  it('uses independent summary counts and paginates the selected display mode', async () => {
    get.mockResolvedValue({ data: { items: [], total: 30, page: 2, pages: 2, page_size: 20 } })
    const signal = new AbortController().signal
    await announcementsAPI.summary(signal)
    expect(get).toHaveBeenNthCalledWith(1, '/announcements/summary', { signal })
    await announcementsAPI.center('timeline', 2, signal)
    expect(get).toHaveBeenNthCalledWith(2, '/announcements/center', { params: { display_mode: 'timeline', page: 2, page_size: 20 }, signal })
  })
  it('posts the opaque snapshot exactly once and preserves the confirmed read IDs and summary', async () => {
    const result = { marked_ids: [1, 2], summary: { timeline_unread_count: 1, snapshot: 'next' } }
    post.mockResolvedValue({ data: result })
    expect(await announcementsAPI.markAllRead('opaque-user-snapshot')).toBe(result)
    expect(post).toHaveBeenCalledWith('/announcements/read-all', { snapshot: 'opaque-user-snapshot' })
  })
})
