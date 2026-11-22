import { describe, it, expect, vi } from 'vitest'
vi.mock('@/api/client', () => ({ apiClient: {}, buildApiUrl: (p: string) => p }))
vi.mock('@/api/tokenRefresh', () => ({ refreshAuthTokens: vi.fn() }))
import { createChatEventDecoder } from '@/api/workbenchChat'
import { clarification, runArtifacts, runErrorMessage, taskItems, toolItems } from '../chatPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'
const event = (sequence: number, type: string, payload: Record<string, unknown>): ChatEvent => ({ id: `e${sequence}`, run_id: 'run', sequence, type, payload, created_at: '2026-11-19T00:00:00Z' })
const run: ChatRun = { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'running', created_at: '', updated_at: '' }
describe('workbench durable event framing and presentation', () => {
  it('explains interrupted execution consistently even when the server detail is English', () => {
    expect(runErrorMessage({ status: 'needs_confirmation', error_code: 'RECOVERY_CONFIRMATION_REQUIRED', error_message: 'Automatic replay is disabled' })).toContain('为避免重复计费')
    expect(runErrorMessage({ status: 'failed', error_message: '模型拒绝了这个请求' })).toBe('模型拒绝了这个请求')
  })
  it('reassembles split CRLF frames and ignores heartbeat comments', () => {
    const accept = vi.fn(), decode = createChatEventDecoder(accept)
    const one = event(1, 'assistant.delta', { text: '中文' })
    const serialized = JSON.stringify(one)
    decode(`: heartbeat\r\n\r\nevent: office\r\ndata: ${serialized.slice(0, 60)}`)
    expect(accept).not.toHaveBeenCalled()
    decode(`${serialized.slice(60)}\r\n\r\n`)
    expect(accept).toHaveBeenCalledTimes(1)
    expect(accept).toHaveBeenCalledWith(one)
  })
  it('rejects a truncated non-heartbeat frame instead of losing the cursor silently', () => {
    const decode = createChatEventDecoder(vi.fn())
    expect(() => decode('data: {"partial":true}', true)).toThrow('意外中断')
  })
  it('keeps real tool identity and stops its spinner after a cancelled run', () => {
    const events = [event(1, 'tool.started', { operation_id: 'op', title: '读取文档' })]
    expect(toolItems(events, run)[0]?.state).toBe('running')
    expect(toolItems(events, { ...run, status: 'cancelled' })[0]?.state).toBe('ended')
    events.push(event(2, 'tool.completed', { operation_id: 'op', title: '读取文档', status: 'failed', message: '文件损坏' }))
    expect(toolItems(events, run)).toHaveLength(1)
    expect(toolItems(events, run)[0]).toMatchObject({ state: 'failed', message: '文件损坏' })
  })
  it('uses the latest task update and only renders clarification while parked', () => {
    const events = [event(1, 'task.updated', { tasks: [{ id: 'a', title: '制作报告', status: 'pending' }] }), event(2, 'task.updated', { tasks: [{ id: 'a', title: '制作报告', status: 'completed' }] }), event(3, 'clarification.requested', { request_id: 'q', questions: [{ id: 'one', question: '格式？', options: ['Word', 'PPT'] }] })]
    expect(taskItems(events)[0]?.status).toBe('completed')
    expect(clarification(events, run)).toBeNull()
    expect(clarification(events, { ...run, status: 'waiting_user' })?.questions[0]?.options).toEqual(['Word', 'PPT'])
  })
  it('normalizes the worker in_progress task status and accepts older running events', () => {
    const events = [event(1, 'task.updated', { tasks: [
      { id: 'a', title: '制作报告', status: 'in_progress' },
      { id: 'b', title: '读取资料', status: 'running' },
      { id: 'c', title: '未知状态', status: 'unexpected' },
    ] })]
    expect(taskItems(events)).toEqual([
      { id: 'a', title: '制作报告', status: 'running' },
      { id: 'b', title: '读取资料', status: 'running' },
    ])
  })
  it('deduplicates immutable artifact versions while keeping multiple results', () => {
    const a = { id: 'a', version_id: 'v1', name: '报告.docx', mime: '', bytes: 1 }
    const b = { id: 'b', version_id: 'v2', name: '汇报.pptx', mime: '', bytes: 2 }
    expect(runArtifacts([event(1, 'artifact.published', { artifact: a }), event(2, 'artifact.published', { artifact: b })], { ...run, artifacts: [a] })).toEqual([a, b])
  })
  it('does not revive a deleted historical artifact from an older publication event', () => {
    const artifact = { id: 'a', version_id: 'v1', name: '报告.docx', mime: '', bytes: 1 }
    expect(runArtifacts([event(1, 'artifact.published', { artifact })], { ...run, status: 'completed', artifacts: [] })).toEqual([])
  })
})
