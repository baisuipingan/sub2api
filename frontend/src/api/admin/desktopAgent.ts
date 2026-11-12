import { apiClient } from '../client'

export const DESKTOP_AGENT_REASONING_LEVELS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const
export type DesktopAgentReasoningLevel = typeof DESKTOP_AGENT_REASONING_LEVELS[number]

export interface DesktopAgentModel {
  id: string
  display_name: string
  context_window: number
  max_output_tokens: number
  default_max_output_tokens: number
  reasoning_levels: DesktopAgentReasoningLevel[]
  default_reasoning: DesktopAgentReasoningLevel
  supports_reasoning: boolean
  supports_streaming: boolean
  requires_mfjs_tool_schema: boolean
  input: { text: boolean; image: boolean; video: boolean; audio: boolean; pdf: boolean }
  output: { text: boolean }
  supports_tool_call: boolean
  supports_json_schema_output: boolean
  supports_native_web_search: boolean
  supports_mid_conversation_system: boolean
}

export interface DesktopAgentConfig {
  enabled: boolean
  group_id: number
  base_url: string
  model?: DesktopAgentModel
}

// 仅关闭是明确操作，不能把无效草稿伪装成一次完整保存。
export interface DesktopAgentDisableRequest { enabled: false }
export type DesktopAgentUpdateRequest = DesktopAgentConfig | DesktopAgentDisableRequest

export async function getDesktopAgentConfig(): Promise<DesktopAgentConfig> {
  const { data } = await apiClient.get<DesktopAgentConfig>('/admin/desktop/config')
  return data
}

export async function updateDesktopAgentConfig(config: DesktopAgentUpdateRequest): Promise<DesktopAgentConfig> {
  const { data } = await apiClient.put<DesktopAgentConfig>('/admin/desktop/config', config)
  return data
}

