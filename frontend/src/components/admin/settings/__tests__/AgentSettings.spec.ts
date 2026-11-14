import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentSettings from '../AgentSettings.vue'
import type { DesktopAgentConfig, DesktopAgentModel, DesktopAgentProfile } from '@/api/admin/desktopAgent'
import enMessages from '@/i18n/locales/en/admin/agentSettings'
import zhMessages from '@/i18n/locales/zh/admin/agentSettings'
import enCommon from '@/i18n/locales/en/common'
import zhCommon from '@/i18n/locales/zh/common'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn(), getGroups: vi.fn(), getModels: vi.fn(), disable: vi.fn(), getProfiles: vi.fn() }))
vi.mock('@/api/admin/desktopAgent', async importOriginal => ({ ...(await importOriginal<typeof import('@/api/admin/desktopAgent')>()), getDesktopAgentConfig: mocks.getConfig, updateDesktopAgentConfig: mocks.updateConfig, getDesktopAgentModels: mocks.getModels, disableDesktopAgentConfig: mocks.disable, getDesktopAgentProfiles: mocks.getProfiles }))
vi.mock('@/api/admin/groups', async importOriginal => ({ ...(await importOriginal<typeof import('@/api/admin/groups')>()), getAllIncludingInactive: mocks.getGroups }))
const SelectStub = defineComponent({
  inheritAttrs: false, props: ['modelValue', 'options', 'disabled', 'searchable', 'creatable'], emits: ['update:modelValue'],
  setup(props, { attrs, emit }) {
    return () => h('select', { ...attrs, value: props.modelValue, disabled: props.disabled, 'data-searchable': props.searchable, 'data-creatable': props.creatable, onChange: (event: Event) => { const raw = (event.target as HTMLSelectElement).value; emit('update:modelValue', props.options.find((item: { value: unknown }) => String(item.value) === raw)?.value) } }, props.options.map((item: { value: string | number; label: string; disabled?: boolean }) => h('option', { value: item.value, disabled: item.disabled }, item.label)))
  },
})
const StepUpStub = defineComponent({ props: ['controller'], setup: props => () => props.controller.visible.value ? h('div', { 'data-test': 'step-up' }, [h('button', { type: 'button', 'data-test': 'step-up-verify', onClick: props.controller.onVerified }, 'Verify'), h('button', { type: 'button', 'data-test': 'step-up-cancel', onClick: props.controller.onCancel }, 'Cancel')]) : null })
const wrappers: ReturnType<typeof mount>[] = []
function model(id = 'verified-model', overrides: Partial<DesktopAgentModel> = {}): DesktopAgentModel {
  return { id, display_name: `Display ${id}`, context_window: 128000, max_output_tokens: 16000, default_max_output_tokens: 4096, reasoning_levels: ['none', 'low', 'high'], default_reasoning: 'low', supports_reasoning: true, supports_streaming: true, supports_tool_call: true, requires_mfjs_tool_schema: false, input: { text: true, image: true, video: false, audio: false, pdf: true }, output: { text: true }, supports_json_schema_output: true, supports_native_web_search: false, supports_mid_conversation_system: true, ...overrides }
}
function profile(): DesktopAgentProfile {
  return { id: 'gpt-6.1-sol', display_name: 'GPT-6.1 Sol', source: 'official_openai', source_url: 'https://developers.openai.com/api/docs/models/gpt-6.1-sol', reviewed_at: '2026-11-13', revision: '2026-11-13.1', aliases: ['gpt-6.1-sol'], official_context_window: 1050000, max_input_tokens: 922000, official_reasoning_levels: ['low', 'medium', 'high', 'xhigh', 'max'], notes: ['input_budget_compatibility', 'native_web_search_unavailable'], model: model('gpt-6.1-sol', { context_window: 922000, max_output_tokens: 128000, default_max_output_tokens: 16384, reasoning_levels: ['low', 'medium', 'high', 'xhigh', 'max'], default_reasoning: 'medium' }) }
}
function fixture(): DesktopAgentConfig {
  return { enabled: true, group_id: 7, base_url: 'https://account.example.invalid/v1', models: [model()], default_model_id: 'verified-model', model_profiles: { 'verified-model': { profile_id: 'gpt-6.1-sol', override: true } } }
}
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail }); return { promise, resolve, reject } }
function setup(active = true, locale: 'en' | 'zh' = 'en', realSelect = false) {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en: { ...enCommon, admin: enMessages, stepUp: { adminApiKeyForbidden: 'Full administrator required', notEnabled: 'Enable TOTP first' } }, zh: { ...zhCommon, admin: zhMessages, stepUp: { adminApiKeyForbidden: '需要完整管理员权限', notEnabled: '请先启用二次验证' } } } })
  const wrapper = mount(AgentSettings, { attachTo: document.body, props: { active }, global: { plugins: [i18n], stubs: { DesktopReleaseStorageSettings: true, ...(!realSelect ? { Select: SelectStub } : {}), TotpStepUpDialog: StepUpStub, Teleport: true, Transition: false } } })
  wrappers.push(wrapper); return { wrapper, i18n }
}
type Wrapper = ReturnType<typeof setup>['wrapper']
const card = (wrapper: Wrapper, index = 0) => wrapper.findAll('[data-test="agent-model-card"]')[index]
const input = (wrapper: Wrapper, key: string) => wrapper.get<HTMLInputElement>(`[data-test="agent-${key}"]`)
const modelInput = (wrapper: Wrapper, key: string, index = 0) => card(wrapper, index).get<HTMLInputElement>(`[data-test="agent-${key}"]`)
async function add(wrapper: Wrapper, id = 'second-model') { await wrapper.get('[data-test="agent-model-picker"]').setValue(id); await wrapper.get('[data-test="agent-add-model"]').trigger('click') }
beforeEach(() => {
  vi.clearAllMocks(); mocks.getProfiles.mockResolvedValue({ profiles: [profile()] }); mocks.getConfig.mockResolvedValue(fixture())
  mocks.getGroups.mockResolvedValue([{ id: 7, name: 'Composite balance', status: 'active', platform: 'composite', subscription_type: 'standard' }, { id: 8, name: 'OpenAI balance', status: 'active', platform: 'openai', subscription_type: 'standard' }, { id: 9, name: 'Inactive', status: 'inactive', platform: 'openai', subscription_type: 'standard' }, { id: 10, name: 'Subscription', status: 'active', platform: 'openai', subscription_type: 'subscription' }, { id: 11, name: 'Anthropic', status: 'active', platform: 'anthropic', subscription_type: 'standard' }])
  mocks.getModels.mockImplementation(async groupId => ({ group_id: groupId, models: ['verified-model', 'second-model', 'third-model'] }))
  mocks.updateConfig.mockImplementation(async config => config)
  mocks.disable.mockImplementation(async () => ({ ...fixture(), enabled: false }))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })

