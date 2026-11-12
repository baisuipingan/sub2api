import type { AdminGroup } from '@/types'
import { DESKTOP_AGENT_REASONING_LEVELS, type DesktopAgentConfig, type DesktopAgentModel } from '@/api/admin/desktopAgent'

export function isDesktopAgentGroupEligible(group: Pick<AdminGroup, 'status' | 'subscription_type' | 'platform'>): boolean {
  return group.status === 'active' && group.subscription_type === 'standard' &&
    (group.platform === 'openai' || group.platform === 'composite')
}

export type DesktopAgentValidationCode =
  | 'group_required' | 'gateway_invalid' | 'model_required' | 'model_id_invalid'
  | 'display_name_invalid' | 'context_window_invalid' | 'max_output_invalid'
  | 'default_output_invalid' | 'capability_required' | 'required_capability'
  | 'reasoning_levels_invalid' | 'default_reasoning_invalid' | 'reasoning_unsupported'
  | 'models_limit' | 'model_duplicate' | 'default_model_invalid'

export interface DesktopAgentValidationIssue { field: string; code: DesktopAgentValidationCode }

function validModelString(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value === value.trim() &&
    new TextEncoder().encode(value).length <= 128 && !/\p{Cc}/u.test(value)
}

function validGateway(value: string): boolean {
  // URL会折叠dot segment与忽略空query，先检查原文，保持精确/v1合同。
  if (typeof value !== 'string' || !/^https:\/\/[^/?#@]+\/v1$/.test(value) || value !== value.trim() || /\p{Cc}/u.test(value)) return false
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !!url.hostname && !url.username && !url.password && url.pathname === '/v1'
  } catch { return false }
}

const positiveInteger = (value: number) => Number.isSafeInteger(value) && value > 0

/** 编辑前置提示；服务端仍最终验证分组授权、模型白名单及reasoning策略。 */
export function validateDesktopAgentConfig(config: DesktopAgentConfig): DesktopAgentValidationIssue[] {
  const issues: DesktopAgentValidationIssue[] = []
  const add = (field: string, code: DesktopAgentValidationCode) => issues.push({ field, code })
  if (!Number.isSafeInteger(config.group_id) || config.group_id < 0) add('group_id', 'group_required')
  // 后端未配置零值保持关闭，不填充猜测的模型能力。
  if (!config.enabled && config.group_id === 0 && config.base_url === '' &&
    Array.isArray(config.models) && config.models.length === 0 && config.default_model_id === '') return issues
  if (config.group_id === 0) add('group_id', 'group_required')
  if (!validGateway(config.base_url)) add('base_url', 'gateway_invalid')
  if (!Array.isArray(config.models) || config.models.length > 32) { add('models', 'models_limit'); return issues }
  if (config.models.length === 0) add('models', 'model_required')
  const ids = new Set<string>()
  config.models.forEach((model, index) => {
    const path = `models.${index}`
    if (!model || typeof model !== 'object') { add(path, 'model_required'); return }
    if (ids.has(model.id)) add(`${path}.id`, 'model_duplicate')
    ids.add(model.id)
    issues.push(...validateDesktopAgentModel(model, path))
  })
  if (!validModelString(config.default_model_id) || !ids.has(config.default_model_id)) add('default_model_id', 'default_model_invalid')
  return issues
}

function validateDesktopAgentModel(model: DesktopAgentModel, path: string): DesktopAgentValidationIssue[] {
  const issues: DesktopAgentValidationIssue[] = []
  const add = (field: string, code: DesktopAgentValidationCode) => issues.push({ field: `${path}.${field}`, code })
  if (!validModelString(model.id) || /[ *?\\]/u.test(model.id)) add('id', 'model_id_invalid')
  if (!validModelString(model.display_name)) add('display_name', 'display_name_invalid')
  if (!positiveInteger(model.context_window) || model.context_window > 100_000_000) add('context_window', 'context_window_invalid')
  if (!positiveInteger(model.max_output_tokens) || model.max_output_tokens >= model.context_window) add('max_output_tokens', 'max_output_invalid')
  if (!positiveInteger(model.default_max_output_tokens) || model.default_max_output_tokens > model.max_output_tokens) add('default_max_output_tokens', 'default_output_invalid')
  const capabilities: [string, unknown, boolean][] = [
    ['supports_reasoning', model.supports_reasoning, false],
    ['supports_streaming', model.supports_streaming, true],
    ['requires_mfjs_tool_schema', model.requires_mfjs_tool_schema, false],
    ['supports_tool_call', model.supports_tool_call, true],
    ['supports_json_schema_output', model.supports_json_schema_output, false],
    ['supports_native_web_search', model.supports_native_web_search, false],
    ['supports_mid_conversation_system', model.supports_mid_conversation_system, false],
    ['input.text', model.input?.text, true],
    ['input.image', model.input?.image, false],
    ['input.video', model.input?.video, false],
    ['input.audio', model.input?.audio, false],
    ['input.pdf', model.input?.pdf, false],
    ['output.text', model.output?.text, true],
  ]
  for (const [field, value, requiredTrue] of capabilities) {
    if (typeof value !== 'boolean') add(field, 'capability_required')
    else if (requiredTrue && !value) add(field, 'required_capability')
  }
  let previous = -1
  let validLevels = Array.isArray(model.reasoning_levels) && model.reasoning_levels.length > 0
  if (Array.isArray(model.reasoning_levels)) {
    for (const level of model.reasoning_levels) {
      const index = DESKTOP_AGENT_REASONING_LEVELS.indexOf(level)
      if (index <= previous) validLevels = false
      previous = index
    }
  }
  if (!validLevels) add('reasoning_levels', 'reasoning_levels_invalid')
  if (!Array.isArray(model.reasoning_levels) || !model.reasoning_levels.includes(model.default_reasoning)) add('default_reasoning', 'default_reasoning_invalid')
  if (model.supports_reasoning === false &&
    (!Array.isArray(model.reasoning_levels) || model.reasoning_levels.length !== 1 || model.reasoning_levels[0] !== 'none')) add('reasoning_levels', 'reasoning_unsupported')
  return issues
}
