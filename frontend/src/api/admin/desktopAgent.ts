import { apiClient } from '../client'

export const DESKTOP_AGENT_REASONING_LEVELS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const
export type DesktopAgentReasoningLevel = typeof DESKTOP_AGENT_REASONING_LEVELS[number]

export interface DesktopAgentModel {
  id: string
  display_name: string
  context_window: number
  max_output_tokens: number
  output_limit_kind?: '' | 'fixed' | 'unlimited' | 'not_published'
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
  models: DesktopAgentModel[]
  default_model_id: string
  model_profiles?: Record<string, DesktopAgentModelProfileBinding>
}

export interface DesktopAgentModels { group_id: number; models: string[] }
export interface DesktopAgentModelProfileBinding { profile_id: string; override: boolean }
export interface DesktopAgentProfile {
  id: string
  display_name: string
  model: DesktopAgentModel
  source: 'official_openai' | 'official_deepseek' | 'official_anthropic' | 'official_xai' | 'official_google'
  source_url: string
  reviewed_at: string
  revision: string
  aliases: string[]
  official_context_window: number | null
  max_input_tokens: number | null
  official_max_output_tokens?: number | null
  official_reasoning_levels: DesktopAgentReasoningLevel[]
  notes: string[]
}

export async function getDesktopAgentProfiles(): Promise<{ profiles: DesktopAgentProfile[] }> {
  const { data } = await apiClient.get<{ profiles: DesktopAgentProfile[] }>('/admin/desktop/profiles')
  return data
}

export interface DesktopAgentProfileCatalogStatus {
  source_url: string
  revision: string
  profile_count: number
  last_success_at: string | null
  last_checked_at: string | null
  last_error: string
  using_fallback: boolean
}

export async function getDesktopAgentProfileCatalog(): Promise<DesktopAgentProfileCatalogStatus> {
  const { data } = await apiClient.get<DesktopAgentProfileCatalogStatus>('/admin/desktop/profile-catalog')
  return data
}

export async function refreshDesktopAgentProfileCatalog(): Promise<DesktopAgentProfileCatalogStatus> {
  const { data } = await apiClient.post<DesktopAgentProfileCatalogStatus>('/admin/desktop/profile-catalog/refresh')
  return data
}

export async function updateDesktopAgentProfileCatalogSource(source_url: string): Promise<DesktopAgentProfileCatalogStatus> {
  const { data } = await apiClient.put<DesktopAgentProfileCatalogStatus>('/admin/desktop/profile-catalog/source', { source_url })
  return data
}

export async function getDesktopAgentConfig(): Promise<DesktopAgentConfig> {
  const { data } = await apiClient.get<DesktopAgentConfig>('/admin/desktop/config')
  return data
}

export async function updateDesktopAgentConfig(config: DesktopAgentConfig): Promise<DesktopAgentConfig> {
  const { data } = await apiClient.put<DesktopAgentConfig>('/admin/desktop/config', config)
  return data
}

/** 只读已保存分组目录，不发上游探测或领取推理凭据。 */
export async function getDesktopAgentModels(groupId: number): Promise<DesktopAgentModels> {
  const { data } = await apiClient.get<DesktopAgentModels>('/admin/desktop/models', { params: { group_id: groupId } })
  return data
}

/** 关闭只提交动作，不把未保存草稿带到服务器。 */
export async function disableDesktopAgentConfig(): Promise<DesktopAgentConfig> {
  const { data } = await apiClient.post<DesktopAgentConfig>('/admin/desktop/config/disable')
  return data
}

