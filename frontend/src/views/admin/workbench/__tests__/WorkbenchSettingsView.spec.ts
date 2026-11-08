import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkbenchSettingsView from '../WorkbenchSettingsView.vue'

const { getConfig, updateConfig, getGroups, showError, showSuccess, refreshPublicSettings, stepUpRun } = vi.hoisted(() => ({
  getConfig: vi.fn(), updateConfig: vi.fn(), getGroups: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), refreshPublicSettings: vi.fn(),
  stepUpRun: vi.fn((action: () => unknown) => action()),
}))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: getGroups } } }))
vi.mock('@/api/admin/workbench', () => ({ getWorkbenchConfig: getConfig, updateWorkbenchConfig: updateConfig }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError, showSuccess, fetchPublicSettings: refreshPublicSettings }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params?.id ? `${key}:${params.id}` : key }) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: stepUpRun }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))

const base = { image_studio_enabled: true, image_studio_group_id: 7 }
const wrappers: ReturnType<typeof mount>[] = []
function mountSettings() {
  const wrapper = mount(WorkbenchSettingsView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TotpStepUpDialog: true,
    ImageStorageSettings: true,
    RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
    Select: {
      props: ['modelValue', 'options'], emits: ['update:modelValue'],
      template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', Number($event.target.value))"><option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">{{ option.label }}</option></select>',
    },
  } } })
  wrappers.push(wrapper)
  return wrapper
}

describe('WorkbenchSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({ ...base })
    updateConfig.mockImplementation(async payload => payload)
    getGroups.mockResolvedValue([
      { id: 7, name: 'Image group', status: 'active', allow_image_generation: true },
      { id: 8, name: 'Chat only', status: 'active', allow_image_generation: false },
      { id: 9, name: 'Inactive group', status: 'inactive', allow_image_generation: true },
    ])
    refreshPublicSettings.mockResolvedValue(null)
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('shows the existing image storage component and only eligible image groups', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ImageStorageSettings' }).exists()).toBe(true)
    expect(wrapper.get('#workbench-group').text()).toContain('Image group')
    expect(wrapper.get('#workbench-group').text()).not.toContain('Chat only')
    expect(wrapper.get('#workbench-group').text()).not.toContain('Inactive group')
    expect(wrapper.get('#workbench-entry-enabled').attributes('aria-checked')).toBe('true')
  })

  it('saves only workbench access values through step-up and refreshes public settings', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#workbench-entry-enabled').trigger('click')
    await wrapper.get('[data-testid="workbench-access-save"]').trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(updateConfig).toHaveBeenCalledWith({ image_studio_enabled: false, image_studio_group_id: 7 })
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
    expect(updateConfig).toHaveBeenCalledWith({ image_studio_enabled: false, image_studio_group_id: 0 })
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

  it('links routing edits to the existing console pages', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('a[href="/admin/groups"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/admin/accounts"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/admin/channels"]').exists()).toBe(true)
  })
})
