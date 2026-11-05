import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { TaskRecord } from '../../types'

const { rawState, downloadImageSource, ensureImageCached } = vi.hoisted(() => ({
  rawState: {
    tasks: [] as TaskRecord[],
    searchQuery: '',
    filterStatus: 'all',
    catalogStatus: 'ready',
    modelCatalog: [],
    imagePricing: {},
    settings: { miloRouter: { connectionStatus: 'connected' } },
    latestSubmittedTaskId: null,
    showToast: vi.fn(),
    setDetailTaskId: vi.fn(),
    setPrompt: vi.fn(),
    setSearchQuery: vi.fn(),
    setFilterStatus: vi.fn(),
  },
  downloadImageSource: vi.fn(),
  ensureImageCached: vi.fn(),
}))

vi.mock('../../stores/store', async () => ({
  storeState: (await import('vue')).reactive(rawState),
  editOutputs: vi.fn(),
  retryTask: vi.fn(),
  reuseConfig: vi.fn(),
  taskMatchesFilterStatus: () => true,
  taskMatchesSearchQuery: () => true,
}))
vi.mock('../../lib/downloadImage', () => ({ downloadImageSource }))
vi.mock('../../lib/imageCache', () => ({ ensureImageCached }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import { storeState as state } from '../../stores/store'
import StudioResultPanel from '../StudioResultPanel.vue'
import StudioSquarePreview from '../StudioSquarePreview.vue'
import StudioHistoryRail from '../StudioHistoryRail.vue'

enableAutoUnmount(afterEach)

function task(overrides: Partial<TaskRecord> = {}): TaskRecord {
  return {
    id: 'task-download',
    modelId: 'gpt-image-2',
    modelName: 'gpt-image-2',
    apiModel: 'gpt-image-2',
    apiKind: 'openai',
    prompt: 'A landscape',
    params: { size: 'auto', resolution: 'auto', n: 1 },
    status: 'done',
    inputImageIds: [],
    outputImages: ['image-one', 'image-two'],
    createdAt: Date.now(),
    elapsed: 1000,
    ...overrides,
  } as TaskRecord
}

function mountPanel() {
  return mount(StudioResultPanel, {
    global: { stubs: { StudioSquarePreview: true, StudioHistoryRail: true, StudioIcon: true } },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  state.tasks = [task()]
  ensureImageCached.mockReset().mockResolvedValue('data:image/png;base64,original')
  downloadImageSource.mockReset().mockResolvedValue(undefined)
})

describe('StudioResultPanel download', () => {
  it('offers a download button and downloads the original selected result image', async () => {
    const wrapper = mountPanel()
    const button = wrapper.get('[data-testid="download-result-image"]')
    expect(button.text()).toBe('imageStudio.lightbox.download')
    expect(button.attributes('type')).toBe('button')
    await button.trigger('click')
    await flushPromises()

    expect(ensureImageCached).toHaveBeenCalledWith('image-one')
    expect(downloadImageSource).toHaveBeenCalledTimes(1)
    await expect(downloadImageSource.mock.calls[0][0]).resolves.toBe('data:image/png;base64,original')
    expect(state.showToast).toHaveBeenCalledWith('imageStudio.lightbox.downloaded', 'success')
  })

  it('downloads the image selected using preview navigation', async () => {
    const wrapper = mountPanel()
    wrapper.findComponent(StudioSquarePreview).vm.$emit('select-image', 1)
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-testid="download-result-image"]').trigger('click')
    await flushPromises()

    expect(ensureImageCached).toHaveBeenCalledWith('image-two')
  })

  it('downloads the selected image from history', async () => {
    state.tasks = [task(), task({ id: 'older-task', outputImages: ['older-image'], createdAt: Date.now() - 1000 })]
    const wrapper = mountPanel()
    wrapper.findComponent(StudioHistoryRail).vm.$emit('select', 'older-task', 'older-image')
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-testid="download-result-image"]').trigger('click')
    await flushPromises()

    expect(ensureImageCached).toHaveBeenCalledWith('older-image')
  })

  it('disables repeated clicks while the download is pending', async () => {
    let finish!: () => void
    downloadImageSource.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve }))
    const wrapper = mountPanel()
    const button = wrapper.get('[data-testid="download-result-image"]')
    await button.trigger('click')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('aria-busy')).toBe('true')
    await button.trigger('click')
    expect(downloadImageSource).toHaveBeenCalledTimes(1)
    finish()
    await flushPromises()
    expect(button.attributes('disabled')).toBeUndefined()
  })

  it('reports failure and allows another download attempt', async () => {
    downloadImageSource.mockRejectedValueOnce(new Error('Download failed'))
    const wrapper = mountPanel()
    const button = wrapper.get('[data-testid="download-result-image"]')
    await button.trigger('click')
    await flushPromises()

    expect(state.showToast).toHaveBeenCalledWith('imageStudio.lightbox.downloadFailed', 'error')
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click')
    await flushPromises()
    expect(downloadImageSource).toHaveBeenCalledTimes(2)
  })

  it.each(['running', 'error'] as const)('does not offer download when a %s task has no result', (status) => {
    state.tasks = [task({ status, outputImages: [] })]
    const wrapper = mountPanel()
    expect(wrapper.find('[data-testid="download-result-image"]').exists()).toBe(false)
  })
})
