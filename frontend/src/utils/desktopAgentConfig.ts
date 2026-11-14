import type { AdminGroup } from '@/types'
import { DESKTOP_AGENT_REASONING_LEVELS, type DesktopAgentConfig, type DesktopAgentModel, type DesktopAgentModelProfileBinding, type DesktopAgentProfile } from '@/api/admin/desktopAgent'

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
  | 'profile_required' | 'profile_invalid'

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
const publishedLimit = (value: unknown): value is number => typeof value === 'number' && positiveInteger(value)
export const hasDesktopAgentOutputLimit = (model: DesktopAgentModel): boolean => !model.output_limit_kind || model.output_limit_kind === 'fixed'
export const desktopAgentOutputBudgetCeiling = (model: DesktopAgentModel): number => hasDesktopAgentOutputLimit(model) ? model.max_output_tokens : model.context_window

export function getDesktopAgentProfileBinding(config: DesktopAgentConfig, publicID: string): DesktopAgentModelProfileBinding | undefined {
  return config.model_profiles && Object.prototype.hasOwnProperty.call(config.model_profiles, publicID) ? config.model_profiles[publicID] : undefined
}

export function resolveDesktopAgentProfile(profiles: DesktopAgentProfile[], publicID: string, binding?: DesktopAgentModelProfileBinding): DesktopAgentProfile | undefined {
  const official = profiles.find(profile => profile.aliases.includes(publicID))
  if (official && binding && binding.profile_id !== official.id) return undefined
  return profiles.find(profile => binding ? profile.id === binding.profile_id : profile.aliases.includes(publicID))
}

/** Profile application never changes the public identity or the parent default-model choice. */
export function applyDesktopAgentProfile(model: DesktopAgentModel, profile: DesktopAgentProfile, preserveDefaults = true): DesktopAgentModel {
  const next = { ...JSON.parse(JSON.stringify(profile.model)) as DesktopAgentModel, id: model.id, display_name: model.display_name }
  if (preserveDefaults) {
    if (next.reasoning_levels.includes(model.default_reasoning)) next.default_reasoning = model.default_reasoning
    if (positiveInteger(model.default_max_output_tokens) && model.default_max_output_tokens <= desktopAgentOutputBudgetCeiling(next)) next.default_max_output_tokens = model.default_max_output_tokens
  }
  return next
}

export function normalizeDesktopAgentProfiles(config: DesktopAgentConfig, profiles: DesktopAgentProfile[]): DesktopAgentConfig {
  const next = JSON.parse(JSON.stringify(config)) as DesktopAgentConfig
  const bindings: Record<string, DesktopAgentModelProfileBinding> = Object.create(null)
  next.models = next.models.map(model => {
    const oldBinding = getDesktopAgentProfileBinding(next, model.id)
    const profile = resolveDesktopAgentProfile(profiles, model.id, oldBinding)
    if (!profile) {
      if (oldBinding) bindings[model.id] = oldBinding
      return model
    }
    const binding = oldBinding || { profile_id: profile.id, override: false }
    bindings[model.id] = binding
    return binding.override ? model : applyDesktopAgentProfile(model, profile)
  })
  if (Object.keys(bindings).length) next.model_profiles = bindings
  else delete next.model_profiles
  return next
}

/** Fail closed on a malformed registry rather than publish guessed capabilities. */
const officialProfileHosts: Record<DesktopAgentProfile['source'], readonly string[]> = {
  official_openai: ['developers.openai.com', 'platform.openai.com'],
  official_deepseek: ['api-docs.deepseek.com'],
  official_anthropic: ['platform.claude.com', 'docs.anthropic.com'],
  official_xai: ['docs.x.ai'],
  official_google: ['ai.google.dev', 'aistudio.google.com'],
}