describe('system Agent multiple model settings', () => {
  it('loads on activation, retrieves the selected group catalog and keeps one draft', async () => {
    const { wrapper } = setup(false); await flushPromises(); expect(mocks.getConfig).not.toHaveBeenCalled(); expect(mocks.getModels).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true }); await flushPromises(); expect(mocks.getModels).toHaveBeenCalledWith(7)
    await modelInput(wrapper, 'model-name').setValue('Unsaved name')
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    expect(modelInput(wrapper, 'model-name').element.value).toBe('Unsaved name'); expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    expect(wrapper.find('form').exists()).toBe(false); expect(wrapper.findAll('button').every(button => button.attributes('type') === 'button')).toBe(true)
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }); wrapper.get('[data-test="agent-save"]').element.dispatchEvent(event); expect(event.defaultPrevented).toBe(false)
  })
  it('adds one selected catalog model without copying another model capabilities or choosing a default', async () => {
    const { wrapper } = setup(); await flushPromises(); await add(wrapper)
    expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(2)
    expect(modelInput(wrapper, 'model-name', 1).element.value).toBe('second-model')
    expect(modelInput(wrapper, 'context', 1).element.value).toBe('0')
    expect(modelInput(wrapper, 'cap-input.image', 1).element.checked).toBe(false)
    expect(modelInput(wrapper, 'cap-input.image', 0).element.checked).toBe(true)
    expect(modelInput(wrapper, 'model-default', 0).element.checked).toBe(true)
    const options = wrapper.get('[data-test="agent-model-picker"]').findAll('option').map(option => option.attributes('value'))
    expect(options).not.toContain('second-model'); expect(wrapper.find('input[data-test="agent-model-id"]').exists()).toBe(false)
    expect(mocks.updateConfig).not.toHaveBeenCalled()
  })
  it('rejects forged picker values and duplicate selection events', async () => {
    const { wrapper } = setup(); await flushPromises()
    const picker = wrapper.findComponent('[data-test="agent-model-picker"]')
    picker.vm.$emit('update:modelValue', 'invented-model'); await flushPromises()
    expect(wrapper.get('[data-test="agent-add-model"]').attributes('disabled')).toBeDefined()
    picker.vm.$emit('update:modelValue', 'verified-model'); await flushPromises()
    expect(wrapper.get('[data-test="agent-add-model"]').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(1)
  })
  it('does not add a 33rd model even when it is present in the group catalog', async () => {
    const cfg = fixture(); cfg.models = Array.from({ length: 32 }, (_, index) => model(`model-${index}`)); cfg.default_model_id = 'model-0'
    mocks.getConfig.mockResolvedValueOnce(cfg); mocks.getModels.mockResolvedValueOnce({ group_id: 7, models: [...cfg.models.map(item => item.id), 'extra-model'] })
    const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-model-picker"]').setValue('extra-model')
    expect(wrapper.get('[data-test="agent-add-model"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="agent-add-model"]').trigger('click'); expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(32)
  })
  it('saves separate model capabilities, budgets and the selected default with all fields', async () => {
    const cfg = fixture(); cfg.models.push(model('second-model', { default_max_output_tokens: 2048, input: { text: true, image: false, video: false, audio: false, pdf: false } })); cfg.model_profiles!['second-model'] = { profile_id: 'gpt-6.1-sol', override: true }; mocks.getConfig.mockResolvedValueOnce(cfg)
    const { wrapper } = setup(); await flushPromises()
    await modelInput(wrapper, 'model-name', 0).setValue('First display'); await modelInput(wrapper, 'default-output', 1).setValue('8192')
    await modelInput(wrapper, 'cap-input.audio', 1).setValue(true); await modelInput(wrapper, 'cap-requires_mfjs_tool_schema', 1).setValue(true); await modelInput(wrapper, 'model-default', 1).setValue(true)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    const expected = structuredClone(cfg); expected.models[0].display_name = 'First display'; expected.models[1].default_max_output_tokens = 8192; expected.models[1].input.audio = true; expected.models[1].requires_mfjs_tool_schema = true; expected.default_model_id = 'second-model'
    expect(mocks.updateConfig).toHaveBeenCalledWith(expected); expect(wrapper.get('[role="status"]').text()).toBe(enMessages.agentSettings.saved)
  })
  it('deletes models individually and requires an explicit default after removing the default model', async () => {
    const cfg = fixture(); cfg.models.push(model('second-model')); cfg.model_profiles!['second-model'] = { profile_id: 'gpt-6.1-sol', override: true }; mocks.getConfig.mockResolvedValueOnce(cfg)
    const { wrapper } = setup(); await flushPromises(); await card(wrapper).get('[data-test="agent-model-remove"]').trigger('click')
    expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(1); expect(modelInput(wrapper, 'model-default').element.checked).toBe(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
    await modelInput(wrapper, 'model-default').setValue(true); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({ ...cfg, models: [cfg.models[1]], default_model_id: 'second-model', model_profiles: { 'second-model': cfg.model_profiles!['second-model'] } })
  })
  it('shows natural empty state, needs a group first, and does not create model settings automatically', async () => {
    const empty = { enabled: false, group_id: 0, base_url: '', models: [], default_model_id: '' }; mocks.getConfig.mockResolvedValueOnce(empty)
    const { wrapper } = setup(); await flushPromises(); expect(mocks.getModels).not.toHaveBeenCalled(); expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(0)
    expect(wrapper.get('[data-test="agent-add-model"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="agent-group"]').setValue(7); await flushPromises(); await add(wrapper)
    expect(modelInput(wrapper, 'model-default').element.checked).toBe(false); expect(modelInput(wrapper, 'context').element.value).toBe('0')
    expect(input(wrapper, 'gateway').element.value).toBe(`${window.location.origin}/v1`)
  })
  it('does not publish an empty initial configuration and exposes no connection switches', async () => {
    mocks.getConfig.mockResolvedValueOnce({ enabled: false, group_id: 0, base_url: '', models: [], default_model_id: '' })
    const { wrapper } = setup(); await flushPromises()
    expect(wrapper.get('[data-test="agent-config-status"]').text()).toBe(enMessages.agentSettings.unconfigured)
    expect(wrapper.find('[data-test="agent-enabled"]').exists()).toBe(false); expect(wrapper.find('[data-test="agent-disable"]').exists()).toBe(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
  })
  it('shows an unpublished legacy configuration and enables it only on valid complete save', async () => {
    mocks.getConfig.mockResolvedValueOnce({ ...fixture(), enabled: false })
    const { wrapper } = setup(); await flushPromises()
    expect(wrapper.get('[data-test="agent-config-status"]').text()).toBe(enMessages.agentSettings.unpublished)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith(fixture()); expect(wrapper.get('[data-test="agent-config-status"]').text()).toBe(enMessages.agentSettings.configured)
  })
  it('preserves cards after changing group and blocks models outside the new catalog', async () => {
    mocks.getModels.mockImplementation(async groupId => ({ group_id: groupId, models: groupId === 7 ? ['verified-model'] : ['second-model'] }))
    const { wrapper } = setup(); await flushPromises(); await modelInput(wrapper, 'model-name').setValue('Keep draft')
    await wrapper.get('[data-test="agent-group"]').setValue(8); await flushPromises()
    expect(modelInput(wrapper, 'model-name').element.value).toBe('Keep draft'); expect(card(wrapper).find('[data-test="agent-model-unavailable"]').exists()).toBe(true)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
  })
  it('keeps a saved model absent from the catalog visible and invalid', async () => {
    mocks.getModels.mockResolvedValueOnce({ group_id: 7, models: ['second-model'] })
    const { wrapper } = setup(); await flushPromises(); expect(card(wrapper).text()).toContain('verified-model'); expect(card(wrapper).find('[data-test="agent-model-unavailable"]').exists()).toBe(true)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
  })
  it.each(['failure', 'empty', 'wrong-group'])('blocks complete save for catalog %s without a misleading disable control', async state => {
    if (state === 'failure') mocks.getModels.mockRejectedValueOnce(new Error('private upstream'))
    if (state === 'empty') mocks.getModels.mockResolvedValueOnce({ group_id: 7, models: [] })
    if (state === 'wrong-group') mocks.getModels.mockResolvedValueOnce({ group_id: 8, models: ['verified-model'] })
    const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
    await modelInput(wrapper, 'model-name').setValue('Unsaved change')
    expect(wrapper.find('[data-test="agent-disable"]').exists()).toBe(false); expect(mocks.disable).not.toHaveBeenCalled(); expect(modelInput(wrapper, 'model-name').element.value).toBe('Unsaved change')
  })
  it('rejects late group A responses after switching to B and allows retry after a failure', async () => {
    const first = deferred<{ group_id: number; models: string[] }>(); mocks.getModels.mockReturnValueOnce(first.promise)
    const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-group"]').setValue(8); await flushPromises()
    first.resolve({ group_id: 7, models: ['old-only-model'] }); await flushPromises()
    const options = wrapper.get('[data-test="agent-model-picker"]').findAll('option').map(option => option.attributes('value')); expect(options).not.toContain('old-only-model')
    mocks.getModels.mockRejectedValueOnce(new Error('failed')); await wrapper.get('[data-test="agent-refresh-models"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="agent-refresh-models"]').trigger('click'); await flushPromises(); expect(mocks.getModels.mock.calls.at(-1)).toEqual([8])
  })
  it.each([['max-output', '128000', 'max_output_invalid'], ['default-output', '16001', 'default_output_invalid'], ['context', '1.5', 'context_window_invalid']])('keeps per-model %s validation', async (field, value, code) => {
    const { wrapper } = setup(); await flushPromises(); await modelInput(wrapper, field).setValue(value); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).not.toHaveBeenCalled(); expect(wrapper.text()).toContain(enMessages.agentSettings.validation[code as keyof typeof enMessages.agentSettings.validation])
  })
  it('retains reasoning validation without silently correcting defaults', async () => {
    const { wrapper } = setup(); await flushPromises(); await modelInput(wrapper, 'reasoning-low').setValue(false); await modelInput(wrapper, 'cap-supports_tool_call').setValue(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled(); expect(wrapper.text()).toContain(enMessages.agentSettings.validation.default_reasoning_invalid)
  })
  it.each(['configuration', 'groups'])('blocks mutation after %s load failure', async failed => {
    (failed === 'configuration' ? mocks.getConfig : mocks.getGroups).mockRejectedValueOnce(new Error('private diagnostic')); const { wrapper } = setup(); await flushPromises()
    expect(wrapper.find('[data-test="agent-save"]').exists()).toBe(false); expect(wrapper.text()).not.toContain('private diagnostic')
    await wrapper.get('[data-test="agent-reload"]').trigger('click'); await flushPromises(); expect(wrapper.find('[data-test="agent-save"]').exists()).toBe(true)
  })
  it('retains a rejected save draft and reports a safe error', async () => {
    mocks.updateConfig.mockRejectedValueOnce({ reason: 'DESKTOP_AGENT_INVALID', message: 'private diagnostic' }); const { wrapper } = setup(); await flushPromises(); await modelInput(wrapper, 'model-name').setValue('Keep edit')
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(modelInput(wrapper, 'model-name').element.value).toBe('Keep edit'); expect(wrapper.get('[role="alert"]').text()).toBe(enMessages.agentSettings.configRejected)
  })
  it('freezes one complete payload through real step-up and retries it once', async () => {
    mocks.updateConfig.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' }); const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="step-up"]').exists()).toBe(true); expect(wrapper.get('[data-test="agent-reload"]').attributes('disabled')).toBeDefined()
    await modelInput(wrapper, 'model-name').setValue('Synthetic later edit'); await wrapper.get('[data-test="step-up-verify"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledTimes(2); expect(mocks.updateConfig.mock.calls[0][0]).toBe(mocks.updateConfig.mock.calls[1][0]); expect(mocks.updateConfig.mock.calls[1][0]).toEqual(fixture())
  })
  it('silently cancels step-up without retry or success', async () => {
    mocks.updateConfig.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' }); const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); await wrapper.get('[data-test="step-up-cancel"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledTimes(1); expect(wrapper.find('[role="alert"]').exists()).toBe(false); expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('does not prompt after leaving while the first save response is pending', async () => {
    const request = deferred<DesktopAgentConfig>(); mocks.updateConfig.mockReturnValueOnce(request.promise)
    const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); await wrapper.setProps({ active: false }); request.reject({ reason: 'STEP_UP_REQUIRED' }); await flushPromises()
    expect(wrapper.find('[data-test="step-up"]').exists()).toBe(false); expect(mocks.updateConfig).toHaveBeenCalledTimes(1)
  })
  it('reloads the catalog after returning while a pending save is cancelled', async () => {
    const request = deferred<DesktopAgentConfig>(); mocks.updateConfig.mockReturnValueOnce(request.promise)
    const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    request.reject({ reason: 'STEP_UP_REQUIRED' }); await flushPromises()
    expect(mocks.getModels).toHaveBeenCalledTimes(2); expect(wrapper.find('[data-test="step-up"]').exists()).toBe(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).toHaveBeenCalledTimes(2)
  })
  it('discards old activation loads and catalog results after leaving', async () => {
    const first = deferred<DesktopAgentConfig>(); mocks.getConfig.mockReturnValueOnce(first.promise); const { wrapper } = setup(); await flushPromises()
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises(); await modelInput(wrapper, 'model-name').setValue('Newer draft')
    first.resolve({ ...fixture(), models: [model('late-model')] }); await flushPromises(); expect(modelInput(wrapper, 'model-name').element.value).toBe('Newer draft')
  })
  it('does not retry a pending save after unmount', async () => {
    const request = deferred<DesktopAgentConfig>(); mocks.updateConfig.mockReturnValueOnce(request.promise); const { wrapper } = setup(); await flushPromises(); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); wrapper.unmount(); request.reject({ reason: 'STEP_UP_REQUIRED' }); await flushPromises(); expect(mocks.updateConfig).toHaveBeenCalledTimes(1)
  })
  it('uses a real searchable non-creatable Select with keyboard selection and closes it on refresh', async () => {
    const { wrapper } = setup(true, 'en', true); await flushPromises(); const picker = wrapper.get('[data-test="agent-model-picker"]'); await picker.get('button[aria-haspopup]').trigger('click')
    const search = wrapper.get('.select-search-input'); await search.setValue('SECOND'); expect(wrapper.findAll('[role="option"]').map(option => option.text())).toEqual(['second-model'])
    await search.trigger('keydown', { key: 'ArrowDown' }); await search.trigger('keydown', { key: 'Enter' }); await wrapper.get('[data-test="agent-add-model"]').trigger('click'); expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(2)
    await wrapper.get('[data-test="agent-model-picker"]').get('button[aria-haspopup]').trigger('click'); await wrapper.get('.select-search-input').setValue('invented-model'); expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
    await wrapper.get('[data-test="agent-refresh-models"]').trigger('click'); expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
  })
  it('translates all visible labels without raw keys', async () => {
    const { wrapper, i18n } = setup(true, 'zh'); await flushPromises(); expect(wrapper.text()).toContain(zhMessages.agentSettings.gateway); i18n.global.locale.value = 'en'; await flushPromises(); expect(wrapper.text()).not.toMatch(/admin\.agentSettings\./); expect(wrapper.text()).not.toMatch(/[\u3400-\u9fff]/u)
  })
  it('keeps frequently used defaults outside the collapsed capability and connection overrides', async () => {
    const { wrapper } = setup(); await flushPromises()
    expect(modelInput(wrapper, 'default-output').element.closest('details')).toBeNull()
    expect(card(wrapper).get('[data-test="agent-default-reasoning"]').element.closest('details')).toBeNull()
    expect(modelInput(wrapper, 'context').element.closest('details')).not.toBeNull()
    expect(input(wrapper, 'gateway').element.closest('details')).not.toBeNull()
    expect(card(wrapper).get('[data-test="agent-capability-summary"]').text()).toContain('128,000')
    expect(card(wrapper).text()).toContain(enMessages.agentSettings.defaultMaxOutputTokens)
  })
  it('applies a trusted profile atomically without renaming an alias or changing the default model', async () => {
    const { wrapper } = setup(); await flushPromises(); await add(wrapper)
    expect(card(wrapper, 1).get('[data-test="agent-profile-required"]').exists()).toBe(true)
    await card(wrapper, 1).get('[data-test="agent-profile"]').setValue('gpt-6.1-sol')
    expect(card(wrapper, 1).attributes('data-model-id')).toBe('second-model')
    expect(modelInput(wrapper, 'model-name', 1).element.value).toBe('second-model')
    expect(modelInput(wrapper, 'context', 1).element.value).toBe('922000')
    expect(modelInput(wrapper, 'context', 1).element.disabled || modelInput(wrapper, 'context', 1).element.matches(':disabled')).toBe(true)
    expect(modelInput(wrapper, 'default-output', 1).element.value).toBe('16384')
    expect(modelInput(wrapper, 'model-default', 0).element.checked).toBe(true)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig.mock.calls[0][0].model_profiles['second-model']).toEqual({ profile_id: 'gpt-6.1-sol', override: false })
    expect(mocks.updateConfig.mock.calls[0][0].models[1].input.image).toBe(true)
  })
  it('blocks unknown public IDs until a profile is chosen', async () => {
    const { wrapper } = setup(); await flushPromises(); await add(wrapper)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain(enMessages.agentSettings.validation.profile_required)
    expect(mocks.updateConfig).not.toHaveBeenCalled()
  })
  it('shows no trusted capability summary for a legacy custom ID without a binding', async () => {
    const cfg = fixture(); delete cfg.model_profiles; mocks.getConfig.mockResolvedValueOnce(cfg)
    const { wrapper } = setup(); await flushPromises()
    expect(card(wrapper).get('[data-test="agent-capability-summary"]').text()).toBe(enMessages.agentSettings.unverifiedSummary)
    expect(card(wrapper).get('[data-test="agent-capability-summary"]').text()).not.toContain('128,000')
    expect(modelInput(wrapper, 'context').element.value).toBe('128000')
    expect(modelInput(wrapper, 'context').element.matches(':disabled')).toBe(true)
  })
  it('normalizes known official IDs automatically while preserving valid operator defaults', async () => {
    const cfg = fixture(); cfg.models = [model('gpt-6.1-sol', { context_window: 42, max_output_tokens: 40 })]; cfg.default_model_id = 'gpt-6.1-sol'; delete cfg.model_profiles
    mocks.getConfig.mockResolvedValueOnce(cfg); mocks.getModels.mockResolvedValueOnce({ group_id: 7, models: ['gpt-6.1-sol'] })
    const { wrapper } = setup(); await flushPromises()
    expect(modelInput(wrapper, 'context').element.value).toBe('922000')
    expect(modelInput(wrapper, 'default-output').element.value).toBe('4096')
    expect(card(wrapper).get('[data-test="agent-default-reasoning"]').element.value).toBe('low')
    expect(modelInput(wrapper, 'context').element.matches(':disabled')).toBe(true)
    expect(card(wrapper).get('[data-test="agent-profile"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig.mock.calls[0][0].model_profiles['gpt-6.1-sol']).toEqual({ profile_id: 'gpt-6.1-sol', override: false })
  })
  it.each(['zh', 'en'] as const)('renders mixed-vendor budgets and execution limits without changing the automatic reasoning default: %s', async locale => {
    const automatic = { reasoning_levels: ['none'] as DesktopAgentModel['reasoning_levels'], default_reasoning: 'none' as const, supports_reasoning: false, supports_json_schema_output: false }
    const reviewed = (id: string, source: DesktopAgentProfile['source'], sourceUrl: string, notes: string[], officialLevels: DesktopAgentProfile['official_reasoning_levels'] = ['low', 'medium', 'high']): DesktopAgentProfile => ({
      ...profile(), id, display_name: id, source, source_url: sourceUrl, aliases: [id], official_context_window: 1000000, max_input_tokens: 872000, notes, official_reasoning_levels: officialLevels,
      model: model(id, { ...automatic, context_window: 872000, max_output_tokens: 128000, default_max_output_tokens: 16384 }),
    })
    const profiles = [
      profile(),
      reviewed('deepseek-flash', 'official_deepseek', 'https://api-docs.deepseek.com/quick_start/pricing', ['shared_context_budget', 'output_budget_compatibility', 'native_web_search_unavailable']),
      reviewed('claude-haiku-4-5-20251001', 'official_anthropic', 'https://platform.claude.com/docs/en/models/haiku-4-5/overview', ['shared_context_budget', 'reasoning_auto_only', 'structured_output_compatibility', 'unreviewed-message'], []),
      reviewed('grok-4', 'official_xai', 'https://docs.x.ai/docs/models', ['grok_xhigh_compatibility']),
      reviewed('gemini-4-carbon', 'official_google', 'https://aistudio.google.com/docs/models/gemini-4-carbon', ['responses_compatibility_required', 'reasoning_auto_only']),
    ]
    const cfg = fixture(); cfg.models = profiles.map(item => structuredClone(item.model)); cfg.default_model_id = profiles[0].id; delete cfg.model_profiles
    mocks.getProfiles.mockResolvedValueOnce({ profiles }); mocks.getConfig.mockResolvedValueOnce(cfg); mocks.getModels.mockResolvedValueOnce({ group_id: 7, models: profiles.map(item => item.id) })
    const { wrapper } = setup(true, locale); await flushPromises()
    const messages = (locale === 'zh' ? zhMessages : enMessages).agentSettings
    expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(5)
    expect(card(wrapper, 0).get('[data-test="agent-profile-budgets"]').text()).toContain(locale === 'zh' ? '最大输入' : 'maximum input')
    const sharedBudget = card(wrapper, 1).get('[data-test="agent-profile-budgets"]').text()
    expect(sharedBudget).toContain(locale === 'zh' ? '输入与输出共享上下文' : 'shared input/output context')
    expect(sharedBudget).toContain(locale === 'zh' ? '保守输入预算 872,000' : 'conservative input budget 872,000')
    expect(sharedBudget).not.toContain(locale === 'zh' ? '最大输入' : 'maximum input')
    expect(card(wrapper, 1).get('[data-test="agent-profile-note-output_budget_compatibility"]').text()).toContain('128,000')
    expect(card(wrapper, 1).get('[data-test="agent-profile-note-native_web_search_unavailable"]').text()).toBe(messages.profileNotes.native_web_search_unavailable)
    for (const note of ['shared_context_budget', 'reasoning_auto_only', 'structured_output_compatibility'] as const) expect(card(wrapper, 2).get(`[data-test="agent-profile-note-${note}"]`).text()).toBe(messages.profileNotes[note])
    expect(card(wrapper, 2).find('[data-test="agent-profile-note-unreviewed-message"]').exists()).toBe(false)
    expect(card(wrapper, 3).get('[data-test="agent-profile-note-grok_xhigh_compatibility"]').text()).toBe(messages.profileNotes.grok_xhigh_compatibility)
    expect(card(wrapper, 4).get('[data-test="agent-profile-note-responses_compatibility_required"]').text()).toBe(messages.profileNotes.responses_compatibility_required)
    const reasoning = card(wrapper, 4).get<HTMLSelectElement>('[data-test="agent-default-reasoning"]')
    expect(reasoning.element.value).toBe('none')
    expect(reasoning.findAll('option').filter(option => !option.element.disabled).map(option => option.attributes('value'))).toEqual(['none'])
    expect(wrapper.text()).not.toMatch(/admin\.agentSettings\.|unreviewed-message/)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledOnce()
    expect(mocks.updateConfig.mock.calls[0][0].models[4]).toMatchObject({ id: 'gemini-4-carbon', default_reasoning: 'none', reasoning_levels: ['none'], supports_reasoning: false })
  })
  it('requires explicit overrides and restores profile defaults in one draft', async () => {
    const { wrapper } = setup(); await flushPromises()
    await modelInput(wrapper, 'default-output').setValue('8192')
    await modelInput(wrapper, 'context').setValue('256000')
    await modelInput(wrapper, 'profile-override').setValue(false)
    expect(modelInput(wrapper, 'context').element.value).toBe('922000')
    expect(modelInput(wrapper, 'default-output').element.value).toBe('8192')
    expect(modelInput(wrapper, 'context').element.matches(':disabled')).toBe(true)
    await modelInput(wrapper, 'profile-override').setValue(true)
    expect(modelInput(wrapper, 'context').element.matches(':disabled')).toBe(false)
    await card(wrapper).get('[data-test="agent-restore-profile"]').trigger('click')
    expect(modelInput(wrapper, 'default-output').element.value).toBe('16384')
    expect(modelInput(wrapper, 'profile-override').element.checked).toBe(false)
    expect(card(wrapper).get('[data-test="agent-default-reasoning"]').element.value).toBe('medium')
  })
  it.each(['network', 'malformed'])('blocks publish on %s profile failure and retries without discarding edits', async failure => {
    if (failure === 'network') mocks.getProfiles.mockRejectedValueOnce(new Error('profile unavailable'))
    else mocks.getProfiles.mockResolvedValueOnce({ profiles: [{ id: 'saved-config', source: 'saved_configuration' }] })
    const { wrapper } = setup(); await flushPromises()
    expect(wrapper.get('[data-test="agent-profiles-failed"]').text()).toBe(enMessages.agentSettings.profilesFailed)
    await modelInput(wrapper, 'model-name').setValue('Unsaved identity')
    expect(wrapper.get('[data-test="agent-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
    await wrapper.get('[data-test="agent-retry-profiles"]').trigger('click'); await flushPromises()
    expect(modelInput(wrapper, 'model-name').element.value).toBe('Unsaved identity')
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig.mock.calls[0][0].models[0].display_name).toBe('Unsaved identity')
  })
})
