import { apiClient } from '../client'
import type {
  BasePaginationResponse,
  EventCategory,
  EventImportBatch,
  EventImportEnvelope,
  EventMapSettings,
  EventSource,
  EventStatus,
  EventWriteRequest,
  TeamEvent,
} from '@/types'

export interface AdminEventFilters {
  status?: string
  category?: string
  search?: string
  city?: string
  district?: string
  fee_type?: string
  from?: string
  to?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export async function list(page = 1, pageSize = 20, filters: AdminEventFilters = {}, signal?: AbortSignal): Promise<BasePaginationResponse<TeamEvent>> {
  const { data } = await apiClient.get<BasePaginationResponse<TeamEvent>>('/admin/events', {
    params: { page, page_size: pageSize, ...filters },
    signal,
  })
  return data
}

export async function getById(id: number): Promise<TeamEvent> {
  const { data } = await apiClient.get<TeamEvent>(`/admin/events/${id}`)
  return data
}

export async function create(payload: EventWriteRequest): Promise<TeamEvent> {
  const { data } = await apiClient.post<TeamEvent>('/admin/events', payload)
  return data
}

export async function update(id: number, payload: EventWriteRequest): Promise<TeamEvent> {
  const { data } = await apiClient.put<TeamEvent>(`/admin/events/${id}`, payload)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/events/${id}`)
}

export async function setStatus(id: number, status: Exclude<EventStatus, 'draft'>, reason = ''): Promise<TeamEvent> {
  const { data } = await apiClient.post<TeamEvent>(`/admin/events/${id}/${status === 'published' ? 'publish' : status === 'cancelled' ? 'cancel' : 'archive'}`, { reason })
  return data
}

export async function listCategories(): Promise<EventCategory[]> {
  const { data } = await apiClient.get<EventCategory[]>('/admin/event-categories')
  return data
}

export async function createCategory(payload: Omit<EventCategory, 'id'>): Promise<EventCategory> {
  const { data } = await apiClient.post<EventCategory>('/admin/event-categories', payload)
  return data
}

export async function updateCategory(id: number, payload: Omit<EventCategory, 'id'>): Promise<EventCategory> {
  const { data } = await apiClient.put<EventCategory>(`/admin/event-categories/${id}`, payload)
  return data
}

export async function deleteCategory(id: number): Promise<void> {
  await apiClient.delete(`/admin/event-categories/${id}`)
}

export async function listSources(): Promise<EventSource[]> {
  const { data } = await apiClient.get<EventSource[]>('/admin/event-sources')
  return data
}

export async function createSource(payload: Omit<EventSource, 'id' | 'last_sync_at'>): Promise<EventSource> {
  const { data } = await apiClient.post<EventSource>('/admin/event-sources', payload)
  return data
}

export async function updateSource(id: number, payload: Omit<EventSource, 'id' | 'last_sync_at'>): Promise<EventSource> {
  const { data } = await apiClient.put<EventSource>(`/admin/event-sources/${id}`, payload)
  return data
}

export async function deleteSource(id: number): Promise<void> {
  await apiClient.delete(`/admin/event-sources/${id}`)
}

export async function getMapSettings(): Promise<EventMapSettings> {
  const { data } = await apiClient.get<EventMapSettings>('/admin/event-settings')
  return data
}

export async function updateMapSettings(payload: EventMapSettings): Promise<EventMapSettings> {
  const { data } = await apiClient.put<EventMapSettings>('/admin/event-settings', payload)
  return data
}

export async function previewImport(payload: EventImportEnvelope): Promise<EventImportBatch> {
  const { data } = await apiClient.post<EventImportBatch>('/admin/event-imports/preview', payload)
  return data
}

export async function getImport(id: number): Promise<EventImportBatch> {
  const { data } = await apiClient.get<EventImportBatch>(`/admin/event-imports/${id}`)
  return data
}

export async function commitImport(id: number, publish: boolean): Promise<EventImportBatch> {
  const { data } = await apiClient.post<EventImportBatch>(`/admin/event-imports/${id}/commit`, { publish })
  return data
}

export default {
  list,
  getById,
  create,
  update,
  remove,
  setStatus,
  listCategories,
  createCategory,
  updateCategory,
  deleteCategory,
  listSources,
  createSource,
  updateSource,
  deleteSource,
  getMapSettings,
  updateMapSettings,
  previewImport,
  getImport,
  commitImport,
}
