import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import DesktopReleaseStorageSettings from '../DesktopReleaseStorageSettings.vue'
import en from '@/i18n/locales/en/admin/desktopReleaseStorage'
import zh from '@/i18n/locales/zh/admin/desktopReleaseStorage'
import type { DesktopReleaseStorageConfig } from '@/api/admin/desktopReleaseStorage'

const mocks = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), test: vi.fn() }))
vi.mock('@/api/admin/desktopReleaseStorage', () => ({ getDesktopReleaseStorage: mocks.get, updateDesktopReleaseStorage: mocks.save, testDesktopReleaseStorage: mocks.test }))
const StepUpStub = defineComponent({
  props: ['controller'],
  setup: props => () => props.controller.visible.value ? h('div', { 'data-test': 'storage-step-up' }, [
    h('button', { type: 'button', 'data-test': 'storage-step-up-verify', onClick: props.controller.onVerified }, 'Verify'),
    h('button', { type: 'button', 'data-test': 'storage-step-up-cancel', onClick: props.controller.onCancel }, 'Cancel'),
  ]) : null,
})
const initial: DesktopReleaseStorageConfig = { bucket: '', public_base_url: '', release_base_url: '', backup_s3_configured: true, configured: false }
const configured: DesktopReleaseStorageConfig = { ...initial, bucket: 'release-bucket', public_base_url: 'https://download.example.test', release_base_url: 'https://download.example.test/milocode/', configured: true }
const wrappers: ReturnType<typeof mount>[] = []
function setup(active = true, locale = 'en') {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: {
    en: { admin: en, common: { refresh: 'Refresh', saving: 'Saving', loading: 'Loading' }, stepUp: { adminApiKeyForbidden: 'Full administrator required', notEnabled: 'Enable TOTP first' } },
    zh: { admin: zh, common: { refresh: '刷新', saving: '保存中', loading: '加载中' }, stepUp: { adminApiKeyForbidden: '需要完整管理员', notEnabled: '请先启用二次验证' } },
  } })
  const wrapper = mount(DesktopReleaseStorageSettings, { props: { active }, global: { plugins: [i18n], stubs: { TotpStepUpDialog: StepUpStub } } })
  wrappers.push(wrapper)
  return wrapper
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
async function fill(wrapper: ReturnType<typeof mount>, bucket = configured.bucket, domain = configured.public_base_url) {
  await wrapper.get('#desktop-release-bucket').setValue(bucket)
  await wrapper.get('#desktop-release-domain').setValue(domain)
}
const save = (wrapper: ReturnType<typeof mount>) => wrapper.get('[data-test="release-storage-save"]').trigger('click')
const testDraft = (wrapper: ReturnType<typeof mount>) => wrapper.get('[data-test="release-storage-test"]').trigger('click')

beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ ...initial })
  mocks.save.mockImplementation(async value => ({ ...initial, ...value, configured: !!value.bucket, release_base_url: value.public_base_url ? `${value.public_base_url}/milocode/` : '' }))
  mocks.test.mockResolvedValue({ success: true })
})
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('independent MiloCode release storage settings', () => {
  it('loads only on activation, keeps placeholders unsaved and renders no secret field', async () => {
    const wrapper = setup(false)
    await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    await wrapper.setProps({ active: true }); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect((wrapper.get('#desktop-release-bucket').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('#desktop-release-bucket').attributes('placeholder')).toBe('milocode-releases')
    expect(wrapper.findAll('input')).toHaveLength(2)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(mocks.save).not.toHaveBeenCalled()
    await fill(wrapper)
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect((wrapper.get('#desktop-release-bucket').element as HTMLInputElement).value).toBe(configured.bucket)
  })
  it('discards an expired load response across navigation', async () => {
    const old = deferred<DesktopReleaseStorageConfig>()
    mocks.get.mockReturnValueOnce(old.promise).mockResolvedValueOnce(configured)
    const wrapper = setup()
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    await fill(wrapper, 'new-draft')
    old.resolve(initial); await flushPromises()
    expect((wrapper.get('#desktop-release-bucket').element as HTMLInputElement).value).toBe('new-draft')
    expect(mocks.get).toHaveBeenCalledTimes(2)
  })
  it.each(['http://download.example.test', 'https://user:password@download.example.test', 'https://download.example.test/files', 'https://download.example.test?token=secret', 'https://download.example.test#fragment', 'https://download.example.test/a/..', 'https://download.example.test?', 'https://download.example.test#'])('rejects invalid public domain %s before save or test', async domain => {
    const wrapper = setup(); await flushPromises(); await fill(wrapper, 'release-bucket', domain)
    await save(wrapper); await testDraft(wrapper); await flushPromises()
    expect(mocks.save).not.toHaveBeenCalled(); expect(mocks.test).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('HTTPS origin')
  })
  it('tests unsaved entries without persisting, and normalizes a bare HTTPS origin', async () => {
    const wrapper = setup(); await flushPromises()
    await fill(wrapper, 'draft-release-bucket', 'https://DOWNLOAD.example.test/')
    await testDraft(wrapper); await flushPromises()
    expect(mocks.test).toHaveBeenCalledWith({ bucket: 'draft-release-bucket', public_base_url: 'https://download.example.test' })
    expect(mocks.save).not.toHaveBeenCalled()
    expect(wrapper.get('[role="status"]').text()).toContain('tests passed')
    expect(wrapper.get('[data-test="release-storage-base"]').text()).toContain('/milocode/')
  })
  it('clears a successful draft test notice when the entries change', async () => {
    const wrapper = setup(); await flushPromises(); await fill(wrapper)
    await testDraft(wrapper); await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    await fill(wrapper, 'another-release-bucket')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('preserves both entries after a save failure without exposing backend secrets', async () => {
    mocks.save.mockRejectedValue({ message: 'S3 Authorization secret-key=must-not-render' })
    const wrapper = setup(); await flushPromises(); await fill(wrapper)
    await save(wrapper); await flushPromises()
    expect((wrapper.get('#desktop-release-bucket').element as HTMLInputElement).value).toBe(configured.bucket)
    expect((wrapper.get('#desktop-release-domain').element as HTMLInputElement).value).toBe(configured.public_base_url)
    expect(wrapper.get('[role="alert"]').text()).toContain('preserved')
    expect(wrapper.text()).not.toContain('must-not-render')
    expect(wrapper.get('[data-test="release-storage-save"]').attributes('disabled')).toBeUndefined()
  })
  it('freezes a separate payload through step-up and cancels silently on navigation', async () => {
    mocks.save.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' })
    const wrapper = setup(); await flushPromises(); await fill(wrapper)
    await save(wrapper); await flushPromises()
    expect(wrapper.find('[data-test="storage-step-up"]').exists()).toBe(true)
    expect(mocks.save).toHaveBeenLastCalledWith({ bucket: configured.bucket, public_base_url: configured.public_base_url })
    await wrapper.setProps({ active: false }); await flushPromises()
    expect(wrapper.find('[data-test="storage-step-up"]').exists()).toBe(false)
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it('retries exactly the saved payload after step-up verification', async () => {
    mocks.save.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' })
    const wrapper = setup(); await flushPromises(); await fill(wrapper)
    await save(wrapper); await flushPromises()
    await wrapper.get('[data-test="storage-step-up-verify"]').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(mocks.save).toHaveBeenNthCalledWith(2, { bucket: configured.bucket, public_base_url: configured.public_base_url })
    expect(wrapper.get('[role="status"]').text()).toContain('saved')
  })
  it('uses step-up for a write/delete connection test with the frozen draft', async () => {
    mocks.test.mockRejectedValueOnce({ reason: 'STEP_UP_REQUIRED' })
    const wrapper = setup(); await flushPromises(); await fill(wrapper, 'test-bucket')
    await testDraft(wrapper); await flushPromises()
    await wrapper.get('[data-test="storage-step-up-verify"]').trigger('click'); await flushPromises()
    expect(mocks.test).toHaveBeenCalledTimes(2)
    expect(mocks.test).toHaveBeenLastCalledWith({ bucket: 'test-bucket', public_base_url: configured.public_base_url })
    expect(mocks.save).not.toHaveBeenCalled()
  })
  it('waits for a pending write before reloading after reactivation', async () => {
    const pending = deferred<DesktopReleaseStorageConfig>()
    mocks.save.mockReturnValueOnce(pending.promise)
    const wrapper = setup(); await flushPromises(); await fill(wrapper)
    await save(wrapper)
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true }); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    mocks.get.mockResolvedValue(configured)
    pending.resolve(configured); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect((wrapper.get('#desktop-release-bucket').element as HTMLInputElement).value).toBe(configured.bucket)
  })
  it('discards late test results and leaves the newer draft intact', async () => {
    const pending = deferred<{ success: boolean }>()
    mocks.test.mockReturnValueOnce(pending.promise)
    const wrapper = setup(); await flushPromises(); await fill(wrapper)
    await testDraft(wrapper)
    await wrapper.setProps({ active: false }); await wrapper.setProps({ active: true })
    await fill(wrapper, 'new-release-draft')
    pending.resolve({ success: true }); await flushPromises()
    expect((wrapper.get('#desktop-release-bucket').element as HTMLInputElement).value).toBe('new-release-draft')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('can clear both fields to disable but never tests an empty configuration', async () => {
    mocks.get.mockResolvedValue(configured)
    const wrapper = setup(); await flushPromises(); await fill(wrapper, '', '')
    await testDraft(wrapper); await flushPromises(); expect(mocks.test).not.toHaveBeenCalled()
    await save(wrapper); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ bucket: '', public_base_url: '' })
  })
  it('shows a missing-backup warning and localized Chinese text', async () => {
    mocks.get.mockResolvedValue({ ...initial, backup_s3_configured: false })
    const wrapper = setup(true, 'zh'); await flushPromises()
    expect(wrapper.get('[data-test="release-storage-backup-missing"]').text()).toContain('备份管理')
    expect(wrapper.get('[data-test="release-storage-test"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('MiloCode 发布存储')
  })
  it('does not enable editing after a load failure', async () => {
    mocks.get.mockRejectedValue(new Error('unavailable'))
    const wrapper = setup(); await flushPromises()
    expect(wrapper.find('#desktop-release-bucket').exists()).toBe(false)
    expect(wrapper.find('[data-test="release-storage-save"]').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('Unable to load')
  })
})
