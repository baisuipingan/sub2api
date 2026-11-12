import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentSettings from '../AgentSettings.vue'
import type { DesktopAgentConfig, DesktopAgentModel } from '@/api/admin/desktopAgent'
import enMessages from '@/i18n/locales/en/admin/agentSettings'
import zhMessages from '@/i18n/locales/zh/admin/agentSettings'
import enCommon from '@/i18n/locales/en/common'
import zhCommon from '@/i18n/locales/zh/common'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn(), getGroups: vi.fn(), getModels: vi.fn(), disable: vi.fn() }))
vi.mock('@/api/admin/desktopAgent', async importOriginal => ({ ...(await importOriginal<typeof import('@/api/admin/desktopAgent')>()), getDesktopAgentConfig: mocks.getConfig, updateDesktopAgentConfig: mocks.updateConfig, getDesktopAgentModels: mocks.getModels, disableDesktopAgentConfig: mocks.disable }))
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
function fixture(): DesktopAgentConfig {
  return { enabled: true, group_id: 7, base_url: 'https://account.example.invalid/v1', models: [model()], default_model_id: 'verified-model' }
}
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail }); return { promise, resolve, reject } }
function setup(active = true, locale: 'en' | 'zh' = 'en', realSelect = false) {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en: { ...enCommon, admin: enMessages, stepUp: { adminApiKeyForbidden: 'Full administrator required', notEnabled: 'Enable TOTP first' } }, zh: { ...zhCommon, admin: zhMessages, stepUp: { adminApiKeyForbidden: '需要完整管理员权限', notEnabled: '请先启用二次验证' } } } })
  const wrapper = mount(AgentSettings, { attachTo: document.body, props: { active }, global: { plugins: [i18n], stubs: { ...(!realSelect ? { Select: SelectStub } : {}), TotpStepUpDialog: StepUpStub, Teleport: true, Transition: false } } })
  wrappers.push(wrapper); return { wrapper, i18n }
}
type Wrapper = ReturnType<typeof setup>['wrapper']
const card = (wrapper: Wrapper, index = 0) => wrapper.findAll('[data-test="agent-model-card"]')[index]
const input = (wrapper: Wrapper, key: string) => wrapper.get<HTMLInputElement>(`[data-test="agent-${key}"]`)
const modelInput = (wrapper: Wrapper, key: string, index = 0) => card(wrapper, index).get<HTMLInputElement>(`[data-test="agent-${key}"]`)
async function add(wrapper: Wrapper, id = 'second-model') { await wrapper.get('[data-test="agent-model-picker"]').setValue(id); await wrapper.get('[data-test="agent-add-model"]').trigger('click') }
beforeEach(() => {
  vi.clearAllMocks(); mocks.getConfig.mockResolvedValue(fixture())
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
    const cfg = fixture(); cfg.models.push(model('second-model', { default_max_output_tokens: 2048, input: { text: true, image: false, video: false, audio: false, pdf: false } })); mocks.getConfig.mockResolvedValueOnce(cfg)
    const { wrapper } = setup(); await flushPromises()
    await modelInput(wrapper, 'model-name', 0).setValue('First display'); await modelInput(wrapper, 'default-output', 1).setValue('8192')
    await modelInput(wrapper, 'cap-input.audio', 1).setValue(true); await modelInput(wrapper, 'cap-requires_mfjs_tool_schema', 1).setValue(true); await modelInput(wrapper, 'model-default', 1).setValue(true)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    const expected = structuredClone(cfg); expected.models[0].display_name = 'First display'; expected.models[1].default_max_output_tokens = 8192; expected.models[1].input.audio = true; expected.models[1].requires_mfjs_tool_schema = true; expected.default_model_id = 'second-model'
    expect(mocks.updateConfig).toHaveBeenCalledWith(expected); expect(wrapper.get('[role="status"]').text()).toBe(enMessages.agentSettings.saved)
  })
  it('deletes models individually and requires an explicit default after removing the default model', async () => {
    const cfg = fixture(); cfg.models.push(model('second-model')); mocks.getConfig.mockResolvedValueOnce(cfg)
    const { wrapper } = setup(); await flushPromises(); await card(wrapper).get('[data-test="agent-model-remove"]').trigger('click')
    expect(wrapper.findAll('[data-test="agent-model-card"]')).toHaveLength(1); expect(modelInput(wrapper, 'model-default').element.checked).toBe(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises(); expect(mocks.updateConfig).not.toHaveBeenCalled()
    await modelInput(wrapper, 'model-default').setValue(true); await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({ ...cfg, models: [cfg.models[1]], default_model_id: 'second-model' })
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
})
