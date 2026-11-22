import { apiClient, buildApiUrl } from './client'
import { refreshAuthTokens } from './tokenRefresh'
import type { ChatStatus, ChatModels, ChatConversation, ChatConversationPage, ChatHistory, ChatRun, ChatSubmit, ChatEvent, ChatResource, ChatResourceAccess, ChatClarificationAnswer, ChatArtifact, ChatToolDetail, ChatToolSummary, ChatPrintPreview } from '@/types/workbenchChat'
const base = '/workbench/chat'
const segment = (id: string) => encodeURIComponent(id)
export const workbenchChatApi = {
  async status() { return (await apiClient.get<ChatStatus>(`${base}/status`)).data },
  async models() { return (await apiClient.get<ChatModels>(`${base}/models`)).data },
  async conversations(cursor?: string) { return (await apiClient.get<ChatConversationPage>(`${base}/conversations`, { params: { cursor, limit: 40 } })).data },
  async createConversation(id: string, title: string) { return (await apiClient.post<ChatConversation>(`${base}/conversations`, { id, title })).data },
  async renameConversation(id: string, title: string) { return (await apiClient.patch<ChatConversation>(`${base}/conversations/${segment(id)}`, { title })).data },
  async deleteConversation(id: string) { await apiClient.delete(`${base}/conversations/${segment(id)}`) },
  async history(id: string, before_seq?: number) { return (await apiClient.get<ChatHistory>(`${base}/conversations/${segment(id)}/messages`, { params: { before_seq } })).data },
  async submit(id: string, body: ChatSubmit) { return (await apiClient.post<ChatRun>(`${base}/conversations/${segment(id)}/runs`, body)).data },
  async run(id: string) { return (await apiClient.get<ChatRun>(`${base}/runs/${segment(id)}`)).data },
  async tools(id: string, signal: AbortSignal) { return (await apiClient.get<{ items: ChatToolSummary[] }>(`${base}/runs/${segment(id)}/tools`, { signal })).data.items },
  async toolDetail(id: string, operation: string, signal: AbortSignal) { return (await apiClient.get<ChatToolDetail>(`${base}/runs/${segment(id)}/tools/${segment(operation)}`, { signal })).data },
  async advance(id: string, signal: AbortSignal) {
    return (await apiClient.post<ChatRun>(`${base}/runs/${segment(id)}/advance`, undefined, { signal, timeout: 0 })).data
  },
  async selectAnswer(id: string) { return (await apiClient.post<ChatRun>(`${base}/runs/${segment(id)}/select`)).data },
  async cancel(id: string) { return (await apiClient.delete<ChatRun>(`${base}/runs/${segment(id)}`)).data },
  async clarify(id: string, request_id: string, answers: ChatClarificationAnswer[]) { return (await apiClient.post<ChatRun>(`${base}/runs/${segment(id)}/input`, { request_id, answers })).data },
  async upload(file: File) { const body = new FormData(); body.append('file', file); return (await apiClient.post<ChatResource>(`${base}/resources`, body, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 120000 })).data },
  async resources(cursor?: string) { return (await apiClient.get<{ items: ChatArtifact[]; next_cursor?: string }>(`${base}/resources`, { params: { cursor, limit: 40 } })).data },
  async deleteResource(id: string) { await apiClient.delete(`${base}/resources/${segment(id)}`) },
  async access(versionId: string, signal?: AbortSignal) { return (await apiClient.get<ChatResourceAccess>(`${base}/resources/${segment(versionId)}/access`, { signal })).data },
  async printPreview(versionId: string, signal: AbortSignal, retry = false) {
    return (await apiClient.post<ChatPrintPreview>(`${base}/resources/${segment(versionId)}/print-preview`, undefined, { signal, params: retry ? { retry: 1 } : undefined })).data
  },
}

/** Incremental SSE framing: handles CRLF, UTF-8 chunk boundaries (at the caller), comments and multiline data. */
export function createChatEventDecoder(accept: (event: ChatEvent) => void) {
  let buffer = ''
  return (chunk: string, final = false) => {
    buffer += chunk
    if (buffer.length > 2 * 1024 * 1024) throw new Error('任务事件过大，请重新打开会话')
    let match: RegExpExecArray | null
    while ((match = /\r?\n\r?\n/.exec(buffer))) {
      const frame = buffer.slice(0, match.index)
      buffer = buffer.slice(match.index + match[0].length)
      const data = frame.split(/\r?\n/).filter(line => line.startsWith('data:')).map(line => line.slice(5).replace(/^ /, '')).join('\n')
      if (!data || data === '[DONE]') continue
      const parsed: unknown = JSON.parse(data)
      if (!parsed || typeof parsed !== 'object') continue
      const e = parsed as ChatEvent
      if (typeof e.run_id === 'string' && Number.isSafeInteger(e.sequence) && typeof e.type === 'string' && e.payload && typeof e.payload === 'object') accept(e)
    }
    if (final && buffer.trim() && !buffer.trim().startsWith(':')) throw new Error('任务连接意外中断，正在同步运行状态')
  }
}

/** Uses the site's bearer token, never a workbench model API key or token in a query string. */
export async function readChatEvents(runId: string, after: number, signal: AbortSignal, accept: (event: ChatEvent) => void): Promise<void> {
  const url = buildApiUrl(`${base}/runs/${segment(runId)}/events?after=${after}`)
  const connect = () => fetch(url, { signal, credentials: 'include', headers: { Authorization: `Bearer ${localStorage.getItem('auth_token') || ''}`, Accept: 'text/event-stream' } })
  let response = await connect()
  if (response.status === 401) { await refreshAuthTokens(); response = await connect() }
  if (!response.ok) {
    let detail = ''
    try { const body = await response.json(); detail = body.message || body.error?.message || '' } catch { /* nginx text/HTML is not user-visible */ }
    throw Object.assign(new Error(detail || `任务连接失败（${response.status}）`), { status: response.status })
  }
  if (!response.body || !response.headers.get('content-type')?.includes('text/event-stream')) throw new Error('任务事件接口未返回流式响应')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  const decode = createChatEventDecoder(accept)
  try {
    while (true) { const { value, done } = await reader.read(); if (done) break; decode(decoder.decode(value, { stream: true })) }
    decode(decoder.decode(), true)
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
}
