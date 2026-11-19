import type { ChatArtifact, ChatClarification, ChatEvent, ChatRun } from '@/types/workbenchChat'
export interface TaskItem { id: string; title: string; status: 'pending' | 'running' | 'completed' | 'failed' }
export interface ToolItem { id: string; title: string; message: string; state: 'running' | 'completed' | 'failed' | 'ended'; created_at: string }
export function runErrorMessage(run: Pick<ChatRun, 'status' | 'error_code' | 'error_message'>): string {
  if (run.error_code === 'RECOVERY_CONFIRMATION_REQUIRED') return '执行服务曾在请求已发出后中断，为避免重复计费，本次任务已暂停自动重试。你可以先终止本次任务，再发起新任务。'
  return run.error_message || (run.status === 'needs_confirmation' ? '本次请求已发出，但尚未确认最终结果。为避免重复计费，已暂停自动重试。你可以先终止本次任务，再发起新任务。' : '本次任务未完成，请稍后重试。')
}
export function taskItems(events: ChatEvent[]): TaskItem[] {
  const event = [...events].reverse().find(e => e.type === 'task.updated' && Array.isArray(e.payload.tasks))
  if (!event) return []
  return (event.payload.tasks as { id?: unknown; title?: unknown; status?: unknown }[]).flatMap(task => {
    if (!task || typeof task.id !== 'string' || typeof task.title !== 'string') return []
    const status = task.status === 'in_progress' ? 'running' : task.status
    if (!['pending', 'running', 'completed', 'failed'].includes(status as string)) return []
    return [{ id: task.id, title: task.title, status: status as TaskItem['status'] }]
  })
}
export function toolItems(events: ChatEvent[], run: ChatRun): ToolItem[] {
  const tools = new Map<string, ToolItem>()
  for (const e of events) {
    if (!['tool.started', 'tool.completed'].includes(e.type)) continue
    const p = e.payload
    const id = typeof p.operation_id === 'string' ? p.operation_id : e.id
    const previous = tools.get(id)
    tools.set(id, { id, title: typeof p.title === 'string' ? p.title : previous?.title || '处理任务', message: typeof p.message === 'string' ? p.message : previous?.message || '', state: e.type === 'tool.started' ? 'running' : p.status === 'failed' ? 'failed' : 'completed', created_at: previous?.created_at || e.created_at })
  }
  if (!['running', 'queued', 'recovering', 'waiting_user'].includes(run.status)) for (const tool of tools.values()) if (tool.state === 'running') tool.state = 'ended'
  return [...tools.values()]
}
export function runArtifacts(events: ChatEvent[], run: ChatRun): ChatArtifact[] {
  // A finished history projection is authoritative, including an explicit []
  // after a resource was deleted. Old publication events must not restore it.
  if (run.artifacts !== undefined && !['queued', 'running', 'recovering', 'waiting_user', 'needs_confirmation'].includes(run.status)) return run.artifacts
  const files = new Map<string, ChatArtifact>()
  for (const file of run.artifacts || []) files.set(file.version_id, file)
  for (const e of events) { const a = e.payload.artifact as ChatArtifact | undefined; if (e.type === 'artifact.published' && a && typeof a.version_id === 'string' && typeof a.name === 'string') files.set(a.version_id, a) }
  return [...files.values()]
}
export function clarification(events: ChatEvent[], run: ChatRun): ChatClarification | null {
  if (run.status !== 'waiting_user') return null
  const e = [...events].reverse().find(e => e.type === 'clarification.requested')
  if (!e || typeof e.payload.request_id !== 'string' || !Array.isArray(e.payload.questions)) return null
  const questions = e.payload.questions.filter(q => q && typeof q === 'object' && typeof (q as {id: unknown}).id === 'string' && typeof (q as {question: unknown}).question === 'string') as ChatClarification['questions']
  return { request_id: e.payload.request_id, title: typeof e.payload.title === 'string' ? e.payload.title : undefined, questions: questions.map(q => ({ ...q, options: Array.isArray(q.options) ? q.options.filter(v => typeof v === 'string') : [] })) }
}
export const runStatusLabel: Record<string, string> = { queued: '等待执行', running: '正在执行任务', recovering: '正在恢复任务', waiting_user: '等待你的补充', completed: '已完成', partially_completed: '部分完成', failed: '任务失败', cancelled: '已停止', needs_confirmation: '结果需要确认' }
