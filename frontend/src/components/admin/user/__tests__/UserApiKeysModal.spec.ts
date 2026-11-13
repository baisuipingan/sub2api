import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserApiKeysModal from '../UserApiKeysModal.vue'
import type { AdminUser, ApiKey } from '@/types'
import type { StepUpController } from '@/composables/useStepUp'

const { getKeys, manageCredential, updateGroup, showError, showSuccess } = vi.hoisted(() => ({ getKeys: vi.fn(), manageCredential: vi.fn(), updateGroup: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  users: { getUserApiKeys: getKeys }, groups: { getAll: vi.fn().mockResolvedValue([]) },
  apiKeys: { updateApiKeyGroup: updateGroup },
} }))
vi.mock('@/api/admin/desktopCredentials', () => ({ manageUserDesktopCredential: manageCredential }))
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { name: 'TotpStepUpDialog', props: ['controller'], template: '<div />' } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { getKeys.mockReset(); manageCredential.mockReset(); updateGroup.mockReset(); showError.mockReset(); showSuccess.mockReset(); vi.spyOn(console, 'error').mockImplementation(() => {}) })
afterEach(() => vi.restoreAllMocks())
function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (error: Error) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const user = (id: number) => ({ id, email: `user${id}@example.com`, username: `user${id}` }) as AdminUser
const keys = (id: number, name: string) => ({ items: [{ id, name, key: 'sk-example-key-value-for-tests', status: 'active', created_at: '2026-09-20', group_id: null }] })
const agentKey = (overrides: Partial<ApiKey> = {}) => ({ ...keys(73, 'Managed Agent').items[0], quota: 0, quota_used: 0, expires_at: null, is_system: true, purpose: 'desktop_agent', ...overrides }) as ApiKey
async function open() {
  const wrapper = mount(UserApiKeysModal, {
    props: { show: false, user: user(1) },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      GroupBadge: true, GroupOptionItem: true,
    } },
  })
  await wrapper.setProps({ show: true })
  return wrapper
}
async function switchUser(wrapper: Awaited<ReturnType<typeof open>>) {
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true, user: user(2) })
}

