import { describe, expect, it, vi } from 'vitest'
vi.mock('@/api/client', () => ({ apiClient: {} }))
import type { DesktopAgentConfig, DesktopAgentModel } from '@/api/admin/desktopAgent'
import { isDesktopAgentGroupEligible, validateDesktopAgentConfig } from '@/utils/desktopAgentConfig'

const valid = (): DesktopAgentConfig => ({
  enabled: true, group_id: 7, base_url: 'https://accounts.example.invalid/v1',
  model: {
    id: 'fixture-model', display_name: 'Fixture Model', context_window: 128000,
    max_output_tokens: 8192, default_max_output_tokens: 4096,
    reasoning_levels: ['none', 'low', 'high'], default_reasoning: 'low',
    supports_reasoning: true, supports_streaming: true, requires_mfjs_tool_schema: false,
    input: { text: true, image: false, video: false, audio: false, pdf: false }, output: { text: true },
    supports_tool_call: true, supports_json_schema_output: false,
    supports_native_web_search: false, supports_mid_conversation_system: false,
  },
})
const model = (config: DesktopAgentConfig) => config.model!

describe('desktop Agent config preflight', () => {
  it('accepts unconfigured disabled, full enabled and complete disabled without mutation', () => {
    expect(validateDesktopAgentConfig({ enabled: false, group_id: 0, base_url: '' })).toEqual([])
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
    const config = valid(); delete config.model
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'model', code: 'model_required' })
  })

  it.each(['', ' guessed ', 'model*', 'model?', 'with space', 'with\\slash', 'line\nbreak', 'x'.repeat(129)])('rejects invalid model id %s', (id) => {
    const config = valid(); model(config).id = id
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'model.id', code: 'model_id_invalid' })
  })

  it('counts the backend UTF-8 byte limit and rejects control/trimmed display names', () => {
    for (const name of ['模型'.repeat(22), ' label', 'label\u0000']) {
      const config = valid(); model(config).display_name = name
      expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'model.display_name', code: 'display_name_invalid' })
    }
  })

  it.each([
    ['context_window', 100_000_001, 'context_window_invalid'], ['context_window', 0, 'context_window_invalid'],
    ['max_output_tokens', 128000, 'max_output_invalid'], ['max_output_tokens', 1.5, 'max_output_invalid'],
    ['default_max_output_tokens', 8193, 'default_output_invalid'], ['default_max_output_tokens', NaN, 'default_output_invalid'],
  ] as const)('rejects invalid budget %s=%s', (field, value, code) => {
    const config = valid(); model(config)[field] = value
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: `model.${field}`, code })
  })

  it('does not infer omitted false flags and requires stream/tools/text', () => {
    const config = valid()
    delete (model(config) as Partial<DesktopAgentModel>).supports_json_schema_output
    model(config).input.image = undefined as unknown as boolean
    model(config).supports_streaming = false
    model(config).supports_tool_call = false
    model(config).output.text = false
    expect(validateDesktopAgentConfig(config)).toEqual(expect.arrayContaining([
      { field: 'model.supports_json_schema_output', code: 'capability_required' },
      { field: 'model.input.image', code: 'capability_required' },
      { field: 'model.supports_streaming', code: 'required_capability' },
      { field: 'model.supports_tool_call', code: 'required_capability' },
      { field: 'model.output.text', code: 'required_capability' },
    ]))
  })

  it.each([[], ['high', 'low'], ['low', 'low'], ['unknown']])('rejects reasoning order/subset %j', (levels) => {
    const config = valid(); model(config).reasoning_levels = levels as DesktopAgentModel['reasoning_levels']
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'model.reasoning_levels', code: 'reasoning_levels_invalid' })
  })

  it('requires default membership and none-only when reasoning is unsupported', () => {
    const config = valid(); model(config).default_reasoning = 'max'
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'model.default_reasoning', code: 'default_reasoning_invalid' })
    model(config).supports_reasoning = false
    expect(validateDesktopAgentConfig(config)).toContainEqual({ field: 'model.reasoning_levels', code: 'reasoning_unsupported' })
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
