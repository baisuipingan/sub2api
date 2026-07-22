import { apiClient } from './client'
import type { BasePaginationResponse, EventCategory, EventMapMarker, UserEvent } from '@/types'

export interface EventFilters {
  category?: string
  search?: string
  city?: string
  district?: string
  fee_type?: string
  from?: string
  to?: string
}

export async function list(page = 1, pageSize = 20, filters: EventFilters = {}, signal?: AbortSignal): Promise<BasePaginationResponse<UserEvent>> {
	const { data } = await apiClient.get<BasePaginationResponse<UserEvent>>('/events', {
    params: { page, page_size: pageSize, ...filters },
    signal,
  })
  return data
}

export async function map(filters: EventFilters & { bbox?: string }, signal?: AbortSignal): Promise<{ markers: EventMapMarker[]; truncated: boolean }> {
  const { data } = await apiClient.get<{ markers: EventMapMarker[]; truncated: boolean }>('/events/map', {
    params: filters,
    signal,
  })
  return data
}

export async function getById(id: number): Promise<UserEvent> {
	const { data } = await apiClient.get<UserEvent>(`/events/${id}`)
  return data
}

export async function categories(): Promise<EventCategory[]> {
  const { data } = await apiClient.get<EventCategory[]>('/events/categories')
  return data
}

export default { list, map, getById, categories }
