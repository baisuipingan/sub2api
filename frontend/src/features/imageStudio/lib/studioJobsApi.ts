import type { StudioJobStatus, TaskRecord } from '../types'
import { GalleryApiError, ensureGalleryIdentity } from './galleryApi'
import { StudioRequestTimeoutError, withStudioRequestDeadline } from './requestDeadline'

const BASE = '/api/v1/image-studio/jobs'

export type { StudioJobStatus } from '../types'
export type StudioJobProtocol = 'openai' | 'grok' | 'gemini'
export type StudioPostprocess = 'none' | 'mask' | 'keyed'

export interface StudioJobSpec {
  protocol: StudioJobProtocol
  provider_profile?: string
  model: string
  prompt: string
  params: Record<string, unknown>
  input_image_ids: string[]
  request_input_image_ids: string[]
  mask_image_id?: string
  mask_target_image_id?: string
  postprocess: StudioPostprocess
  output_format?: 'png' | 'jpeg' | 'webp'
  output_compression?: number
  image_count: number
}

export interface StudioJobCall {
  ordinal: number
  status: StudioJobStatus
  attempt: number
  output_image_ids?: string[]
  raw_image_ids?: string[]
  error_code?: string
  error_message?: string
  billing_state: 'not_started' | 'pending' | 'recorded' | 'unknown'
}

export interface StudioJob {
  task_id: string
  status: StudioJobStatus
  task: TaskRecord
  calls: StudioJobCall[]
  created_at: string
  updated_at: string
}

export interface StudioJobPage {
  items: StudioJob[]
  next_cursor: string | null
}

function currentToken() {
  const value = localStorage.getItem('auth_token')
  if (!value) throw new GalleryApiError('Studio authentication is required', 401)
  return value
}

async function request<T>(path: string, init: RequestInit, authToken: string): Promise<T> {
  if (!authToken || localStorage.getItem('auth_token') !== authToken) {
    throw new GalleryApiError('Studio login changed', 401, 'STUDIO_LOGIN_CHANGED_BEFORE_SEND')
  }
  await ensureGalleryIdentity(authToken)
  if (localStorage.getItem('auth_token') !== authToken) {
    throw new GalleryApiError('Studio login changed', 401, 'STUDIO_LOGIN_CHANGED_BEFORE_SEND')
  }
  try {
    return await withStudioRequestDeadline(20_000, init.signal, async signal => {
      const response = await fetch(BASE + path, {
        ...init,
        signal,
        cache: 'no-store',
        headers: { Authorization: 'Bearer ' + authToken, ...init.headers },
      })
      let body: { code?: number; data?: T; message?: string; reason?: string; error?: { code?: string; message?: string } }
      try {
        body = await response.json() as typeof body
      } catch {
        throw new GalleryApiError('Invalid Studio task response', response.status)
      }
      if (localStorage.getItem('auth_token') !== authToken) {
        throw new GalleryApiError('Studio login changed', 401, 'STUDIO_LOGIN_CHANGED_AFTER_SEND')
      }
      if (!response.ok || body.code !== 0 || !body.data) {
        throw new GalleryApiError(body.error?.message ?? body.message ?? 'Studio task request failed', response.status, body.error?.code ?? body.reason)
      }
      return body.data
    })
  } catch (error) {
    if (error instanceof StudioRequestTimeoutError) throw new GalleryApiError('Studio task request timed out', 408, 'STUDIO_REQUEST_TIMEOUT')
    throw error
  }
}

export function createStudioJob(task: TaskRecord, spec: StudioJobSpec, authToken = currentToken()): Promise<{ job: StudioJob }> {
  return request('', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ task_id: task.id, task, spec }),
  }, authToken)
}

export function getStudioJob(taskId: string, authToken = currentToken(), signal?: AbortSignal): Promise<{ job: StudioJob }> {
  return request('/' + encodeURIComponent(taskId), { signal }, authToken)
}

export function listStudioJobs(cursor: string | null = null, limit = 50, authToken = currentToken(), signal?: AbortSignal, activeOnly = false): Promise<StudioJobPage> {
  const query = new URLSearchParams({ limit: String(limit) })
  if (cursor) query.set('cursor', cursor)
  if (activeOnly) query.set('active', '1')
  return request('?' + query, { signal }, authToken)
}

export function deleteStudioJob(taskId: string, authToken = currentToken()): Promise<{ deleted: boolean }> {
  return request('/' + encodeURIComponent(taskId), { method: 'DELETE' }, authToken)
}
