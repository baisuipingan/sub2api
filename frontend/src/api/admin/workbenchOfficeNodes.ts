import { apiClient } from '@/api/client'

export type OfficeNodeState = 'active' | 'draining' | 'disabled'
export type OfficeNodeHealth = 'unchecked' | 'online' | 'unavailable'

export interface OfficeNodeStatus {
  node_id: string
  version: string
  actions: string[]
  concurrency: number
  concurrency_revision?: number
  running: number
  queued: number
  awaiting_upload: number
  storage_origins: string[]
}

export interface OfficeNode {
  id: string
  name: string
  url: string
  state: OfficeNodeState
  max_concurrent: number
  assigned: number
  token_configured: boolean
  health: OfficeNodeHealth
  last_checked_at?: string
  status?: OfficeNodeStatus
}

export interface OfficeNodeInput {
  name: string
  url: string
  state: OfficeNodeState
  max_concurrent: number
  token?: string
}

const base = '/admin/workbench/office-nodes'
const nodePath = (id: string) => `${base}/${encodeURIComponent(id)}`

export const workbenchOfficeNodesApi = {
  async list(signal?: AbortSignal) {
    const { data } = await apiClient.get<{ items: OfficeNode[] }>(base, { signal })
    return data.items
  },
  async create(input: OfficeNodeInput) {
    const { data } = await apiClient.post<OfficeNode>(base, input)
    return data
  },
  async update(id: string, input: OfficeNodeInput) {
    const { data } = await apiClient.put<OfficeNode>(nodePath(id), input)
    return data
  },
  async check(id: string) {
    const { data } = await apiClient.post<OfficeNode>(`${nodePath(id)}/check`)
    return data
  },
  async remove(id: string) {
    await apiClient.delete(nodePath(id))
  },
}
