import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { initialState, ensureStudioApiKey } = vi.hoisted(() => ({
  ensureStudioApiKey: vi.fn<() => Promise<string | null>>(),
  initialState: {
    catalogStatus: 'ready',
    catalogError: undefined as string | undefined,
    modelCatalog: [{ id: 'gpt-image-2', name: 'Preset name' }],
    settings: {
      selectedModelId: 'gpt-image-2',
      miloRouter: { apiKey: 'sk-studio', connectionStatus: 'connected' },
    },
    selectModel: vi.fn(),
    showToast: vi.fn(),
  },
}))

vi.mock('../../stores/store', async () => ({
  storeState: (await import('vue')).reactive(initialState),
}))
vi.mock('../../lib/studioAuth', () => ({ ensureStudioApiKey }))
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
  vi.clearAllMocks()
  ensureStudioApiKey.mockReset().mockResolvedValue('sk-studio')
  state.catalogStatus = 'ready'
  state.catalogError = undefined
  state.settings.miloRouter.apiKey = 'sk-studio'
})

describe('StudioCreationPanel connection and model refresh', () => {
  it('reconnects and refreshes through the shared automatic connection flow', async () => {
    const wrapper = mountPanel()
    await wrapper.get('[data-testid="refresh-models"]').trigger('click')
    await flushPromises()

    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)
  })

  it('allows connection retry even when the initial key request failed', async () => {
    state.settings.miloRouter.apiKey = ''
    state.settings.miloRouter.connectionStatus = 'idle'
    state.catalogStatus = 'idle'
    const wrapper = mountPanel()
    expect(wrapper.get('[data-testid="refresh-models"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="refresh-models"]').trigger('click')
    await flushPromises()

    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)
  })

  it('disables repeated refreshes while obtaining a key', async () => {
    let finish!: (key: string) => void
    ensureStudioApiKey.mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
    const wrapper = mountPanel()
    const refresh = wrapper.get('[data-testid="refresh-models"]')
    await refresh.trigger('click')
    expect(refresh.attributes('disabled')).toBeDefined()
    expect(refresh.attributes('aria-busy')).toBe('true')
    await refresh.trigger('click')
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)
    finish('sk-studio')
    await flushPromises()
    expect(refresh.attributes('disabled')).toBeUndefined()
  })

  it('reflects loading and disables refresh until the catalog request finishes', async () => {
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

  it('uses the same reconnect flow from the catalog error retry button', async () => {
    state.catalogStatus = 'error'
    state.catalogError = 'Fetch failed'
    const wrapper = mountPanel()
    await wrapper.findAll('button').find((button) => button.text() === 'imageStudio.creation.refetch')!.trigger('click')
    await flushPromises()

    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)
  })

  it.each([null, new Error('Connection failed')])('reports connection failure and permits another attempt', async (failure) => {
    if (failure instanceof Error) ensureStudioApiKey.mockRejectedValueOnce(failure)
    else ensureStudioApiKey.mockResolvedValueOnce(failure)
    const wrapper = mountPanel()
    const refresh = wrapper.get('[data-testid="refresh-models"]')
    await refresh.trigger('click')
    await flushPromises()

    expect(state.showToast).toHaveBeenCalledWith('imageStudio.creation.connectionFailed', 'error')
    expect(refresh.attributes('disabled')).toBeUndefined()
    await refresh.trigger('click')
    await flushPromises()
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(2)
  })
})
