import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkbenchSettingsView from '../WorkbenchSettingsView.vue'

const { getConfig, updateConfig, getGroups, showError, showSuccess, refreshPublicSettings, stepUpRun } = vi.hoisted(() => ({
  getConfig: vi.fn(), updateConfig: vi.fn(), getGroups: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), refreshPublicSettings: vi.fn(),
  stepUpRun: vi.fn((action: () => unknown) => action()),
}))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: getGroups } } }))
vi.mock('@/api/admin/workbench', () => ({ getWorkbenchConfig: getConfig, updateWorkbenchConfig: updateConfig }))
vi.mock('@/api/admin/desktopAgent', () => ({ DESKTOP_AGENT_REASONING_LEVELS: ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError, showSuccess, fetchPublicSettings: refreshPublicSettings }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params?.id ? `${key}:${params.id}` : key }) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: stepUpRun }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))

const base = { image_studio_enabled: true, image_studio_group_id: 7, agent_group_id: 8 }
const wrappers: ReturnType<typeof mount>[] = []
function mountSettings() {
  const wrapper = mount(WorkbenchSettingsView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TotpStepUpDialog: true,
    ImageStorageSettings: true,
    OfficeStorageSettings: true,
    WorkbenchSearchSettings: true,
    RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
    Select: {
      props: ['modelValue', 'options'], emits: ['update:modelValue'],
      template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', options.find(option => String(option.value) === $event.target.value)?.value)"><option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">{{ option.label }}</option></select>',
    },
  } } })
  wrappers.push(wrapper)
  return wrapper
}

