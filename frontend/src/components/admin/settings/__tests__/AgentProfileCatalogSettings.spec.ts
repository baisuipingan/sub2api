import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentProfileCatalogSettings from '../AgentProfileCatalogSettings.vue'
import type { DesktopAgentProfileCatalogStatus } from '@/api/admin/desktopAgent'
import en from '@/i18n/locales/en/admin/agentSettings'
import zh from '@/i18n/locales/zh/admin/agentSettings'

const api = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), refresh: vi.fn() }))
vi.mock('@/api/admin/desktopAgent', () => ({ getDesktopAgentProfileCatalog: api.get, updateDesktopAgentProfileCatalogSource: api.save, refreshDesktopAgentProfileCatalog: api.refresh }))
const StepUpStub = defineComponent({
  props: ['controller'],
  setup: props => () => props.controller.visible.value ? h('div', { 'data-test': 'catalog-step-up' }, [
    h('button', { type: 'button', 'data-test': 'catalog-verify', onClick: props.controller.onVerified }, 'Verify'),
    h('button', { type: 'button', 'data-test': 'catalog-cancel', onClick: props.controller.onCancel }, 'Cancel'),
  ]) : null,
})
const initial: DesktopAgentProfileCatalogStatus = { source_url: 'https://profiles.example.test/catalog.json', revision: '2026-11-14.2', profile_count: 37, last_success_at: '', last_checked_at: '', last_error: '', using_fallback: true }
const wrappers: ReturnType<typeof mount>[] = []
function setup(active = true, locale = 'en') {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en: { admin: en, common: { loading: 'Loading' } }, zh: { admin: zh, common: { loading: '加载中' } } } })
  const wrapper = mount(AgentProfileCatalogSettings, { props: { active }, global: { plugins: [i18n], stubs: { TotpStepUpDialog: StepUpStub } } })
  wrappers.push(wrapper)
  return wrapper
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (value: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockResolvedValue({ ...initial })
  api.save.mockImplementation(async source_url => ({ ...initial, source_url }))
  api.refresh.mockResolvedValue({ ...initial, revision: '2026-11-15.1', using_fallback: false, last_success_at: '2026-11-15T01:00:00Z' })
})
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('remote capability profile settings', () => {
  it('loads on activation and retains an unsaved source across tab switches', async () => {
    const wrapper = setup(false); await flushPromises(); expect(api.get).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true }); await flushPromises()
    expect(wrapper.get('[data-test="profile-catalog-revision"]').text()).toBe(initial.revision)
    await wrapper.get('#agent-profile-source').setValue('https://draft.example.test/catalog.json')
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(1)
    expect((wrapper.get('#agent-profile-source').element as HTMLInputElement).value).toBe('https://draft.example.test/catalog.json')
    expect(api.save).not.toHaveBeenCalled(); expect(api.refresh).not.toHaveBeenCalled()
  })
  it('ignores a stale load response after leaving and returning', async () => {
    const old = deferred<DesktopAgentProfileCatalogStatus>(); api.get.mockReturnValueOnce(old.promise)
    const wrapper = setup(); await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    await wrapper.get('#agent-profile-source').setValue('https://draft.example.test/current.json')
    old.resolve({ ...initial, source_url: 'https://old.example.test/ignored.json' }); await flushPromises()
    expect((wrapper.get('#agent-profile-source').element as HTMLInputElement).value).toBe('https://draft.example.test/current.json')
  })
  it.each(['http://example.test/catalog.json', 'https://user:secret@example.test/catalog.json', 'https://example.test/catalog.json?key=secret', 'https://example.test/catalog.json?', 'https://example.test/catalog.json#', 'https://example.test/a\\b'])('rejects unsafe source syntax %s before a write', async source => {
    const wrapper = setup(); await flushPromises(); await wrapper.get('#agent-profile-source').setValue(source)
    await wrapper.get('[data-test="profile-catalog-save"]').trigger('click'); await flushPromises()
    expect(api.save).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(en.agentSettings.profileCatalog.sourceInvalid)
  })
  it('syncs the saved source without overwriting an unsaved source draft', async () => {
    const wrapper = setup(); await flushPromises(); await wrapper.get('#agent-profile-source').setValue('https://draft.example.test/catalog.json')
    await wrapper.get('[data-test="profile-catalog-refresh"]').trigger('click'); await flushPromises()
    expect(api.refresh).toHaveBeenCalledWith()
    expect(api.save).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="profile-catalog-revision"]').text()).toBe('2026-11-15.1')
    expect((wrapper.get('#agent-profile-source').element as HTMLInputElement).value).toBe('https://draft.example.test/catalog.json')
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })
  it('retains the last catalog and draft when sync fails', async () => {
    api.refresh.mockRejectedValueOnce(new Error('upstream body must not be shown'))
    const wrapper = setup(); await flushPromises(); await wrapper.get('#agent-profile-source').setValue('https://draft.example.test/catalog.json')
    await wrapper.get('[data-test="profile-catalog-refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="profile-catalog-revision"]').text()).toBe(initial.revision)
    expect(wrapper.text()).toContain(en.agentSettings.profileCatalog.refreshFailed)
    expect(wrapper.text()).not.toContain('upstream body must not be shown')
    expect(wrapper.emitted('updated')).toBeUndefined()
  })
  it('freezes source writes through step-up and retries the same payload', async () => {
    api.save.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' })
    const wrapper = setup(); await flushPromises(); await wrapper.get('#agent-profile-source').setValue('https://draft.example.test/catalog.json')
    await wrapper.get('[data-test="profile-catalog-save"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-test="catalog-step-up"]').exists()).toBe(true)
    await wrapper.get('[data-test="catalog-verify"]').trigger('click'); await flushPromises()
    expect(api.save.mock.calls).toEqual([['https://draft.example.test/catalog.json'], ['https://draft.example.test/catalog.json']])
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })
  it('does not reopen step-up or retry a delayed rejection after navigation', async () => {
    const pending = deferred<DesktopAgentProfileCatalogStatus>(); api.save.mockReturnValueOnce(pending.promise)
    const wrapper = setup(); await flushPromises(); await wrapper.get('[data-test="profile-catalog-save"]').trigger('click'); await flushPromises()
    await wrapper.setProps({ active: false }); pending.reject({ reason: 'STEP_UP_REQUIRED' }); await flushPromises()
    expect(wrapper.find('[data-test="catalog-step-up"]').exists()).toBe(false)
    expect(api.save).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ active: true }); await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(2)
  })
  it.each(['en', 'zh'])('can pause remote sync without deleting the last catalog: %s', async locale => {
    const wrapper = setup(true, locale); await flushPromises(); await wrapper.get('#agent-profile-source').setValue('')
    await wrapper.get('[data-test="profile-catalog-save"]').trigger('click'); await flushPromises()
    expect(api.save).toHaveBeenCalledWith('')
    expect(wrapper.get('[data-test="profile-catalog-revision"]').text()).toBe(initial.revision)
    expect(wrapper.get('[data-test="profile-catalog-refresh"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toMatch(/admin\.agentSettings\.|common\.loading/)
  })
})
