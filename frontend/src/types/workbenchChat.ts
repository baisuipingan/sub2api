/** Workbench public contract. All identifiers are opaque; never infer tenant ownership in the client. */
export type ChatRunStatus = 'queued' | 'running' | 'waiting_user' | 'recovering' | 'completed' | 'partially_completed' | 'failed' | 'cancelled' | 'needs_confirmation'
export interface ChatStatus {
  enabled: boolean
  browser_driven?: boolean
  office_file_extensions?: string[]
  files_enabled?: boolean
  office_enabled?: boolean
  search_allowed?: boolean
  search_enabled?: boolean
  search_price_per_1000_usd?: number
  uploads_enabled?: boolean
  max_file_bytes?: number
  max_files?: number
}
export interface ChatModel {
  id: string
  protocol?: string
  display_name?: string
  supports_tools?: boolean
  supports_vision?: boolean
  reasoning_efforts?: string[]
  default_reasoning_effort?: string
}
export interface ChatModels { models: ChatModel[]; default_model?: string }
export interface ChatConversation { id: string; title: string; created_at: string; updated_at: string }
export interface ChatConversationPage { items: ChatConversation[]; next_cursor?: string }
export interface ChatResource {
  id: string
  version_id: string
  name: string
  mime: string
  bytes: number
  created_at?: string
}
export interface ChatArtifact extends ChatResource { source?: 'upload' | 'generated' | 'gallery'; preview_kind?: 'pdf' | 'image' | 'text' | 'markdown' | 'unsupported'; role?: 'input' | 'output' }
export interface ChatCitation { id: string; title: string; url?: string; excerpt?: string }
export interface ChatMessageContent { text: string; reasoning_summary?: string; artifacts?: ChatArtifact[]; citations?: ChatCitation[] }
export interface ChatMessage {
  id: string
  conversation_id: string
  run_id: string
  sequence: number
  role: 'user' | 'assistant' | 'system'
  content: string | ChatMessageContent
  created_at: string
}
export interface ChatRun {
  id: string
  conversation_id: string
  model: string
  status: ChatRunStatus
  model_calls?: number
  tool_calls?: number
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
  finished_at?: string
  mode?: 'chat' | 'office'
  search_enabled?: boolean
  regeneration_of?: string
  selected?: boolean
  task_mode?: boolean
  browser_driven?: boolean
  artifacts?: ChatArtifact[]
}
export interface ChatEvent {
  id: string
  run_id: string
  sequence: number
  type: string
  payload: Record<string, unknown>
  created_at: string
}
export type ChatToolStatus = 'pending' | 'running' | 'completed' | 'partial' | 'failed' | 'cancelled' | 'unknown' | 'ended'
export interface ChatToolSummary {
  id: string
  name?: string
  title: string
  state: ChatToolStatus
}
export type ChatToolArtifact = Pick<ChatArtifact, 'name' | 'version_id'> & Partial<Pick<ChatArtifact, 'id' | 'mime' | 'bytes' | 'preview_kind'>>
export interface ChatToolDetail extends ChatToolSummary {
  query?: string
  provider?: string
  search_charge?: ChatSearchCharge
  result_count?: number
  source?: string
  file_name?: string
  source_version_id?: string
  scope?: string
  duration_ms?: number
  replacements?: number
  cells_changed?: number
  calculated?: number
  formula_errors?: number
  processed?: number
  skipped?: number
  failed?: number
  error_message?: string
  artifacts?: ChatToolArtifact[]
  files?: { name: string; status: string; replacements?: number; message?: string }[]
}
export interface ChatHistory { messages: ChatMessage[]; runs: ChatRun[]; next_before?: number; has_more?: boolean }
export interface ChatSubmit {
  run_id: string
  model: string
  content: string
  task_mode?: boolean
  web_search: boolean
  search_price_per_1000_usd?: number
  reasoning_effort?: string
  resource_version_ids?: string[]
  target_resource_version_id?: string
  regenerate_run_id?: string
}
export interface ChatClarificationQuestion { id: string; question: string; options: string[]; multiple?: boolean }
export interface ChatClarification { request_id: string; title?: string; questions: ChatClarificationQuestion[] }
export interface ChatClarificationAnswer { question_id: string; values: string[] }
export interface ChatResourceAccess { url: string; preview_url?: string; expires_at?: string; preview_kind?: string; can_render_print_preview?: boolean }
export type ChatPrintPreview =
  | { status: 'pending'; retry_after_ms?: number }
  | { status: 'ready'; preview_url: string; expires_at: string }
export interface ChatSearchCharge {
  run_id: string
  operation_id: string
  provider: string
  amount_usd: number
  amount_credits: number
  billing_currency: 'USD' | 'CNY'
  usd_to_cny_rate: number
  created_at: string
  result_count?: number
}
export const CHAT_ACTIVE_STATUSES = new Set<ChatRunStatus>(['queued', 'running', 'recovering', 'waiting_user', 'needs_confirmation'])
export function chatText(content: ChatMessage['content']): string { return typeof content === 'string' ? content : content?.text || '' }
export function chatArtifacts(content: ChatMessage['content']): ChatArtifact[] { return typeof content === 'string' ? [] : content?.artifacts || [] }
export function isChatDocx(file: { name: string }): boolean { return /\.docx$/i.test(file.name) }
export const CHAT_IMAGE_EXTENSIONS = ['.png', '.jpg', '.jpeg', '.webp']
export function acceptsChatAttachment(file: { name: string }, extensions?: string[]): boolean {
  const name = file.name.toLowerCase()
  return !extensions || extensions.some(extension => name.endsWith(extension))
}
