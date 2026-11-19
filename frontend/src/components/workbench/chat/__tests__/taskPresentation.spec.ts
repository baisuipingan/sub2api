import { describe, expect, it } from 'vitest'
import { episodeActivity, executionEpisodes } from '../taskPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'

const run: ChatRun = { id: 'run', conversation_id: 'conversation', model: 'model', status: 'running', created_at: '', updated_at: '' }
function event(sequence: number, type: string, payload: Record<string, unknown>): ChatEvent { return { id: `event-${sequence}`, run_id: run.id, sequence, type, payload, created_at: '' } }

describe('public task execution presentation', () => {
  it('merges start/end events and never assigns an unscoped tool to a planned task', () => {
    const episodes = executionEpisodes([
      event(1, 'task.updated', { tasks: [{ id: 'todo', title: '计划中的任务', status: 'running' }] }),
      event(2, 'tool.started', { operation_id: 'search', tool_name: 'web_search', title: '查询资料' }),
      event(3, 'tool.completed', { operation_id: 'search', status: 'completed', message: '找到公开资料' }),
    ], run)
    expect(episodes).toHaveLength(1)
    expect(episodes[0]?.task).toBeUndefined()
    expect(episodes[0]?.steps).toHaveLength(1)
    expect(episodes[0]?.steps[0]).toMatchObject({ name: 'web_search', title: '查询资料', state: 'completed', message: '找到公开资料' })
    expect(episodeActivity(episodes[0]!.steps)).toBe('搜索资料 1 次')
  })

  it('renders a scoped task only when the backend explicitly identifies it', () => {
    const episodes = executionEpisodes([
      event(1, 'task.updated', { tasks: [{ id: 'todo', title: '核对文档', status: 'in_progress' }] }),
      event(2, 'tool.started', { operation_id: 'read', task_id: 'todo', tool_name: 'read_file' }),
    ], run)
    expect(episodes[0]?.task).toMatchObject({ id: 'todo', title: '核对文档', status: 'running' })
    expect(episodes[0]?.active).toBe(true)
  })

  it.each(['failed', 'cancelled', 'needs_confirmation', 'completed'] as const)('does not turn an unconfirmed operation into success for a %s run', status => {
    const [episode] = executionEpisodes([event(1, 'tool.started', { operation_id: 'office', tool_name: 'execute_office_python' })], { ...run, status })
    expect(episode?.active).toBe(false)
    expect(episode?.steps[0]?.state).toBe('ended')
  })

  it('separates clarification resumes and leaves the previous episode paused', () => {
    const events = [event(1, 'tool.started', { operation_id: 'old', tool_name: 'read_file' }), event(2, 'run.queued', { resumed: true })]
    const beforeNewTool = executionEpisodes(events, run)
    expect(beforeNewTool[0]?.active).toBe(false)
    expect(beforeNewTool[0]?.steps[0]?.state).toBe('paused')
    const episodes = executionEpisodes([...events, event(3, 'tool.started', { operation_id: 'new', tool_name: 'write_file' })], run)
    expect(episodes).toHaveLength(2)
    expect(episodes[1]?.active).toBe(true)
    expect(episodeActivity(episodes[1]!.steps)).toBe('整理文件 1 次')
    expect(executionEpisodes(events.slice(0, 1), { ...run, status: 'waiting_user' })[0]?.steps[0]?.state).toBe('paused')
  })

  it('does not manufacture a reasoning step or an activity summary for todo bookkeeping', () => {
    expect(executionEpisodes([event(1, 'task.updated', { tasks: [{ id: 'todo', title: '计划', status: 'pending' }] })], run)).toEqual([])
    const [episode] = executionEpisodes([event(1, 'tool.completed', { operation_id: 'todo', tool_name: 'write_todos' })], run)
    expect(episodeActivity(episode!.steps)).toBe('')
  })
})
