import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

const { state, initStore, ensureStudioApiKey } = vi.hoisted(() => ({
  state: { catalogStatus: 'idle', modelCatalog: [] as unknown[], settings: { miloRouter: { apiKey: '' } }, setImagePricing: vi.fn() },
  initStore: vi.fn(),
  ensureStudioApiKey: vi.fn().mockResolvedValue('studio-key'),
}))
vi.mock('@/features/imageStudio/stores/store', () => ({ storeState: state, initStore, retryPendingGalleryTasks: vi.fn() }))
vi.mock('@/features/imageStudio/lib/studioAuth', () => ({ ensureStudioApiKey }))
vi.mock('@/features/imageStudio/lib/galleryApi', () => ({ ensureGalleryIdentity: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ token: null, user: null }) }))
vi.mock('@/features/imageStudio/lib/studioPricing', () => ({ loadImagePricing: vi.fn(), collectBillingModels: vi.fn() }))

import ImageStudioView from '../ImageStudioView.vue'

enableAutoUnmount(afterEach)

describe('ImageStudioView layout', () => {
  it('uses the creation and result columns without the extra product connection row', async () => {
    const wrapper = mount(ImageStudioView, {
      global: { stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        StudioProductHeader: true,
        StudioCreationPanel: true,
        StudioResultPanel: true,
        StudioDetailModal: true,
        StudioLightbox: true,
        StudioMaskEditorModal: true,
        StudioSettingsModal: true,
        StudioConfirmDialog: true,
        StudioImageContextMenu: true,
        StudioToast: true,
      } },
    })
    await flushPromises()
    expect(wrapper.find('studio-product-header-stub').exists()).toBe(false)
    expect(wrapper.get('main').find('studio-creation-panel-stub').exists()).toBe(true)
    expect(wrapper.get('main').find('studio-result-panel-stub').exists()).toBe(true)
    expect(wrapper.get('.image-studio-root').classes()).toContain('lg:h-[calc(100dvh-4rem-1px)]')
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)
    expect(initStore).toHaveBeenCalledTimes(1)
  })
})
