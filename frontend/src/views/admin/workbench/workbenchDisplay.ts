import type { WorkbenchTask, WorkbenchTaskStatus } from '@/api/admin/workbench'

export function workbenchStatusVariant(status: WorkbenchTaskStatus): string {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'error'
  if (status === 'unknown' || status === 'partial') return 'warning'
  return 'neutral'
}

export function workbenchElapsedSeconds(task: WorkbenchTask): number | null {
  if (task.elapsed_ms == null || !Number.isFinite(task.elapsed_ms)) return null
  return Math.max(0, Math.round(task.elapsed_ms / 1000))
}

export function workbenchUserIdFilter(value: string): number | undefined {
  if (!value.trim()) return undefined
  if (!/^[1-9]\d*$/.test(value.trim())) throw new Error('invalid user ID')
  const id = Number(value)
  if (!Number.isSafeInteger(id)) throw new Error('invalid user ID')
  return id
}
