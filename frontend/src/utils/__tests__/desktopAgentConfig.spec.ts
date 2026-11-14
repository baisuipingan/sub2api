import { describe, expect, it, vi } from 'vitest'
vi.mock('@/api/client', () => ({ apiClient: {} }))
import type { DesktopAgentConfig, DesktopAgentModel, DesktopAgentProfile } from '@/api/admin/desktopAgent'
import { applyDesktopAgentProfile, getDesktopAgentProfileBinding, isDesktopAgentGroupEligible, normalizeDesktopAgentProfiles, resolveDesktopAgentProfile, validDesktopAgentProfiles, validateDesktopAgentConfig } from '@/utils/desktopAgentConfig'

const valid = (): DesktopAgentConfig => ({
  enabled: true, group_id: 7, base_url: 'https://accounts.example.invalid/v1',
  default_model_id: 'fixture-model', models: [{
    id: 'fixture-model', display_name: 'Fixture Model', context_window: 128000,
    max_output_tokens: 8192, default_max_output_tokens: 4096,
    reasoning_levels: ['none', 'low', 'high'], default_reasoning: 'low',
    supports_reasoning: true, supports_streaming: true, requires_mfjs_tool_schema: false,
    input: { text: true, image: false, video: false, audio: false, pdf: false }, output: { text: true },
    supports_tool_call: true, supports_json_schema_output: false,
    supports_native_web_search: false, supports_mid_conversation_system: false,
  }],
})
const model = (config: DesktopAgentConfig) => config.models[0]!

const profile = (): DesktopAgentProfile => ({
  id: 'gpt-5.6-sol', display_name: 'GPT-5.6 Sol', source: 'official_openai', source_url: 'https://developers.openai.com/api/docs/models/gpt-5.6-sol',
  reviewed_at: '2026-11-13', revision: '2026-11-13.1', aliases: ['gpt-5.6-sol', 'gpt-5.6'], official_context_window: 1050000, max_input_tokens: 922000,
  official_reasoning_levels: ['none', 'low', 'medium', 'high', 'xhigh', 'max'], notes: ['input_budget_compatibility', 'native_web_search_unavailable', 'reasoning_none_unavailable'],
  model: { ...model(valid()), id: 'gpt-5.6-sol', display_name: 'GPT-5.6 Sol', context_window: 922000, max_output_tokens: 128000, default_max_output_tokens: 16384, reasoning_levels: ['low', 'medium', 'high', 'xhigh', 'max'], default_reasoning: 'medium' },
})

