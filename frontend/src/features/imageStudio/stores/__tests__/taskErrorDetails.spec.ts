import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { DEFAULT_PARAMS } from '../../types'

const { putTask } = vi.hoisted(() => ({ putTask: vi.fn() }))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  putTask,
  putTaskWithPendingSync: putTask,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('../../lib/studioKeySession', async () => ({
  ...await vi.importActual<typeof import('../../lib/studioKeySession')>('../../lib/studioKeySession'),
  isCurrentStudioApiKey: () => true,
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import { storeState as state, submitTask, useStore } from '../store'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import StudioResultPanel from '../../components/StudioResultPanel.vue'
import StudioDetailModal from '../../components/StudioDetailModal.vue'
import Input from '@/components/common/Input.vue'

enableAutoUnmount(afterEach)
afterEach(() => vi.unstubAllGlobals())

beforeEach(() => {
  putTask.mockReset().mockResolvedValue(undefined)
  state.setMiloRouterConfig({ apiKey: 'sk-example-private-key' })
  state.setMiloRouterConfig({ connectionStatus: 'connected' })
  useStore.setState({
    settings: { ...state.settings, selectedModelId: 'gpt-image-2' },
    modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', api: 'openai' }] }),
    catalogStatus: 'ready',
    galleryInitialized: true, galleryStatusKnown: true, galleryEnabled: false, studioJobsEnabled: false,
    prompt: 'A landscape',
    params: { ...DEFAULT_PARAMS, resolution: '4K', size: '2880x2880' },
    inputImages: [], maskDraft: null, tasks: [], streamPreviews: {}, imagePricing: {},
    filterStatus: 'all', searchQuery: '', detailTaskId: null,
  })
})

describe('image provider error persistence and display', () => {
  it.each([
    { status: 400, code: 'unsupported_size', message: 'The selected model does not support 4K; choose 2K.' },
    { status: 502, code: 'request_failed', message: 'The image service is restarting; retry in 30 seconds.' },
  ])('preserves actionable HTTP $status details through the real adapter, store, result panel, and details', async ({ status, code, message }) => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValueOnce(new Response(JSON.stringify({
      error: { code, message: `${message} Authorization: Bearer hidden-authorization api_key="hidden-key" sk-example-private-key` },
    }), { status, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await submitTask()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/generations')
    const failed = state.tasks[0]
    expect(failed.status).toBe('error')
    expect(failed.error).toContain(`HTTP ${status} [${code}]`)
    expect(failed.error).toContain(message)
    for (const secret of ['hidden-authorization', 'hidden-key', 'sk-example-private-key']) expect(failed.error).not.toContain(secret)
    expect(putTask).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'error', error: failed.error }), expect.any(String))

    const result = mount(StudioResultPanel, {
      global: { stubs: { StudioSquarePreview: true, StudioHistoryRail: true, StudioIcon: true } },
    })
    state.setDetailTaskId(failed.id)
    const details = mount(StudioDetailModal, { global: { stubs: { StudioIcon: true, Teleport: true, Transition: false } } })
    await flushPromises()

    expect(result.get('[data-testid="result-error-message"]').text()).toContain(message)
    expect(details.get('[data-testid="detail-error-message"]').text()).toContain(message)
    expect(result.text()).toContain(`HTTP ${status}`)
    expect(details.text()).toContain(`HTTP ${status}`)
    for (const secret of ['hidden-authorization', 'hidden-key', 'sk-example-private-key']) {
      expect(result.text()).not.toContain(secret)
      expect(details.text()).not.toContain(secret)
    }
  })

  it('shows safe status information when an upstream returns a raw HTML page', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValueOnce(new Response('<html><body>Proxy error detail</body></html>', {
      status: 503, headers: { 'Content-Type': 'text/html' },
    })))
    await submitTask()

    expect(state.tasks[0].error).toContain('HTTP 503')
    expect(state.tasks[0].error).not.toContain('Proxy error detail')
    expect(state.tasks[0].error).not.toContain('<html>')
  })

  it('retains a plain-text upstream explanation and redacts credentials', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValueOnce(new Response('Choose 2K for this model. Authorization: Bearer hidden-token', {
      status: 400, headers: { 'Content-Type': 'text/plain' },
    })))
    await submitTask()

    expect(state.tasks[0].error).toContain('Choose 2K for this model.')
    expect(state.tasks[0].error).not.toContain('hidden-token')
  })

  it('uses the shared search input while retaining history filtering', async () => {
    const result = mount(StudioResultPanel, {
      global: { stubs: { StudioSquarePreview: true, StudioHistoryRail: true, StudioIcon: true } },
    })
    expect(result.findComponent(Input).exists()).toBe(true)
    expect(result.get('label[for="studio-history-search"]').text()).toBe('imageStudio.search.ariaHistory')
    await result.get('#studio-history-search').setValue('4K unsupported')
    expect(state.searchQuery).toBe('4K unsupported')
  })
})