describe('WorkbenchSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({ ...base })
    updateConfig.mockImplementation(async payload => ({ ...payload, agent_group_id: base.agent_group_id }))
    getGroups.mockResolvedValue([
      { id: 7, name: 'Image group', status: 'active', allow_image_generation: true },
      { id: 8, name: 'Chat only', status: 'active', allow_image_generation: false, platform: 'composite', subscription_type: 'standard' },
      { id: 9, name: 'Inactive group', status: 'inactive', allow_image_generation: true },
    ])
    refreshPublicSettings.mockResolvedValue(null)
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('shows the existing image storage component and only eligible image groups', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ImageStorageSettings' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'OfficeStorageSettings' }).exists()).toBe(true)
    expect(wrapper.get('#workbench-group').text()).toContain('Image group')
    expect(wrapper.get('#workbench-group').text()).not.toContain('Chat only')
    expect(wrapper.get('#workbench-group').text()).not.toContain('Inactive group')
    expect(wrapper.get('#workbench-entry-enabled').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('#workbench-agent-group').text()).toBe('Chat only')
    expect(wrapper.get('a[href="/admin/settings#agent"]').text()).toBe('admin.workbenchSettings.manageAgentGroup')
    expect(wrapper.text()).toContain('admin.workbenchSettings.agentModelsHint')
  })

  it('saves only workbench access values through step-up and refreshes public settings', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#workbench-entry-enabled').trigger('click')
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(updateConfig).toHaveBeenCalledWith({ image_studio_enabled: false, image_studio_group_id: 7, chat_enabled: false, office_enabled: false, search_enabled: false, files_enabled: false })
    expect(refreshPublicSettings).toHaveBeenCalledWith(true)
    expect(showSuccess).toHaveBeenCalledWith('admin.workbenchSettings.saved')
  })

  it('preserves an unavailable selected group and prevents enabling with it', async () => {
    getConfig.mockResolvedValue({ image_studio_enabled: true, image_studio_group_id: 99 })
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('option[value="99"]').text()).toContain('groupUnavailable:99')
    expect(wrapper.get('option[value="99"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="workbench-access-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
    await wrapper.get('#workbench-group').setValue(7)
    expect(wrapper.get('[data-testid="workbench-access-save"]').attributes('disabled')).toBeUndefined()
  })

  it('allows stopping access without selecting a new group', async () => {
    getConfig.mockResolvedValue({ image_studio_enabled: true, image_studio_group_id: 0 })
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#workbench-entry-enabled').trigger('click')
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith({ image_studio_enabled: false, image_studio_group_id: 0, chat_enabled: false, office_enabled: false, search_enabled: false, files_enabled: false })
  })

  it('requires the shared Agent group and allows chat without an image group', async () => {
    getConfig.mockResolvedValue({ image_studio_enabled: false, image_studio_group_id: 0, chat_enabled: true })
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('[data-testid="workbench-access-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="note"]').text()).toBe('admin.workbenchSettings.agentGroupRequired')
    expect(wrapper.get('#workbench-group').text()).not.toContain('Chat only')
    await wrapper.get('#workbench-group').setValue(7)
    expect(wrapper.get('[data-testid="workbench-access-save"]').attributes('disabled')).toBeDefined()
    getConfig.mockResolvedValue({ image_studio_enabled: false, image_studio_group_id: 0, chat_enabled: true, agent_group_id: 8 })
    await wrapper.get('button.btn-ghost').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="workbench-access-save"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('#workbench-agent-group').text()).toBe('Chat only')
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ chat_enabled: true, image_studio_group_id: 0 }))
    expect(updateConfig.mock.calls[0]![0]).not.toHaveProperty('agent_group_id')
  })

  it('enables the explicit office prerequisites together and closes office when files are disabled', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#office_enabled').trigger('click')
    expect(wrapper.get('#chat_enabled').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('#files_enabled').attributes('aria-checked')).toBe('true')
    await wrapper.get('#files_enabled').trigger('click')
    expect(wrapper.get('#office_enabled').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('#chat_enabled').attributes('aria-checked')).toBe('true')
  })

  it('disables search and office together when chat is closed', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#office_enabled').trigger('click')
    await wrapper.get('#search_enabled').trigger('click')
    await wrapper.get('#chat_enabled').trigger('click')
    expect(wrapper.get('#office_enabled').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('#search_enabled').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('#files_enabled').attributes('aria-checked')).toBe('true')
  })

  it('shows workbench provider readiness separately from the saved workbench search switch', async () => {
    getConfig.mockResolvedValue({ ...base, chat_enabled: true, search_enabled: true, search_provider_status: 'unconfigured' })
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('#search_enabled').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="status"]').text()).toBe('admin.workbenchSettings.searchProviders.unconfigured')
    expect(wrapper.get('a[href="/admin/workbench/settings#workbench-search"]').text()).toBe('admin.workbenchSettings.searchProviders.configure')
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig.mock.calls[0]![0]).not.toHaveProperty('search_provider_status')
  })

  it('leaves execution budgets out of config updates from older server responses', async () => {
    getConfig.mockResolvedValue({
      ...base,
      max_model_calls: 8,
      max_run_credits: 100,
      deployment_limits: { max_model_calls: 8 },
    })
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    const payload = updateConfig.mock.calls[0]![0]
    expect(payload).not.toHaveProperty('max_model_calls')
    expect(payload).not.toHaveProperty('max_run_credits')
    expect(payload).not.toHaveProperty('deployment_limits')
  })

  it('does not save defaults when the existing configuration cannot load', async () => {
    getConfig.mockRejectedValue(new Error('configuration unavailable'))
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toBe('configuration unavailable')
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
  })

  it('leaves model capability editing to Agent Settings and omits legacy bindings from access saves', async () => {
    getConfig.mockResolvedValue({ ...base, model_profiles: { 'company-deepseek': { profile_id: 'deepseek-v4-pro', override: false } } })
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.text()).not.toContain('自定义模型能力')
    expect(wrapper.findAll('a[href="/admin/settings#agent"]')).toHaveLength(1)
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledOnce()
    expect(updateConfig.mock.calls[0]![0]).not.toHaveProperty('model_profiles')
  })

  it('links routing edits to the existing console pages', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('a[href="/admin/groups"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/admin/accounts"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/admin/channels"]').exists()).toBe(true)
  })
})
