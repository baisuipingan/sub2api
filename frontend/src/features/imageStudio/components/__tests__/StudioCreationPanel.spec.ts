import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { initialState, refreshImageModelCatalog } = vi.hoisted(() => ({
  refreshImageModelCatalog: vi.fn(async () => true),
  initialState: {
    catalogStatus: 'ready',
    catalogError: undefined as string | undefined,
    modelCatalog: [{ id: 'gpt-image-2', name: 'Preset name' }],
    settings: {
      selectedModelId: 'gpt-image-2',
      miloRouter: { apiKey: 'sk-studio', connectionStatus: 'connected' },
    },
    selectModel: vi.fn(),
  },
}))

vi.mock('../../stores/store', async () => ({
  storeState: (await import('vue')).reactive({ ...initialState, refreshImageModelCatalog }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import { storeState as state } from '../../stores/store'
import StudioCreationPanel from '../StudioCreationPanel.vue'

enableAutoUnmount(afterEach)

function mountPanel() {
  return mount(StudioCreationPanel, { global: { stubs: { StudioInputBar: true } } })
}

beforeEach(() => {
  refreshImageModelCatalog.mockClear()
  state.catalogStatus = 'ready'
  state.catalogError = undefined
  state.settings.miloRouter.apiKey = 'sk-studio'
})

describe('StudioCreationPanel model refresh', () => {
  it('calls the existing catalog refresh action from the icon beside the selector', async () => {
    const wrapper = mountPanel()
    await wrapper.get('[data-testid="refresh-models"]').trigger('click')
    expect(refreshImageModelCatalog).toHaveBeenCalledTimes(1)
  })

  it('reflects loading and disables refresh until the request finishes', async () => {
    const wrapper = mountPanel()
    state.catalogStatus = 'loading'
    await nextTick()

    const refresh = wrapper.get('[data-testid="refresh-models"]')
    expect(refresh.attributes('disabled')).toBeDefined()
    expect(refresh.attributes('aria-busy')).toBe('true')
    state.catalogStatus = 'ready'
    await nextTick()
    expect(refresh.attributes('disabled')).toBeUndefined()
  })

  it('allows retry after a catalog failure but prevents requests without a key', async () => {
    state.catalogStatus = 'error'
    state.catalogError = 'Fetch failed'
    const wrapper = mountPanel()
    await wrapper.get('[data-testid="refresh-models"]').trigger('click')
    expect(refreshImageModelCatalog).toHaveBeenCalledTimes(1)

    state.settings.miloRouter.apiKey = '  '
    await nextTick()
    expect(wrapper.get('[data-testid="refresh-models"]').attributes('disabled')).toBeDefined()
  })
})
