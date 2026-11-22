import { apiClient } from '../client'
import type { ChatArtifact, ChatHistory, ChatMessage, ChatResourceAccess, ChatRun, ChatRunStatus, ChatToolDetail, ChatToolSummary } from '@/types/workbenchChat'

export const WORKBENCH_TASK_STATUSES = ['queued', 'generating', 'saving', 'completed', 'partial', 'failed', 'unknown', 'cancelled'] as const
export type WorkbenchTaskStatus = typeof WORKBENCH_TASK_STATUSES[number]
export type WorkbenchDays = 7 | 30 | 90

export interface WorkbenchConfig {
 model_profiles?: Record<string, { profile_id: string; override: false }>
 chat_enabled?: boolean
 office_enabled?: boolean
 search_enabled?: boolean
 readonly search_provider_status?: 'unconfigured' | 'unavailable' | 'configured'
 files_enabled?: boolean
  image_studio_enabled: boolean
  image_studio_group_id: number
  readonly agent_group_id?: number
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

export async function updateWorkbenchConfig(config: Omit<WorkbenchConfig, 'agent_group_id' | 'search_provider_status'>): Promise<WorkbenchConfig> {
  const { data } = await apiClient.put<WorkbenchConfig>('/admin/workbench/config', config)
  return data
}

export type WorkbenchSearchProvider = '' | 'brave' | 'tavily' | 'deepseek'

export interface WorkbenchSearchConfig {
  provider: WorkbenchSearchProvider
  api_key_configured: boolean
  price_per_1000_usd?: number
}

export interface WorkbenchSearchTestResult {
  provider: Exclude<WorkbenchSearchProvider, ''>
  query: string
  results: { url: string; title: string; snippet: string }[]
}

export async function getWorkbenchSearchConfig(): Promise<WorkbenchSearchConfig> {
  const { data } = await apiClient.get<WorkbenchSearchConfig>('/admin/workbench/search')
  return data
}

export async function updateWorkbenchSearchConfig(config: { provider: WorkbenchSearchProvider; api_key?: string; price_per_1000_usd?: number }): Promise<WorkbenchSearchConfig> {
  const { data } = await apiClient.put<WorkbenchSearchConfig>('/admin/workbench/search', config)
  return data
}

export async function testWorkbenchSearch(query: string): Promise<WorkbenchSearchTestResult> {
  const { data } = await apiClient.post<WorkbenchSearchTestResult>('/admin/workbench/search/test', { query })
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
  storage_bytes?: number
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
  total_runs: number; completed: number; partially_completed: number; failed: number; cancelled: number
  model_calls: number; input_tokens: number; output_tokens: number; model_charge_credits: number
  search_calls: number; search_charge_credits: number; charge_credits: number
  used_bytes: number; resource_count: number
  office_tools: 'ready' | 'unavailable' | 'unconfigured' | 'disabled'
  office_nodes?: { total: number; online: number; draining: number; running: number; queued: number; slots: number }
}
export interface WorkbenchChatRun extends ChatRun {
  user_id: number; user_email: string; user_name: string
  input_tokens: number; output_tokens: number; charge_credits: number
  started_at?: string; group_id: number
}
export interface WorkbenchChatRunQuery {
  page?: number; page_size?: number; user_id?: number; execution?: 'browser' | 'historical'; status?: ChatRunStatus; model?: string; days?: WorkbenchDays
}
export interface WorkbenchAdminPage<T> { items: T[]; total: number; page: number; page_size: number; pages: number }
export interface WorkbenchChatOperation {
  id: string; run_id: string; kind: string; state: string; request_id: string
  created_at: string; dispatched_at?: string; completed_at?: string
  tool?: ChatToolSummary
  usage?: { settled: boolean; input_tokens: number; output_tokens: number; cache_read_tokens: number; actual_cost: number; charge_credits: number; duration_ms?: number; upstreams: WorkbenchTaskRouteCall['upstreams'] }
}
export interface WorkbenchChatRunDetail {
  run: ChatRun & { user_id: number; group_id: number }; user_email: string; user_name: string; operations: WorkbenchChatOperation[]
}
export interface WorkbenchAdminResource extends ChatArtifact {
  user_id: number
  user_email: string
  user_name: string
  source: 'upload' | 'generated'
  parent_version_id?: string
}
export async function getWorkbenchChatOverview(days: WorkbenchDays, signal?: AbortSignal): Promise<WorkbenchChatOverview> {
  const { data } = await apiClient.get<WorkbenchChatOverview>('/admin/workbench/chat/overview', { params: { days }, signal }); return data
}
export async function listWorkbenchChatRuns(params: WorkbenchChatRunQuery, signal?: AbortSignal): Promise<WorkbenchAdminPage<WorkbenchChatRun>> {
  const { data } = await apiClient.get<WorkbenchAdminPage<WorkbenchChatRun>>('/admin/workbench/chat/runs', { params, signal }); return data
}
export async function getWorkbenchChatRun(id: string, signal?: AbortSignal): Promise<WorkbenchChatRunDetail> {
  const { data } = await apiClient.get<WorkbenchChatRunDetail>(`/admin/workbench/chat/runs/${encodeURIComponent(id)}`, { signal }); return data
}
export interface WorkbenchConversationPage extends ChatHistory {
  conversation_title: string
  selected_user_message?: ChatMessage
}
export async function getWorkbenchConversationPage(id: string, before?: number, signal?: AbortSignal): Promise<WorkbenchConversationPage> {
  const { data } = await apiClient.get<WorkbenchConversationPage>(`/admin/workbench/chat/runs/${encodeURIComponent(id)}/messages`, { params: { before_seq: before }, signal })
  return data
}
export async function getWorkbenchChatToolDetail(id: string, operation: string, signal: AbortSignal): Promise<ChatToolDetail> {
  const { data } = await apiClient.get<ChatToolDetail>(`/admin/workbench/chat/runs/${encodeURIComponent(id)}/tools/${encodeURIComponent(operation)}`, { signal })
  return data
}
export async function listWorkbenchChatResources(params: { page?: number; page_size?: number; user_id?: number }, signal?: AbortSignal): Promise<WorkbenchAdminPage<WorkbenchAdminResource>> {
  const { data } = await apiClient.get<WorkbenchAdminPage<WorkbenchAdminResource>>('/admin/workbench/chat/resources', { params, signal }); return data
}
export async function getWorkbenchChatResourceAccess(versionId: string, variant: 'preview' | 'original' = 'preview'): Promise<ChatResourceAccess> {
  const { data } = await apiClient.post<ChatResourceAccess>(`/admin/workbench/chat/resources/${encodeURIComponent(versionId)}/access`, undefined, { params: { variant } }); return data
}