describe('desktop Agent config preflight', () => {
  it('matches only exact official IDs, explicit aliases or a valid chosen profile', () => {
    const profiles = [profile(), { ...profile(), id: 'gpt-6.1-sol', aliases: ['gpt-6.1-sol'] }]
    expect(resolveDesktopAgentProfile(profiles, 'gpt-5.6')?.id).toBe('gpt-5.6-sol')
    expect(resolveDesktopAgentProfile(profiles, 'gpt-5.6-sol-custom')).toBeUndefined()
    expect(resolveDesktopAgentProfile(profiles, 'custom', { profile_id: 'gpt-5.6-sol', override: false })?.id).toBe('gpt-5.6-sol')
    expect(resolveDesktopAgentProfile(profiles, 'gpt-5.6', { profile_id: 'other', override: true })).toBeUndefined()
    expect(resolveDesktopAgentProfile(profiles, 'gpt-5.6', { profile_id: 'gpt-6.1-sol', override: true })).toBeUndefined()
  })
  it.each(['__proto__', 'constructor', 'toString'])('keeps custom aliases as own JSON bindings: %s', id => {
    const cfg = valid(); model(cfg).id = id; cfg.default_model_id = id
    expect(getDesktopAgentProfileBinding({ ...cfg, model_profiles: {} }, id)).toBeUndefined()
    cfg.model_profiles = JSON.parse(JSON.stringify({ [id]: { profile_id: 'gpt-5.6-sol', override: false } }))
    const normalized = normalizeDesktopAgentProfiles(cfg, [profile()])
    expect(getDesktopAgentProfileBinding(normalized, id)).toEqual({ profile_id: 'gpt-5.6-sol', override: false })
    expect(JSON.parse(JSON.stringify(normalized)).model_profiles[id]).toEqual({ profile_id: 'gpt-5.6-sol', override: false })
    expect(validateDesktopAgentConfig(normalized, [profile()])).toEqual([])
  })
  it('repairs known legacy physical fields but keeps identity and valid operator defaults', () => {
    const cfg = valid(); model(cfg).id = 'gpt-5.6'; cfg.default_model_id = 'gpt-5.6'
    const before = JSON.stringify(cfg)
    const normalized = normalizeDesktopAgentProfiles(cfg, [profile()])
    expect(JSON.stringify(cfg)).toBe(before)
    expect(normalized.models[0]).toMatchObject({ id: 'gpt-5.6', display_name: 'Fixture Model', context_window: 922000, max_output_tokens: 128000, default_max_output_tokens: 4096, default_reasoning: 'low' })
    expect(normalized.model_profiles).toEqual({ 'gpt-5.6': { profile_id: 'gpt-5.6-sol', override: false } })
    expect(normalized.default_model_id).toBe('gpt-5.6')
    expect(validateDesktopAgentConfig(normalized, [profile()])).toEqual([])
  })
  it('retains only explicit advanced overrides and removes orphan bindings', () => {
    const cfg = valid(); cfg.model_profiles = { 'fixture-model': { profile_id: 'gpt-5.6-sol', override: true }, orphan: { profile_id: 'gpt-5.6-sol', override: false } }
    const normalized = normalizeDesktopAgentProfiles(cfg, [profile()])
    expect(normalized.models[0]).toEqual(model(cfg))
    expect(normalized.model_profiles).not.toHaveProperty('orphan')
    expect(validateDesktopAgentConfig(cfg, [profile()])).toContainEqual({ field: 'model_profiles', code: 'profile_invalid' })
  })
  it('requires a profile for unknown public IDs and rejects invalid known bindings', () => {
    const cfg = valid()
    expect(validateDesktopAgentConfig(cfg, [profile()])).toContainEqual({ field: 'models.0.profile', code: 'profile_required' })
    model(cfg).id = 'gpt-5.6'; cfg.default_model_id = 'gpt-5.6'; cfg.model_profiles = { 'gpt-5.6': { profile_id: 'other', override: false } }
    expect(validateDesktopAgentConfig(cfg, [profile()])).toContainEqual({ field: 'models.0.profile', code: 'profile_invalid' })
  })
  it('restores invalid defaults, or all defaults when explicitly requested', () => {
    const old = model(valid()); old.default_reasoning = 'none'; old.default_max_output_tokens = 128001
    expect(applyDesktopAgentProfile(old, profile())).toMatchObject({ default_reasoning: 'medium', default_max_output_tokens: 16384 })
    old.default_reasoning = 'low'; old.default_max_output_tokens = 4096
    expect(applyDesktopAgentProfile(old, profile(), false)).toMatchObject({ default_reasoning: 'medium', default_max_output_tokens: 16384 })
  })
  it('rejects empty, saved-derived, unsafe and incomplete capability registries', () => {
    expect(validDesktopAgentProfiles([profile()])).toBe(true)
    for (const value of [[], [null], [{ ...profile(), source: 'saved_configuration' }], [{ ...profile(), source_url: 'https://attacker.invalid' }], [{ ...profile(), aliases: ['gpt-5.6'] }], [{ ...profile(), model: { ...profile().model, input: {} } }], [profile(), profile()]]) {
      expect(validDesktopAgentProfiles(value)).toBe(false)
    }
  })
  it('accepts mixed official vendors and preserves a custom public model binding', () => {
    const profiles = [
      profile(),
      vendorProfile('deepseek-flash', 'official_deepseek', 'https://api-docs.deepseek.com/quick_start/pricing'),
      vendorProfile('claude-sonnet-4-6', 'official_anthropic', 'https://platform.claude.com/docs/en/models/sonnet-4-6/overview'),
      vendorProfile('grok-4', 'official_xai', 'https://docs.x.ai/docs/models'),
      vendorProfile('gemini-4-carbon', 'official_google', 'https://aistudio.google.com/docs/models/gemini-4-carbon'),
    ]
    expect(validDesktopAgentProfiles(profiles)).toBe(true)
    const cfg = valid()
    cfg.model_profiles = { 'fixture-model': { profile_id: 'deepseek-flash', override: false } }
    const normalized = normalizeDesktopAgentProfiles(cfg, profiles)
    expect(normalized.models[0]).toMatchObject({ id: 'fixture-model', display_name: 'Fixture Model', context_window: 922000 })
    expect(normalized.default_model_id).toBe('fixture-model')
    expect(normalized.model_profiles).toEqual(cfg.model_profiles)
    expect(validateDesktopAgentConfig(normalized, profiles)).toEqual([])
  })
  it('accepts budget-based official thinking metadata but still requires executable reasoning levels', () => {
    const budgetProfile = vendorProfile('claude-haiku-4-5-20251001', 'official_anthropic', 'https://platform.claude.com/docs/en/models/haiku-4-5/overview')
    budgetProfile.official_reasoning_levels = []
    budgetProfile.model.reasoning_levels = ['none']
    budgetProfile.model.default_reasoning = 'none'
    budgetProfile.model.supports_reasoning = false
    expect(validDesktopAgentProfiles([budgetProfile])).toBe(true)
    const cfg = valid(); cfg.model_profiles = { 'fixture-model': { profile_id: budgetProfile.id, override: false } }
    const normalized = normalizeDesktopAgentProfiles(cfg, [budgetProfile])
    expect(normalized.models[0]).toMatchObject({ reasoning_levels: ['none'], default_reasoning: 'none', supports_reasoning: false })
    expect(validateDesktopAgentConfig(normalized, [budgetProfile])).toEqual([])
    budgetProfile.model.reasoning_levels = []
    expect(validDesktopAgentProfiles([budgetProfile])).toBe(false)
    budgetProfile.model.reasoning_levels = ['none']
    budgetProfile.official_reasoning_levels = ['invented' as DesktopAgentProfile['official_reasoning_levels'][number]]
    expect(validDesktopAgentProfiles([budgetProfile])).toBe(false)
  })
  it.each([
    ['official_openai', 'https://platform.openai.com/docs/models'],
    ['official_anthropic', 'https://docs.anthropic.com/en/docs/about-claude/models'],
    ['official_google', 'https://aistudio.google.com/models'],
  ])('accepts the official documentation alias %s %s', (source, url) => {
    expect(validDesktopAgentProfiles([vendorProfile('vendor-fixture', source, url)])).toBe(true)
  })
  it.each([
    ['official_openai', 'https://docs.x.ai/docs/models'],
    ['official_anthropic', 'https://api-docs.deepseek.com/quick_start/pricing'],
    ['official_deepseek', 'https://developers.openai.com/api/docs/models'],
    ['official_google', 'https://docs.anthropic.com/en/docs'],
    ['official_xai', 'https://ai.google.dev/gemini-api/docs/models'],
    ['official_xai', 'https://docs.x.ai.attacker.invalid/models'],
    ['official_deepseek', 'http://api-docs.deepseek.com/quick_start/pricing'],
    ['official_google', 'https://user:password@ai.google.dev/gemini-api/docs/models'],
    ['official_anthropic', 'https://platform.claude.com:8443/docs'],
    ['saved_configuration', 'https://developers.openai.com/api/docs/models'],
    ['__proto__', 'https://developers.openai.com/api/docs/models'],
  ])('rejects mismatched or untrusted provenance %s %s', (source, url) => {
    expect(validDesktopAgentProfiles([vendorProfile('vendor-fixture', source, url)])).toBe(false)
  })
  it('accepts unconfigured disabled, full enabled and complete disabled without mutation', () => {
    expect(validateDesktopAgentConfig({ enabled: false, group_id: 0, base_url: '', models: [], default_model_id: '' })).toEqual([])
    const config = valid()
    const original = JSON.stringify(config)
    expect(validateDesktopAgentConfig(config)).toEqual([])
    expect(JSON.stringify(config)).toBe(original)
    config.enabled = false
    expect(validateDesktopAgentConfig(config)).toEqual([])
    // 独立管理地址与客户端账户站点可不同，不强制浏览器同源。
    config.base_url = 'https://client-accounts.example.invalid:8443/v1'
    expect(validateDesktopAgentConfig(config)).toEqual([])
  })

  it('validates each model independently without inheriting capability or budget values', () => {
    const config = valid()
    const second = structuredClone(model(config)); second.id = 'second-model'
    second.context_window = 1000000; second.max_output_tokens = 32000; second.default_max_output_tokens = 16000
    second.supports_reasoning = false; second.reasoning_levels = ['none']; second.default_reasoning = 'none'
    second.input.image = true
    config.models.push(second); config.default_model_id = second.id
    const before = JSON.stringify(config)
    expect(validateDesktopAgentConfig(config)).toEqual([])
    expect(JSON.stringify(config)).toBe(before)
    second.default_max_output_tokens = 32001
    expect(validateDesktopAgentConfig(config)).toEqual([{ field: 'models.1.default_max_output_tokens', code: 'default_output_invalid' }])
    expect(model(config).default_max_output_tokens).toBe(4096)
    expect(model(config).input.image).toBe(false)
  })

  it('requires canonical initial values and never interprets a partial close as a full save', () => {
    const initial = { enabled: false, group_id: 0, base_url: '', models: [], default_model_id: '' }
    expect(validateDesktopAgentConfig({ ...initial, group_id: 7 })).toEqual(expect.arrayContaining([
      { field: 'models', code: 'model_required' }, { field: 'base_url', code: 'gateway_invalid' },
    ]))
    const partial = { enabled: false, group_id: 0, base_url: '' } as unknown as DesktopAgentConfig
    expect(validateDesktopAgentConfig(partial)).toContainEqual({ field: 'models', code: 'models_limit' })
    expect(validateDesktopAgentConfig({ ...initial, enabled: true })).toEqual(expect.arrayContaining([
      { field: 'models', code: 'model_required' }, { field: 'default_model_id', code: 'default_model_invalid' },
    ]))
  })

  it('accepts 32 models and rejects excess models without selecting an implicit default', () => {
    const config = valid()
    config.models = Array.from({ length: 32 }, (_, index) => ({ ...structuredClone(model(valid())), id: `model-${index}` }))
    config.default_model_id = 'model-31'
    expect(validateDesktopAgentConfig(config)).toEqual([])
    config.models.push({ ...structuredClone(config.models[0]!), id: 'model-32' })
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models', code: 'models_limit' })
    expect(config.default_model_id).toBe('model-31')
  })

  it('rejects duplicate exact IDs while preserving case-sensitive configured IDs', () => {
    const config = valid()
    config.models.push(structuredClone(model(config)))
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.1.id', code: 'model_duplicate' })
    config.models[1]!.id = 'Fixture-Model'
    expect(validateDesktopAgentConfig(config)).toEqual([])
  })

  it.each(['', 'missing-model', ' fixture-model '])('requires an explicit member as default: %s', defaultId => {
    const config = valid(); config.default_model_id = defaultId
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'default_model_id', code: 'default_model_invalid' })
  })

  it('reports a missing model object at its actual index without throwing', () => {
    const config = valid(); config.models.push(null as unknown as DesktopAgentModel)
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.1', code: 'model_required' })
  })

  it.each([
    'http://accounts.example.invalid/v1', 'https://a.invalid/v1/', 'https://a.invalid/v1?',
    'https://a.invalid/v1#', 'https://a.invalid/v1?token=secret', 'https://user:pass@a.invalid/v1',
    'https://@a.invalid/v1', 'https://a.invalid/x/../v1', 'https://a.invalid/%76%31',
    ' https://a.invalid/v1', 'https://a.invalid/v1\n', 'https://a.invalid:bad/v1',
  ])('rejects ambiguous or unsafe gateway %s', (gateway) => {
    const config = valid(); config.base_url = gateway
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'base_url', code: 'gateway_invalid' })
  })

  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1])('rejects invalid group %s', (group) => {
    const config = valid(); config.group_id = group
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'group_id', code: 'group_required' })
  })

  it('requires an explicit complete model rather than assuming capabilities', () => {
    const config = valid(); config.models = []
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models', code: 'model_required' })
  })

  it.each(['', ' guessed ', 'model*', 'model?', 'with space', 'with\\slash', 'line\nbreak', 'x'.repeat(129)])('rejects invalid model id %s', (id) => {
    const config = valid(); model(config).id = id
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.0.id', code: 'model_id_invalid' })
  })

  it('counts the backend UTF-8 byte limit and rejects control/trimmed display names', () => {
    for (const name of ['模型'.repeat(22), ' label', 'label\u0000']) {
      const config = valid(); model(config).display_name = name
      expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.0.display_name', code: 'display_name_invalid' })
    }
  })

  it.each([
    ['context_window', 100_000_001, 'context_window_invalid'], ['context_window', 0, 'context_window_invalid'],
    ['max_output_tokens', 128000, 'max_output_invalid'], ['max_output_tokens', 1.5, 'max_output_invalid'],
    ['default_max_output_tokens', 8193, 'default_output_invalid'], ['default_max_output_tokens', NaN, 'default_output_invalid'],
  ] as const)('rejects invalid budget %s=%s', (field, value, code) => {
    const config = valid(); model(config)[field] = value
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: `models.0.${field}`, code })
  })

  it('does not infer omitted false flags and requires stream/tools/text', () => {
    const config = valid()
    delete (model(config) as Partial<DesktopAgentModel>).supports_json_schema_output
    model(config).input.image = undefined as unknown as boolean
    model(config).supports_streaming = false
    model(config).supports_tool_call = false
    model(config).output.text = false
    expect(validateDesktopAgentConfig(config)).toEqual(expect.arrayContaining([
      { field: 'models.0.supports_json_schema_output', code: 'capability_required' },
      { field: 'models.0.input.image', code: 'capability_required' },
      { field: 'models.0.supports_streaming', code: 'required_capability' },
      { field: 'models.0.supports_tool_call', code: 'required_capability' },
      { field: 'models.0.output.text', code: 'required_capability' },
    ]))
  })

  it.each([[], ['high', 'low'], ['low', 'low'], ['unknown']])('rejects reasoning order/subset %j', (levels) => {
    const config = valid(); model(config).reasoning_levels = levels as DesktopAgentModel['reasoning_levels']
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.0.reasoning_levels', code: 'reasoning_levels_invalid' })
  })

  it('requires default membership and none-only when reasoning is unsupported', () => {
    const config = valid(); model(config).default_reasoning = 'max'
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.0.default_reasoning', code: 'default_reasoning_invalid' })
    model(config).supports_reasoning = false
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'models.0.reasoning_levels', code: 'reasoning_unsupported' })
    model(config).reasoning_levels = ['none']; model(config).default_reasoning = 'none'
    expect(validateDesktopAgentConfig(config)).toEqual([])
  })

  it.each([
    ['active', 'standard', 'openai', true], ['active', 'standard', 'composite', true],
    ['inactive', 'standard', 'openai', false], ['active', 'subscription', 'openai', false],
    ['active', 'standard', 'anthropic', false],
  ] as const)('filters group %s/%s/%s', (status, subscription_type, platform, eligible) => {
    expect(isDesktopAgentGroupEligible({ status, subscription_type, platform })).toBe(eligible)
  })
})

function vendorProfile(id: string, source: string, sourceURL: string): DesktopAgentProfile {
  return {
    ...profile(), id, display_name: id, aliases: [id], source: source as DesktopAgentProfile['source'], source_url: sourceURL,
    model: { ...profile().model, id, display_name: id },
  }
}
