/** User announcement endpoints. The legacy list/read API remains supported. */
import { apiClient } from './client'
import type { UserAnnouncement, PaginatedResponse } from '@/types'

export type AnnouncementCenterTab = 'notice' | 'timeline'
export interface AnnouncementSummary {
  notice_count: number
  timeline_count: number
  timeline_unread_count: number
  snapshot: string
  popup_candidates: UserAnnouncement[]
}
export interface AnnouncementReadAllResult {
  marked_ids: number[]
  summary: AnnouncementSummary
}

export async function list(unreadOnly = false): Promise<UserAnnouncement[]> {
  const { data } = await apiClient.get<UserAnnouncement[]>('/announcements', {
    params: unreadOnly ? { unread_only: 1 } : {}
  })
  return data
}
export async function summary(signal?: AbortSignal): Promise<AnnouncementSummary> {
  const { data } = await apiClient.get<AnnouncementSummary>('/announcements/summary', { signal })
  return data
}
export async function center(displayMode: AnnouncementCenterTab, page = 1, signal?: AbortSignal): Promise<PaginatedResponse<UserAnnouncement>> {
  const { data } = await apiClient.get<PaginatedResponse<UserAnnouncement>>('/announcements/center', {
    params: { display_mode: displayMode, page, page_size: 20 }, signal
  })
  return data
}
export async function markRead(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>(`/announcements/${id}/read`)
  return data
}
export async function markAllRead(snapshot: string): Promise<AnnouncementReadAllResult> {
  const { data } = await apiClient.post<AnnouncementReadAllResult>('/announcements/read-all', { snapshot })
  return data
}

export default { list, summary, center, markRead, markAllRead }
