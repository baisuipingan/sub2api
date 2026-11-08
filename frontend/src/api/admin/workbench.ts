import { apiClient } from '../client'

export const WORKBENCH_TASK_STATUSES = ['queued', 'generating', 'saving', 'completed', 'partial', 'failed', 'unknown', 'cancelled'] as const
export type WorkbenchTaskStatus = typeof WORKBENCH_TASK_STATUSES[number]
export type WorkbenchDays = 7 | 30 | 90

export interface WorkbenchConfig {
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