describe('user API key loading', () => {
  it('shows an existing translated retry action after a load failure and reloads the same user', async () => {
    getKeys.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(keys(1, 'retried-key'))
    const wrapper = await open(); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('desktop.keyListFailed')
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(getKeys.mock.calls).toEqual([[1], [1]])
    expect(wrapper.text()).toContain('retried-key')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it('loads the next page without dropping current keys and reports all loaded', async () => {
    getKeys.mockResolvedValueOnce({ ...keys(1, 'first-page-key'), total: 2 })
      .mockResolvedValueOnce({ items: [agentKey({ status: 'disabled' })], total: 2 })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-keys-load-more"]').trigger('click'); await flushPromises()
    expect(getKeys).toHaveBeenLastCalledWith(1, { page: 2, page_size: 20 })
    expect(wrapper.text()).toContain('first-page-key')
    expect(wrapper.find('[data-test="admin-agent-restore"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="admin-keys-load-more"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="admin-keys-all-loaded"]').exists()).toBe(true)
  })

  it('ignores a late next page after switching users', async () => {
    const late = deferred()
    getKeys.mockResolvedValueOnce({ ...keys(1, 'first-user-key'), total: 2 }).mockReturnValueOnce(late.promise)
      .mockResolvedValueOnce(keys(2, 'second-user-key'))
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-keys-load-more"]').trigger('click')
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    late.resolve({ items: [agentKey()], total: 2 }); await flushPromises()
    expect(wrapper.text()).toContain('second-user-key')
    expect(wrapper.text()).not.toContain('first-user-key')
    expect(wrapper.text()).not.toContain('Managed Agent')
  })

  it('keeps existing keys and permits retry when loading more fails', async () => {
    getKeys.mockResolvedValueOnce({ ...keys(1, 'first-page-key'), total: 2 }).mockRejectedValueOnce(new Error('private detail'))
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-keys-load-more"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('first-page-key')
    expect(wrapper.get('[data-test="admin-keys-load-more"]').attributes('disabled')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('desktop.keyListFailed')
  })

  it('does not display the previous user keys when the next load fails', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = await open(); await flushPromises()
    expect(wrapper.text()).toContain('first-user-key')
    await switchUser(wrapper); await flushPromises()
    expect(wrapper.text()).toContain('user2@example.com')
    expect(wrapper.text()).not.toContain('first-user-key')
  })

  it('does not replace current keys with a late previous response', async () => {
    const old = deferred()
    getKeys.mockReturnValueOnce(old.promise).mockResolvedValueOnce(keys(2, 'current-user-key'))
    const wrapper = await open()
    await switchUser(wrapper); await flushPromises()
    old.resolve(keys(1, 'old-user-key')); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.text()).not.toContain('old-user-key')
  })

  it('keeps the current request loading when an obsolete request fails', async () => {
    const old = deferred(); const current = deferred()
    getKeys.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await open()
    await switchUser(wrapper)
    old.reject(new Error('obsolete')); await flushPromises()
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
    current.resolve(keys(2, 'current-user-key')); await flushPromises()
    expect(wrapper.text()).toContain('current-user-key')
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
  })

  it('loads keys when the selected user changes while the dialog is open', async () => {
    getKeys.mockResolvedValueOnce(keys(1, 'first-user-key')).mockResolvedValueOnce(keys(2, 'second-user-key'))
    const wrapper = await open(); await flushPromises()
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    expect(getKeys).toHaveBeenLastCalledWith(2)
    expect(wrapper.text()).toContain('second-user-key')
    expect(wrapper.text()).not.toContain('first-user-key')
  })
})

describe('administrator Agent credential controls', () => {
  it('reloads all previously loaded pages after restoring an older key', async () => {
    getKeys.mockResolvedValueOnce({ ...keys(1, 'ordinary-new-key'), total: 2 })
      .mockResolvedValueOnce({ items: [agentKey({ status: 'disabled' })], total: 2 })
      .mockResolvedValueOnce({ ...keys(1, 'ordinary-new-key'), total: 2 })
      .mockResolvedValueOnce({ items: [agentKey({ status: 'active' })], total: 2 })
    manageCredential.mockResolvedValueOnce({ status: 'active', key_id: 73 })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-keys-load-more"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-test="admin-agent-restore"]').trigger('click')
    wrapper.findComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm'); await flushPromises()
    expect(getKeys).toHaveBeenNthCalledWith(3, 1)
    expect(getKeys).toHaveBeenNthCalledWith(4, 1, { page: 2, page_size: 20 })
    expect(wrapper.text()).toContain('ordinary-new-key')
    expect(wrapper.find('[data-test="admin-agent-revoke"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="admin-keys-all-loaded"]').exists()).toBe(true)
  })

  it('identifies managed keys by purpose and keeps their group read-only', async () => {
    getKeys.mockResolvedValue({ items: [agentKey(), agentKey({ id: 74, purpose: 'image_studio' }), agentKey({ id: 75, is_system: false })] })
    const wrapper = await open(); await flushPromises()
    expect(wrapper.findAll('[data-test="admin-agent-revoke"]')).toHaveLength(1)
    const groupButtons = wrapper.findAll('[data-test="admin-key-group"]')
    expect(groupButtons[0].attributes('disabled')).toBeDefined()
    expect(groupButtons[1].attributes('disabled')).toBeDefined()
    expect(groupButtons[2].attributes('disabled')).toBeUndefined()
    await groupButtons[0].trigger('click')
    expect(updateGroup).not.toHaveBeenCalled()
    expect(manageCredential).not.toHaveBeenCalled()
  })

  it.each([
    { status: 'inactive' }, { status: 'expired' }, { status: 'quota_exhausted' },
    { status: 'disabled', expires_at: '2020-01-01T00:00:00Z' },
    { status: 'disabled', quota: 10, quota_used: 10 },
    { status: 'disabled', expires_at: 'invalid' },
  ] as Partial<ApiKey>[])('never offers restoration for $status with unusable limits $expires_at $quota_used', async override => {
    getKeys.mockResolvedValue({ items: [agentKey(override)] })
    const wrapper = await open(); await flushPromises()
    expect(wrapper.find('[data-test="admin-agent-restore"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="admin-agent-unavailable"]').exists()).toBe(true)
    expect(manageCredential).not.toHaveBeenCalled()
  })

  it.each(['revoke', 'restore'] as const)('confirms %s for the exact selected user/key and reloads the result', async action => {
    getKeys.mockResolvedValueOnce({ items: [agentKey({ status: action === 'restore' ? 'disabled' : 'active' })] })
      .mockResolvedValueOnce({ items: [agentKey({ status: action === 'restore' ? 'active' : 'disabled' })] })
    manageCredential.mockResolvedValueOnce({ status: action === 'restore' ? 'active' : 'revoked', key_id: 73 })
    const wrapper = await open(); await flushPromises()
    await wrapper.get(`[data-test="admin-agent-${action}"]`).trigger('click')
    expect(manageCredential).not.toHaveBeenCalled()
    const confirmation = wrapper.findComponent({ name: 'ConfirmDialog' })
    expect(confirmation.props('show')).toBe(true)
    expect(confirmation.props('message')).toBe(action === 'restore' ? 'desktop.adminRestoreConfirm' : 'desktop.adminRevokeConfirm')
    confirmation.vm.$emit('confirm'); await flushPromises()
    expect(manageCredential).toHaveBeenCalledWith(action, 1, 73)
    expect(getKeys).toHaveBeenNthCalledWith(2, 1)
    expect(updateGroup).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith(action === 'restore' ? 'desktop.adminRestored' : 'desktop.adminRevoked')
  })

  it('retries the frozen target only after successful existing step-up verification', async () => {
    getKeys.mockResolvedValue({ items: [agentKey({ status: 'disabled' })] })
    manageCredential.mockRejectedValueOnce({ status: 403, code: 'STEP_UP_REQUIRED' }).mockResolvedValueOnce({ status: 'active', key_id: 73 })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-agent-restore"]').trigger('click')
    wrapper.findComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm'); await flushPromises()
    const controller = wrapper.findComponent({ name: 'TotpStepUpDialog' }).props('controller') as StepUpController
    expect(controller.visible.value).toBe(true)
    expect(manageCredential).toHaveBeenCalledTimes(1)
    controller.onVerified(); await flushPromises()
    expect(manageCredential).toHaveBeenCalledTimes(2)
    expect(manageCredential.mock.calls).toEqual([['restore', 1, 73], ['restore', 1, 73]])
  })

  it('cancels pending verification when switching users and never retries the old target', async () => {
    getKeys.mockResolvedValue({ items: [agentKey()] })
    manageCredential.mockRejectedValueOnce({ status: 403, code: 'STEP_UP_REQUIRED' })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-agent-revoke"]').trigger('click')
    wrapper.findComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm'); await flushPromises()
    const controller = wrapper.findComponent({ name: 'TotpStepUpDialog' }).props('controller') as StepUpController
    await wrapper.setProps({ user: user(2) }); await flushPromises()
    controller.onVerified(); await flushPromises()
    expect(manageCredential).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent({ name: 'ConfirmDialog' }).props('show')).toBe(false)
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it.each(['close', 'switch'] as const)('does not open verification for a late STEP_UP_REQUIRED after %s', async change => {
    const late = deferred()
    getKeys.mockResolvedValue({ items: [agentKey()] })
    manageCredential.mockReturnValueOnce(late.promise)
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-agent-revoke"]').trigger('click')
    wrapper.findComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm'); await flushPromises()
    const controller = wrapper.findComponent({ name: 'TotpStepUpDialog' }).props('controller') as StepUpController
    expect(controller.visible.value).toBe(false)
    if (change === 'close') await wrapper.setProps({ show: false })
    else await wrapper.setProps({ user: user(2) })
    late.reject({ status: 403, code: 'STEP_UP_REQUIRED' } as never); await flushPromises()
    expect(controller.visible.value).toBe(false)
    controller.onVerified(); await flushPromises()
    expect(manageCredential).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent({ name: 'ConfirmDialog' }).props('show')).toBe(false)
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('keeps cancellation silent and shows a safe error for forbidden actions', async () => {
    getKeys.mockResolvedValue({ items: [agentKey()] })
    manageCredential.mockRejectedValueOnce({ status: 403, code: 'STEP_UP_REQUIRED' })
    const wrapper = await open(); await flushPromises()
    await wrapper.get('[data-test="admin-agent-revoke"]').trigger('click')
    const confirmation = wrapper.findComponent({ name: 'ConfirmDialog' })
    confirmation.vm.$emit('confirm'); await flushPromises()
    const controller = wrapper.findComponent({ name: 'TotpStepUpDialog' }).props('controller') as StepUpController
    controller.onCancel(); await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(manageCredential).toHaveBeenCalledTimes(1)
    manageCredential.mockRejectedValueOnce({ status: 403, code: 'FORBIDDEN', message: 'internal private detail' })
    confirmation.vm.$emit('confirm'); await flushPromises()
    expect(showError).toHaveBeenCalledWith('desktop.credentialFailed')
    expect(confirmation.props('show')).toBe(true)
  })
})