export function validDesktopAgentProfiles(value: unknown): value is DesktopAgentProfile[] {
  if (!Array.isArray(value) || value.length === 0) return false
  const ids = new Set<string>()
  const aliases = new Set<string>()
  return value.every((profile: DesktopAgentProfile) => {
    if (!profile || !validModelString(profile.id) || ids.has(profile.id) || !validModelString(profile.display_name) || !Object.prototype.hasOwnProperty.call(officialProfileHosts, profile.source)) return false
    ids.add(profile.id)
    try {
      const source = new URL(profile.source_url)
      // 每家档案必须链接自身官方域名；只扩展全局域名白名单会误认跨厂商或伪造来源。
      if (source.protocol !== 'https:' || !officialProfileHosts[profile.source].includes(source.hostname) || source.port || source.username || source.password) return false
    } catch { return false }
    // 预算式 thinking 没有离散官方档位；实际可执行档位仍由下方模型校验要求非空。
    if (!/^\d{4}-\d{2}-\d{2}$/.test(profile.reviewed_at) || typeof profile.revision !== 'string' || !profile.revision ||
      !(profile.official_context_window === null || publishedLimit(profile.official_context_window)) ||
      !(profile.max_input_tokens === null || publishedLimit(profile.max_input_tokens)) ||
      (profile.official_context_window === null && profile.max_input_tokens === null) ||
      (publishedLimit(profile.official_context_window) && publishedLimit(profile.max_input_tokens) && profile.max_input_tokens > profile.official_context_window) ||
      (profile.official_max_output_tokens !== undefined && !(profile.official_max_output_tokens === null || publishedLimit(profile.official_max_output_tokens))) ||
      !Array.isArray(profile.aliases) || !profile.aliases.includes(profile.id) || !profile.aliases.every(alias => {
        if (!validModelString(alias) || aliases.has(alias)) return false
        aliases.add(alias)
        return true
      }) || !Array.isArray(profile.official_reasoning_levels) ||
      !profile.official_reasoning_levels.every(level => DESKTOP_AGENT_REASONING_LEVELS.includes(level)) ||
      !Array.isArray(profile.notes) || !profile.notes.every(note => typeof note === 'string') ||
      !profile.model || profile.model.id !== profile.id || validateDesktopAgentModel(profile.model, 'profile').length > 0) return false
    if (profile.official_max_output_tokens !== undefined &&
      (hasDesktopAgentOutputLimit(profile.model) ? profile.official_max_output_tokens !== profile.model.max_output_tokens : profile.official_max_output_tokens !== null)) return false
    return true
  })
}

/** 编辑前置提示；服务端仍最终验证分组授权、模型白名单及reasoning策略。 */
export function validateDesktopAgentConfig(config: DesktopAgentConfig, profiles?: DesktopAgentProfile[]): DesktopAgentValidationIssue[] {
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
    if (profiles) {
      const binding = getDesktopAgentProfileBinding(config, model.id)
      if (binding && (typeof binding.override !== 'boolean' || typeof binding.profile_id !== 'string')) add(`${path}.profile`, 'profile_invalid')
      else if (!resolveDesktopAgentProfile(profiles, model.id, binding)) add(`${path}.profile`, binding ? 'profile_invalid' : 'profile_required')
    }
  })
  if (profiles && config.model_profiles && Object.keys(config.model_profiles).some(id => !ids.has(id))) add('model_profiles', 'profile_invalid')
  if (!validModelString(config.default_model_id) || !ids.has(config.default_model_id)) add('default_model_id', 'default_model_invalid')
  return issues
}

function validateDesktopAgentModel(model: DesktopAgentModel, path: string): DesktopAgentValidationIssue[] {
  const issues: DesktopAgentValidationIssue[] = []
  const add = (field: string, code: DesktopAgentValidationCode) => issues.push({ field: `${path}.${field}`, code })
  if (!validModelString(model.id) || /[ *?\\]/u.test(model.id)) add('id', 'model_id_invalid')
  if (!validModelString(model.display_name)) add('display_name', 'display_name_invalid')
  if (!positiveInteger(model.context_window) || model.context_window > 100_000_000) add('context_window', 'context_window_invalid')
  if (model.output_limit_kind !== undefined && !['', 'fixed', 'unlimited', 'not_published'].includes(model.output_limit_kind)) add('max_output_tokens', 'max_output_invalid')
  if (hasDesktopAgentOutputLimit(model) ? (!positiveInteger(model.max_output_tokens) || model.max_output_tokens > model.context_window) : model.max_output_tokens !== 0) add('max_output_tokens', 'max_output_invalid')
  if (!positiveInteger(model.default_max_output_tokens) || model.default_max_output_tokens > desktopAgentOutputBudgetCeiling(model)) add('default_max_output_tokens', 'default_output_invalid')
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
