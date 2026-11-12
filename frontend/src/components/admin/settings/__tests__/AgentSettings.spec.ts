import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentSettings from '../AgentSettings.vue'
import type { DesktopAgentConfig } from '@/api/admin/desktopAgent'
import enMessages from '@/i18n/locales/en/admin/agentSettings'
import zhMessages from '@/i18n/locales/zh/admin/agentSettings'
import enCommon from '@/i18n/locales/en/common'
import zhCommon from '@/i18n/locales/zh/common'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn(), getGroups: vi.fn() }))
vi.mock('@/api/admin/desktopAgent', async importOriginal => ({ ...(await importOriginal<typeof import('@/api/admin/desktopAgent')>()), getDesktopAgentConfig: mocks.getConfig, updateDesktopAgentConfig: mocks.updateConfig }))
vi.mock('@/api/admin/groups', async importOriginal => ({ ...(await importOriginal<typeof import('@/api/admin/groups')>()), getAllIncludingInactive: mocks.getGroups }))
const SelectStub = defineComponent({
  inheritAttrs: false, props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'],
  setup(props, { attrs, emit }) {
    return () => h('select', { ...attrs, value: props.modelValue, disabled: props.disabled, onChange: (event: Event) => { const raw = (event.target as HTMLSelectElement).value; emit('update:modelValue', props.options.find((item: { value: unknown }) => String(item.value) === raw)?.value) } }, props.options.map((item: { value: string | number; label: string; disabled?: boolean }) => h('option', { value: item.value, disabled: item.disabled }, item.label)))
  },
})
const StepUpStub = defineComponent({
  props: ['controller'], setup(props) {
    return () => props.controller.visible.value ? h('div', { 'data-test': 'step-up' }, [
      h('button', { type: 'button', 'data-test': 'step-up-verify', onClick: props.controller.onVerified }, 'Verify'),
      h('button', { type: 'button', 'data-test': 'step-up-cancel', onClick: props.controller.onCancel }, 'Cancel'),
    ]) : null
  },
})
const wrappers: ReturnType<typeof mount>[] = []
function fixture(): DesktopAgentConfig {
  return { enabled: true, group_id: 7, base_url: 'https://account.example.invalid/v1', model: {
    id: 'verified-model', display_name: 'Verified model', context_window: 128000, max_output_tokens: 16000, default_max_output_tokens: 4096,
    reasoning_levels: ['none', 'low', 'high'], default_reasoning: 'low', supports_reasoning: true, supports_streaming: true, supports_tool_call: true, requires_mfjs_tool_schema: false,
    input: { text: true, image: true, video: false, audio: false, pdf: true }, output: { text: true },
    supports_json_schema_output: true, supports_native_web_search: false, supports_mid_conversation_system: true,
  } }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
function setup(active = true, locale: 'en' | 'zh' = 'en') {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: {
    en: { ...enCommon, admin: enMessages, stepUp: { adminApiKeyForbidden: 'Full administrator required', notEnabled: 'Enable TOTP first' } },
    zh: { ...zhCommon, admin: zhMessages, stepUp: { adminApiKeyForbidden: '需要完整管理员权限', notEnabled: '请先启用二次验证' } },
  } })
  const wrapper = mount(AgentSettings, { props: { active }, global: { plugins: [i18n], stubs: { Select: SelectStub, TotpStepUpDialog: StepUpStub } } })
  wrappers.push(wrapper)
  return { wrapper, i18n }
}
const input = (wrapper: ReturnType<typeof setup>['wrapper'], key: string) => wrapper.get<HTMLInputElement>(`[data-test="agent-${key}"]`)
beforeEach(() => {
  vi.clearAllMocks()
  mocks.getConfig.mockResolvedValue(fixture())
  mocks.getGroups.mockResolvedValue([
    { id: 7, name: 'Composite balance', status: 'active', platform: 'composite', subscription_type: 'standard' },
    { id: 8, name: 'OpenAI balance', status: 'active', platform: 'openai', subscription_type: 'standard' },
    { id: 9, name: 'Inactive', status: 'inactive', platform: 'openai', subscription_type: 'standard' },
    { id: 10, name: 'Subscription', status: 'active', platform: 'openai', subscription_type: 'subscription' },
    { id: 11, name: 'Anthropic', status: 'active', platform: 'anthropic', subscription_type: 'standard' },
  ])
  mocks.updateConfig.mockImplementation(async config => 'group_id' in config ? config : { enabled: false, group_id: 0, base_url: '' })
})
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('system Agent settings', () => {
  it('loads on first activation and keeps one draft when changing tabs', async () => {
    const { wrapper } = setup(false)
    await flushPromises(); expect(mocks.getConfig).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true }); await flushPromises()
    await input(wrapper, 'model-name').setValue('Unsaved name')
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true })
    expect(input(wrapper, 'model-name').element.value).toBe('Unsaved name')
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.findAll('button').every(button => button.attributes('type') === 'button')).toBe(true)
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    wrapper.get('[data-test="agent-save"]').element.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
  })
  it('saves every explicit capability and budget using the independent endpoint', async () => {
    const { wrapper } = setup(); await flushPromises()
    await input(wrapper, 'model-name').setValue('Configured display')
    await input(wrapper, 'default-output').setValue('8192')
    await input(wrapper, 'cap-input.audio').setValue(true)
    await input(wrapper, 'cap-requires_mfjs_tool_schema').setValue(true)
    await input(wrapper, 'cap-supports_native_web_search').setValue(true)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    const expected = fixture(); expected.model!.display_name = 'Configured display'; expected.model!.default_max_output_tokens = 8192
    expected.model!.input.audio = true; expected.model!.requires_mfjs_tool_schema = true; expected.model!.supports_native_web_search = true
    expect(mocks.updateConfig).toHaveBeenCalledWith(expected)
    expect(wrapper.get('[role="status"]').text()).toBe(enMessages.agentSettings.saved)
    await wrapper.get('[data-test="agent-reload"]').trigger('click'); await flushPromises()
    expect(input(wrapper, 'model-name').element.value).toBe('Verified model')
  })
  it('keeps an unavailable selected group visible and allows disabling without losing valid fields', async () => {
    const cfg = fixture(); cfg.group_id = 9; mocks.getConfig.mockResolvedValueOnce(cfg)
    const { wrapper } = setup(); await flushPromises()
    expect(wrapper.find('[data-test="agent-group-warning"]').exists()).toBe(true)
    const options = wrapper.get('[data-test="agent-group"]').findAll('option')
    expect(options.map(option => option.attributes('value'))).toEqual(['9', '7', '8'])
    expect(options[0].attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    await input(wrapper, 'enabled').setValue(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({ ...cfg, enabled: false })
  })
  it.each([['max-output', '128000', 'max_output_invalid'], ['default-output', '16001', 'default_output_invalid'], ['context', '1.5', 'context_window_invalid'], ['gateway', 'https://account.example.invalid/v1?token=example', 'gateway_invalid'], ['model-id', 'wild*', 'model_id_invalid']])('rejects invalid %s locally', async (field, value, code) => {
    const { wrapper } = setup(); await flushPromises(); await input(wrapper, field).setValue(value)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(enMessages.agentSettings.validation[code as keyof typeof enMessages.agentSettings.validation])
  })
  it('does not silently correct an invalid reasoning default or required capability', async () => {
    const { wrapper } = setup(); await flushPromises()
    await input(wrapper, 'reasoning-low').setValue(false); await input(wrapper, 'cap-supports_tool_call').setValue(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(enMessages.agentSettings.validation.default_reasoning_invalid)
    expect(wrapper.text()).toContain(enMessages.agentSettings.validation.required_capability)
    expect((wrapper.get('[data-test="agent-default-reasoning"]').element as HTMLSelectElement).value).toBe('low')
  })
  it('keeps an unconfigured installation off without inventing model parameters', async () => {
    mocks.getConfig.mockResolvedValueOnce({ enabled: false, group_id: 0, base_url: '' })
    const { wrapper } = setup(); await flushPromises()
    expect(input(wrapper, 'enabled').element.checked).toBe(false)
    expect(wrapper.find('[data-test="agent-model-id"]').exists()).toBe(false)
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({ enabled: false, group_id: 0, base_url: '' })
    await wrapper.get('[data-test="agent-configure-model"]').trigger('click')
    expect(input(wrapper, 'model-id').element.value).toBe(''); expect(input(wrapper, 'context').element.value).toBe('0')
    expect(input(wrapper, 'gateway').element.value).toBe(`${window.location.origin}/v1`)
  })
  it('explicitly submits only enabled:false for the separate disable action', async () => {
    const { wrapper } = setup(); await flushPromises()
    await input(wrapper, 'model-name').setValue('Unsaved field'); await input(wrapper, 'max-output').setValue('0')
    await wrapper.get('[data-test="agent-disable-only"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({ enabled: false })
    expect(wrapper.get('[role="status"]').text()).toBe(enMessages.agentSettings.disabledOnly)
    expect(wrapper.find('[data-test="agent-model-name"]').exists()).toBe(false)
  })
  it.each(['configuration', 'groups'])('blocks saving after a %s loading failure and permits an explicit retry', async failed => {
    const request = failed === 'configuration' ? mocks.getConfig : mocks.getGroups
    request.mockRejectedValueOnce(new Error('private backend diagnostic'))
    const { wrapper } = setup(); await flushPromises()
    expect(wrapper.find('[data-test="agent-save"]').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toBe(enMessages.agentSettings.loadFailed)
    expect(wrapper.text()).not.toContain('private backend diagnostic')
    await wrapper.get('[data-test="agent-reload"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="agent-save"]').exists()).toBe(true)
  })
  it('retains the draft and reports a safe rejected-save message', async () => {
    mocks.updateConfig.mockRejectedValueOnce({ reason: 'DESKTOP_AGENT_INVALID', message: 'database details' })
    const { wrapper } = setup(); await flushPromises(); await input(wrapper, 'model-name').setValue('Keep my edit')
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(input(wrapper, 'model-name').element.value).toBe('Keep my edit')
    expect(wrapper.get('[role="alert"]').text()).toBe(enMessages.agentSettings.configRejected)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('freezes one payload during real step-up and retries that exact payload once', async () => {
    mocks.updateConfig.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' })
    const { wrapper } = setup(); await flushPromises()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="step-up"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="agent-reload"]').attributes('disabled')).toBeDefined()
    expect(input(wrapper, 'model-name').element.closest('fieldset')?.disabled).toBe(true)
    await input(wrapper, 'model-name').setValue('Synthetic event bypasses disabled')
    await wrapper.get('[data-test="step-up-verify"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledTimes(2)
    expect(mocks.updateConfig.mock.calls[0][0]).toBe(mocks.updateConfig.mock.calls[1][0])
    expect(mocks.updateConfig.mock.calls[1][0]).toEqual(fixture())
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
  })
  it('silently cancels step-up without a retry or success notice', async () => {
    mocks.updateConfig.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' })
    const { wrapper } = setup(); await flushPromises()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="step-up-cancel"]').trigger('click'); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false); expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('never opens a hidden-tab step-up when the first PUT rejects after leaving', async () => {
    const put = deferred<DesktopAgentConfig>(); mocks.updateConfig.mockReturnValueOnce(put.promise)
    const { wrapper } = setup(); await flushPromises()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ active: false }); put.reject({ reason: 'STEP_UP_REQUIRED' }); await flushPromises()
    expect(wrapper.find('[data-test="step-up"]').exists()).toBe(false)
    expect(mocks.updateConfig).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ active: true }); await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it('discards a late load from the tab previous activation', async () => {
    const first = deferred<DesktopAgentConfig>(); mocks.getConfig.mockReturnValueOnce(first.promise)
    const { wrapper } = setup(); await flushPromises()
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    await input(wrapper, 'model-name').setValue('Newer draft')
    first.resolve({ ...fixture(), model: { ...fixture().model!, display_name: 'Late response' } }); await flushPromises()
    expect(input(wrapper, 'model-name').element.value).toBe('Newer draft')
  })
  it('reloads the saved fact when an old save succeeds after leaving and returning', async () => {
    const put = deferred<DesktopAgentConfig>(); mocks.updateConfig.mockReturnValueOnce(put.promise)
    const { wrapper } = setup(); await flushPromises()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true })
    const saved = fixture(); saved.model!.display_name = 'Committed on server'
    mocks.getConfig.mockResolvedValueOnce(saved)
    put.resolve(saved); await flushPromises()
    expect(mocks.getConfig).toHaveBeenCalledTimes(2)
    expect(input(wrapper, 'model-name').element.value).toBe('Committed on server')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('ignores a late initial load after unmount', async () => {
    const get = deferred<DesktopAgentConfig>(); mocks.getConfig.mockReturnValueOnce(get.promise)
    const { wrapper } = setup(); await flushPromises()
    wrapper.unmount(); get.resolve(fixture()); await flushPromises()
    expect(mocks.updateConfig).not.toHaveBeenCalled()
  })
  it('does not prompt or retry after unmount', async () => {
    const put = deferred<DesktopAgentConfig>(); mocks.updateConfig.mockReturnValueOnce(put.promise)
    const { wrapper } = setup(); await flushPromises()
    await wrapper.get('[data-test="agent-save"]').trigger('click'); await flushPromises()
    wrapper.unmount(); put.reject({ reason: 'STEP_UP_REQUIRED' }); await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledTimes(1)
  })
  it('translates all visible labels in both languages without raw keys', async () => {
    const { wrapper, i18n } = setup(true, 'zh'); await flushPromises()
    expect(wrapper.text()).toContain(zhMessages.agentSettings.gateway)
    i18n.global.locale.value = 'en'; await flushPromises()
    expect(wrapper.text()).toContain(enMessages.agentSettings.gateway)
    expect(wrapper.text()).not.toMatch(/admin\.agentSettings\./); expect(wrapper.text()).not.toMatch(/[\u3400-\u9fff]/u)
  })
})
