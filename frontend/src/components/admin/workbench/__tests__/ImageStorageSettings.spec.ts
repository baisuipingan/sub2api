import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ImageStorageSettings from '../ImageStorageSettings.vue'

const { getConfig, updateConfig, testConnection, showError, showSuccess, stepUpRun } = vi.hoisted(() => ({
  getConfig: vi.fn(), updateConfig: vi.fn(), testConnection: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
  stepUpRun: vi.fn((action: () => unknown) => action()),
}))
vi.mock('@/api', () => ({ adminAPI: { backup: { getImageStorageConfig: getConfig, updateImageStorageConfig: updateConfig, testImageStorageConnection: testConnection } } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: stepUpRun }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))

const baseConfig = {
  enabled: true, studio_gallery_enabled: true, studio_jobs_enabled: false, reuse_backup_s3: true,
  bucket: 'private-images', prefix: 'image-results/', public_base_url: '', presign_expiry_hours: 12,
  max_download_bytes: 16777216, endpoint: 'https://s3.test', region: 'auto', access_key_id: 'key-id', force_path_style: false,
}
const wrappers: ReturnType<typeof mount>[] = []
const mountSettings = () => {
  const wrapper = mount(ImageStorageSettings, { global: { stubs: { TotpStepUpDialog: true } } })
  wrappers.push(wrapper)
  return wrapper
}
const checked = (wrapper: ReturnType<typeof mount>, id: string) => wrapper.get(id).attributes('aria-checked') === 'true'

describe('shared ImageStorageSettings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({ config: { ...baseConfig }, secret_configured: true })
    updateConfig.mockResolvedValue({ ...baseConfig })
    testConnection.mockResolvedValue({ ok: true })
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('legacy settings keep gallery and server execution disabled without changing async', async () => {
    getConfig.mockResolvedValue({ config: { ...baseConfig, studio_gallery_enabled: undefined, studio_jobs_enabled: undefined }, secret_configured: false })
    const wrapper = mountSettings()
    await flushPromises()
    expect(checked(wrapper, '#async-image-enabled')).toBe(true)
    expect(checked(wrapper, '#studio-gallery-enabled')).toBe(false)
    expect(checked(wrapper, '#studio-jobs-enabled')).toBe(false)
    expect(wrapper.get('#studio-jobs-enabled').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#studio-gallery-private-warning').text()).toContain('studioGalleryPrivateBucketWarning')
  })

  it('server execution is independent from native async and saves through step-up', async () => {
    getConfig.mockResolvedValue({ config: { ...baseConfig, enabled: false }, secret_configured: true })
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#studio-jobs-enabled').trigger('click')
    await wrapper.get('[data-testid="image-storage-save"]').trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, studio_gallery_enabled: true, studio_jobs_enabled: true }))
  })

  it('disabling gallery stops new server jobs and preserves the native async switch', async () => {
    getConfig.mockResolvedValue({ config: { ...baseConfig, studio_jobs_enabled: true }, secret_configured: true })
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#studio-gallery-enabled').trigger('click')
    expect(checked(wrapper, '#studio-jobs-enabled')).toBe(false)
    expect(wrapper.get('#studio-jobs-enabled').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="image-storage-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, studio_gallery_enabled: false, studio_jobs_enabled: false }))
  })

  it('disabling native async preserves gallery and custom storage values', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('#async-image-enabled').trigger('click')
    await wrapper.get('[data-testid="image-storage-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ ...baseConfig, enabled: false, secret_access_key: '' }))
  })

  it('leaves an existing secret blank and preserves it during save', async () => {
    getConfig.mockResolvedValue({ config: { ...baseConfig, reuse_backup_s3: false }, secret_configured: true })
    const wrapper = mountSettings()
    await flushPromises()
    expect((wrapper.get('#image-storage-secret').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('#image-storage-secret').attributes('placeholder')).toContain('secretConfigured')
    await wrapper.get('[data-testid="image-storage-save"]').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ secret_access_key: '' }))
  })

  it('does not overwrite stored settings after a load error', async () => {
    getConfig.mockRejectedValue(new Error('storage settings unavailable'))
    const wrapper = mountSettings()
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toBe('storage settings unavailable')
    await wrapper.get('[data-testid="image-storage-save"]').trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
  })

  it('tests the existing configuration without saving or generating', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await wrapper.get('[data-testid="image-storage-test"]').trigger('click')
    await flushPromises()
    expect(testConnection).toHaveBeenCalledWith(expect.objectContaining({ bucket: 'private-images', secret_access_key: '' }))
    expect(updateConfig).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalled()
  })

  it('ignores a settings response after navigation away', async () => {
    let resolve!: (value: unknown) => void
    getConfig.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountSettings()
    wrapper.unmount()
    resolve({ config: baseConfig, secret_configured: true })
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })
})
