import type { ChatEvent, ChatRun } from '@/types/workbenchChat'
import { taskItems, type TaskItem } from './chatPresentation'

/** Public execution metadata only: no model reasoning, raw arguments or logs. */
export interface ExecutionStep {
  id: string
  name: string
  title: string
  message: string
  state: 'running' | 'completed' | 'failed' | 'ended' | 'paused'
  taskId?: string
  sequence: number
}
export interface ExecutionEpisode { id: string; task?: TaskItem; steps: ExecutionStep[]; active: boolean }
export function isExecuting(run: ChatRun) { return ['queued', 'running', 'recovering'].includes(run.status) }

export interface WorkbenchProgressGroup {
  id: string
  commentary: string
  toolIds: string[]
}

export interface WorkbenchProgress {
  groups: WorkbenchProgressGroup[]
  answer: string
  phase: 'model' | 'tools' | 'tool-results' | 'answer'
}

export function workbenchProgress(events: ChatEvent[]): WorkbenchProgress {
  const groups: WorkbenchProgressGroup[] = []
  let current: WorkbenchProgressGroup | undefined
  let phase: WorkbenchProgress['phase'] = 'model'
  let expectedTools = 0
  const done = new Set<string>()

  for (const { type, id, payload } of events) {
    const operation = typeof payload.operation_id === 'string' ? payload.operation_id : ''
    if (type === 'model.started' || type === 'assistant.delta') {
      // Older runs have text and tool events without explicit model markers.
      if (!current || operation && current.id !== operation || type === 'assistant.delta' && !operation && current.toolIds.length) {
        current = { id: operation || id, commentary: '', toolIds: [] }
        groups.push(current)
        expectedTools = 0
        phase = 'model'
      }
      if (type === 'assistant.delta' && typeof payload.text === 'string') current.commentary += payload.text
    } else if (type === 'model.completed') {
      expectedTools = typeof payload.tool_count === 'number' ? payload.tool_count : 0
      phase = expectedTools > 0 ? 'tools' : 'answer'
    } else if (type === 'tool.started' || type === 'tool.completed') {
      if (!current) {
        current = { id, commentary: '', toolIds: [] }
        groups.push(current)
      }
      const toolId = operation || id
      if (!current.toolIds.includes(toolId)) current.toolIds.push(toolId)
      if (type === 'tool.completed') done.add(toolId)
      else done.delete(toolId)
      const allDone = current.toolIds.length >= expectedTools && current.toolIds.every(tool => done.has(tool))
      phase = allDone ? 'tool-results' : 'tools'
    }
  }
  const answer = current && !current.toolIds.length && phase !== 'tools' ? current.commentary : ''
  return {
    groups: groups.filter(group => group.toolIds.length || group.commentary.trim() && (group !== current || phase === 'tools')),
    answer,
    phase,
  }
}

// Bisheng's session timeline and task-scoped timeline share the same leaf
// components. Our current public tools are session scoped; associate a task
// only when the server actually supplies task_id, never infer it from a todo.
export function executionEpisodes(events: ChatEvent[], run: ChatRun): ExecutionEpisode[] {
  const steps = new Map<string, ExecutionStep>()
  const boundaries: number[] = []
  for (const event of events) {
    if (event.type === 'run.queued' && event.payload.resumed === true) boundaries.push(event.sequence)
    if (!['tool.started', 'tool.completed'].includes(event.type)) continue
    const p = event.payload, id = typeof p.operation_id === 'string' ? p.operation_id : event.id
    const previous = steps.get(id)
    const taskId = typeof p.task_id === 'string' ? p.task_id : previous?.taskId
    steps.set(id, {
      id, name: typeof p.tool_name === 'string' ? p.tool_name : previous?.name || '',
      title: typeof p.title === 'string' ? p.title : previous?.title || '处理任务',
      message: typeof p.message === 'string' ? p.message : previous?.message || '', taskId,
      sequence: previous?.sequence ?? event.sequence,
      state: event.type === 'tool.completed' ? p.status === 'failed' ? 'failed' : 'completed' : 'running',
    })
  }
  const tasks = new Map(taskItems(events).map(task => [task.id, task]))
  const episodes: ExecutionEpisode[] = []
  for (const step of [...steps.values()].sort((a, b) => a.sequence - b.sequence)) {
    const boundary = boundaries.filter(sequence => sequence < step.sequence).length
    if (step.state === 'running' && (boundary < boundaries.length || !isExecuting(run))) step.state = run.status === 'waiting_user' || boundary < boundaries.length ? 'paused' : 'ended'
    const previous = episodes[episodes.length - 1]
    const groupId = `${boundary}:${step.taskId || 'session'}`
    if (!previous || previous.id !== groupId) episodes.push({ id: groupId, task: step.taskId ? tasks.get(step.taskId) : undefined, steps: [step], active: false })
    else previous.steps.push(step)
  }
  const last = episodes[episodes.length - 1]
  if (last && isExecuting(run) && last.id.startsWith(`${boundaries.length}:`)) last.active = !last.task || last.task.status === 'running'
  return episodes
}

const activities: Record<string, { label: string; category: string }> = {
  web_search: { label: '搜索资料', category: 'search' }, web_read: { label: '阅读网页', category: 'web' },
  read_file: { label: '读取文件', category: 'read' }, write_file: { label: '整理文件', category: 'write' },
  edit_file: { label: '修改文件', category: 'edit' }, execute_office_python: { label: '处理办公文件', category: 'office' },
  inspect_rendered_image: { label: '检查文档预览', category: 'inspect' },
}
export function episodeActivity(steps: ExecutionStep[]) {
  const counts = new Map<string, { label: string; count: number }>()
  for (const step of steps) {
    const activity = activities[step.name]
    if (!activity) continue
    const item = counts.get(activity.category) || { label: activity.label, count: 0 }
    item.count++; counts.set(activity.category, item)
  }
  return [...counts.values()].map(item => `${item.label} ${item.count} 次`).join(' · ')
}
export function stepStatus(step: ExecutionStep) {
  return { running: '进行中', completed: '已完成', failed: '未完成', ended: '已结束，未收到完成确认', paused: '已暂停' }[step.state]
}
