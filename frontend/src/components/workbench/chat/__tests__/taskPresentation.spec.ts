import { describe, expect, it } from 'vitest'
import { episodeActivity, executionEpisodes, workbenchProgress } from '../taskPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'

const run: ChatRun = { id: 'run', conversation_id: 'conversation', model: 'model', status: 'running', created_at: '', updated_at: '' }
function event(sequence: number, type: string, payload: Record<string, unknown>): ChatEvent { return { id: `event-${sequence}`, run_id: run.id, sequence, type, payload, created_at: '' } }

describe('browser workbench progress', () => {
  it('groups interleaved tool results with the model round and separates the answer', () => {
    const events = [
      event(1, 'model.started', { operation_id: 'first' }),
      event(2, 'assistant.delta', { operation_id: 'first', text: '我先查' }),
      event(3, 'reasoning.delta', { operation_id: 'first', text: 'Provider summary' }),
      event(4, 'assistant.delta', { operation_id: 'first', text: '一下。' }),
      event(5, 'model.completed', { operation_id: 'first', tool_count: 2 }),
      event(6, 'tool.started', { operation_id: 'search' }),
      event(7, 'tool.completed', { operation_id: 'search' }),
    ]
    expect(workbenchProgress(events)).toEqual({
      groups: [{ id: 'first', commentary: '我先查一下。', toolIds: ['search'] }], phase: 'tools', answer: '', reasoningSummary: 'Provider summary',
    })
    events.push(
      event(8, 'tool.started', { operation_id: 'read' }),
      event(9, 'tool.completed', { operation_id: 'read' }),
    )
    expect(workbenchProgress(events).phase).toBe('tool-results')
    events.push(
      event(10, 'model.started', { operation_id: 'second' }),
      event(11, 'assistant.delta', { operation_id: 'second', text: '这是最终答案。' }),
      event(12, 'model.completed', { operation_id: 'second', tool_count: 0 }),
    )
    expect(workbenchProgress(events)).toEqual({
      groups: [{ id: 'first', commentary: '我先查一下。', toolIds: ['search', 'read'] }],
      phase: 'answer', answer: '这是最终答案。', reasoningSummary: 'Provider summary',
    })
  })

  it('discards retried drafts and reasoning, including delayed deltas for a discarded attempt', () => {
    const events = [
      event(1, 'model.started', { operation_id: 'first' }),
      event(2, 'assistant.delta', { operation_id: 'first', text: '先查资料。' }),
      event(3, 'model.completed', { operation_id: 'first', tool_count: 1 }),
      event(4, 'tool.completed', { operation_id: 'search' }),
      event(5, 'model.started', { operation_id: 'failed' }),
      event(6, 'assistant.delta', { operation_id: 'failed', text: '未完成的回答' }),
      event(7, 'reasoning.delta', { operation_id: 'failed', text: '上次推理摘要' }),
      event(8, 'model.retrying', { operation_id: 'failed', attempt: 1, max_retries: 5 }),
      event(9, 'model.started', { operation_id: 'retry' }),
      event(10, 'assistant.delta', { operation_id: 'failed', text: '延迟到达的旧输出' }),
    ]
    expect(workbenchProgress(events)).toEqual({
      groups: [{ id: 'first', commentary: '先查资料。', toolIds: ['search'] }],
      answer: '', reasoningSummary: '', phase: 'model', retry: { attempt: 1, maxRetries: 5 },
    })
    events.push(
      event(11, 'reasoning.delta', { operation_id: 'retry', text: '本次推理摘要' }),
      event(12, 'assistant.delta', { operation_id: 'retry', text: '正在重新回答' }),
      event(13, 'model.retrying', { operation_id: 'retry', attempt: 2, max_retries: 5 }),
      event(14, 'model.started', { operation_id: 'final' }),
    )
    expect(workbenchProgress(events)).toMatchObject({ answer: '', reasoningSummary: '', retry: { attempt: 2, maxRetries: 5 } })
    events.push(
      event(15, 'reasoning.delta', { operation_id: 'final', text: '最终推理摘要' }),
      event(16, 'assistant.delta', { operation_id: 'final', text: '这是最终答案。' }),
      event(17, 'model.completed', { operation_id: 'final', tool_count: 0 }),
    )
    expect(workbenchProgress(events)).toEqual({
      groups: [{ id: 'first', commentary: '先查资料。', toolIds: ['search'] }],
      answer: '这是最终答案。', reasoningSummary: '最终推理摘要', phase: 'answer',
    })
  })

  it('uses existing operation identifiers for turns recorded before model phase events', () => {
    expect(workbenchProgress([
      event(1, 'assistant.delta', { operation_id: 'past', text: '先下载。' }),
      event(2, 'tool.completed', { operation_id: 'download' }),
      event(3, 'assistant.delta', { operation_id: 'current', text: '再检查。' }),
      event(4, 'tool.completed', { operation_id: 'read' }),
    ]).groups).toEqual([
      { id: 'past', commentary: '先下载。', toolIds: ['download'] },
      { id: 'current', commentary: '再检查。', toolIds: ['read'] },
    ])
  })
})

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
