import { apiClient } from '../client'
import type { ChatArtifact, ChatResourceAccess, ChatRun, ChatRunStatus } from '@/types/workbenchChat'

export const WORKBENCH_TASK_STATUSES = ['queued', 'generating', 'saving', 'completed', 'partial', 'failed', 'unknown', 'cancelled'] as const
export type WorkbenchTaskStatus = typeof WORKBENCH_TASK_STATUSES[number]
export type WorkbenchDays = 7 | 30 | 90

export interface WorkbenchExecutionLimits {
  chat_concurrent_runs: number
  office_concurrent_runs: number
  max_model_calls: number
  max_tool_calls: number
  max_output_tokens: number
  max_run_seconds: number
}
export interface WorkbenchConfig {
 model_profiles?: Record<string, { profile_id: string; override: false }>
 chat_concurrent_runs?: number
 office_concurrent_runs?: number
 max_model_calls?: number
 max_tool_calls?: number
 max_output_tokens?: number
 max_run_seconds?: number
 max_run_credits?: number
 deployment_limits?: WorkbenchExecutionLimits
 chat_enabled?: boolean
 office_enabled?: boolean
 search_enabled?: boolean
 files_enabled?: boolean
  image_studio_enabled: boolean
  image_studio_group_id: number
}

export interface WorkbenchOverview {
  scope: 'studio_cloud_jobs'
  days: WorkbenchDays
  since: string
  until: string
  tasks: Record<WorkbenchTaskStatus, number> & { total: number }
  calls: {
    total: number
    completed: number
    failed: number
    unknown: number
    cancelled: number
    billing_pending: number
    billing_unknown: number
  }
  output_images: number
  success_rate: number | null
  queue: Pick<Record<WorkbenchTaskStatus, number>, 'queued' | 'generating' | 'saving' | 'unknown'>
  gallery: { tasks: number; output_images: number; assets: number; used_bytes: number }
}

export interface WorkbenchTask {
  task_id: string
  user_id: number
  user_email: string
  model: string
  protocol: 'openai' | 'grok' | 'gemini'
  status: WorkbenchTaskStatus
  image_count: number
  output_count: number
  call_count: number
  billing_pending: number
  billing_unknown: number
  created_at: string
  updated_at: string
  finished_at: string | null
  elapsed_ms: number | null
  error_code: string
  error_message?: string
  route_calls?: WorkbenchTaskRouteCall[]
}

export interface WorkbenchTaskRouteCall {
  ordinal: number
  state: 'recorded' | 'not_dispatched' | 'pending' | 'unknown'
  upstreams: {
    account_id: number
    account_name: string
    upstream_model: string | null
    outcome: 'usage' | 'failed'
    status_code?: number | null
    recorded_at: string
  }[]
}

export interface WorkbenchTaskQuery {
  page?: number
  page_size?: number
  status?: WorkbenchTaskStatus
  user_id?: number
  model?: string
}

export interface WorkbenchTaskPage {
  items: WorkbenchTask[]
  total: number
  page: number
  page_size: number
  pages: number
}

export async function getWorkbenchOverview(days: WorkbenchDays = 7, signal?: AbortSignal): Promise<WorkbenchOverview> {
  const { data } = await apiClient.get<WorkbenchOverview>('/admin/workbench/overview', { params: { days }, signal })
  return data
}

export async function listWorkbenchTasks(params: WorkbenchTaskQuery, signal?: AbortSignal): Promise<WorkbenchTaskPage> {
  const { data } = await apiClient.get<WorkbenchTaskPage>('/admin/workbench/tasks', { params, signal })
  return data
}

export async function getWorkbenchConfig(): Promise<WorkbenchConfig> {
  const { data } = await apiClient.get<WorkbenchConfig>('/admin/workbench/config')
  return data
}

export async function updateWorkbenchConfig(config: WorkbenchConfig): Promise<WorkbenchConfig> {
  const { data } = await apiClient.put<WorkbenchConfig>('/admin/workbench/config', config)
  return data
}

export interface WorkbenchGalleryTask {
  user_id: number
  user_email: string
  task_id: string
  model: string
  created_at: string
  output_count: number
  image_ids: string[]
}
export interface WorkbenchGalleryPage {
  items: WorkbenchGalleryTask[]
  total: number
  page: number
  page_size: number
  pages: number
}
export interface WorkbenchGalleryImage {
  url: string
  expires_at: string
  cache_version?: string
  bytes: number
  mime: string
  width: number
  height: number
}
export async function listWorkbenchGallery(params: { page?: number; page_size?: number; user_id?: number; task_id?: string }, signal?: AbortSignal): Promise<WorkbenchGalleryPage> {
  const { data } = await apiClient.get<WorkbenchGalleryPage>('/admin/workbench/gallery', { params, signal })
  return data
}
export async function getWorkbenchGalleryImage(task: Pick<WorkbenchGalleryTask, 'user_id' | 'task_id'>, imageId: string, variant: 'thumbnail' | 'preview' = 'thumbnail', signal?: AbortSignal): Promise<WorkbenchGalleryImage> {
  const path = `/admin/workbench/gallery/${task.user_id}/tasks/${encodeURIComponent(task.task_id)}/images/${encodeURIComponent(imageId)}/url`
  const { data } = await apiClient.get<WorkbenchGalleryImage>(path, { params: { variant }, signal })
  return data
}


export const OFFICE_RUN_STATUSES: ChatRunStatus[] = ['queued', 'running', 'waiting_user', 'recovering', 'needs_confirmation', 'completed', 'partially_completed', 'failed', 'cancelled']
export interface WorkbenchChatOverview {
  since: string; until: string
  total_runs: number; chat_runs: number; office_runs: number
  completed: number; failed: number; queued: number; running: number; waiting_user: number; needs_confirmation: number
  model_calls: number; tool_calls: number; input_tokens: number; output_tokens: number; charge_credits: number
  used_bytes: number; resource_count: number
  workers: { id: string; preset: string; last_seen_at: string; executor_ready: boolean | null }[]
}
export interface WorkbenchChatRun extends ChatRun {
  user_id: number; user_email: string; user_name: string
  input_tokens: number; output_tokens: number; charge_credits: number
  started_at?: string; group_id: number
}
export interface WorkbenchChatRunQuery {
  page?: number; page_size?: number; user_id?: number; mode?: 'chat' | 'office'; status?: ChatRunStatus; model?: string; days?: WorkbenchDays
}
export interface WorkbenchAdminPage<T> { items: T[]; total: number; page: number; page_size: number; pages: number }
export interface WorkbenchChatOperation {
  id: string; run_id: string; kind: string; state: string; request_id: string
  created_at: string; dispatched_at?: string; completed_at?: string
  usage?: { settled: boolean; input_tokens: number; output_tokens: number; cache_read_tokens: number; actual_cost: number; charge_credits: number; duration_ms?: number; upstreams: WorkbenchTaskRouteCall['upstreams'] }
}
export interface WorkbenchChatRunDetail {
  run: ChatRun & { user_id: number; group_id: number }; user_email: string; user_name: string; operations: WorkbenchChatOperation[]
}
export interface WorkbenchAdminResource extends ChatArtifact { user_id: number; user_email: string; user_name: string; source?: 'upload' | 'generated' | 'gallery'; parent_version_id?: string }
export async function getWorkbenchChatOverview(days: WorkbenchDays, signal?: AbortSignal): Promise<WorkbenchChatOverview> {
  const { data } = await apiClient.get<WorkbenchChatOverview>('/admin/workbench/chat/overview', { params: { days }, signal }); return data
}
export async function listWorkbenchChatRuns(params: WorkbenchChatRunQuery, signal?: AbortSignal): Promise<WorkbenchAdminPage<WorkbenchChatRun>> {
  const { data } = await apiClient.get<WorkbenchAdminPage<WorkbenchChatRun>>('/admin/workbench/chat/runs', { params, signal }); return data
}
export async function getWorkbenchChatRun(id: string, signal?: AbortSignal): Promise<WorkbenchChatRunDetail> {
  const { data } = await apiClient.get<WorkbenchChatRunDetail>(`/admin/workbench/chat/runs/${encodeURIComponent(id)}`, { signal }); return data
}
export async function listWorkbenchChatResources(params: { page?: number; page_size?: number; user_id?: number }, signal?: AbortSignal): Promise<WorkbenchAdminPage<WorkbenchAdminResource>> {
  const { data } = await apiClient.get<WorkbenchAdminPage<WorkbenchAdminResource>>('/admin/workbench/chat/resources', { params, signal }); return data
}
export async function getWorkbenchChatResourceAccess(versionId: string, variant: 'preview' | 'original' = 'preview'): Promise<ChatResourceAccess> {
  const { data } = await apiClient.post<ChatResourceAccess>(`/admin/workbench/chat/resources/${encodeURIComponent(versionId)}/access`, undefined, { params: { variant } }); return data
}
