import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'

const { rawState } = vi.hoisted(() => ({
  rawState: {
    tasks: [] as TaskRecord[],
    detailTaskId: 'failed-task' as string | null,
    detailImageId: null as string | null,
    modelCatalog: [],
    streamPreviews: {},
    setDetailTaskId: vi.fn(),
    setLightboxImageId: vi.fn(),
    showToast: vi.fn(),
    setConfirmDialog: vi.fn(),
  },
}))
vi.mock('../../stores/store', async () => ({
  storeState: (await import('vue')).reactive(rawState),
  editOutputs: vi.fn(), removeTask: vi.fn(), retryTask: vi.fn(), reuseConfig: vi.fn(),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('../../lib/imageCache', () => ({
  ensureImageCached: vi.fn().mockResolvedValue(undefined),
}))

import { storeState as state } from '../../stores/store'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StudioDetailModal from '../StudioDetailModal.vue'

enableAutoUnmount(afterEach)

function mountDetails() {
  return mount(StudioDetailModal, {
    global: { stubs: { StudioIcon: true, Teleport: true, Transition: false } },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  state.detailTaskId = 'failed-task'
  state.detailImageId = null
  state.tasks = [{
    id: 'failed-task', modelId: 'gpt-image-2', modelName: 'gpt-image-2', apiModel: 'gpt-image-2',
    prompt: 'A landscape', params: { ...DEFAULT_PARAMS }, status: 'error',
    error: 'Request failed\nHTTP 400 [unsupported_size]: This model does not support 4K. Choose 2K.',
    inputImageIds: [], outputImages: [], createdAt: Date.now(), finishedAt: Date.now(), elapsed: 100,
  }]
})

describe('StudioDetailModal shared dialog', () => {
  it('opens the selected result image and resets to the first image for an old caller without a selection', async () => {
    state.tasks = [{ ...state.tasks[0], status: 'done', error: null, outputImages: ['image-one', 'image-two', 'image-three'] }]
    state.detailImageId = 'image-three'
    const wrapper = mountDetails()
    await flushPromises()
    expect(wrapper.text()).toContain('3 / 3')
    state.detailImageId = null
    await flushPromises()
    expect(wrapper.text()).toContain('1 / 3')
  })
  it('uses the common dialog header and keeps the detailed task error visible', async () => {
    const wrapper = mountDetails()
    await flushPromises()

    expect(wrapper.getComponent(BaseDialog).props()).toMatchObject({ show: true, title: 'imageStudio.detail.title', width: 'extra-wide', closeOnEscape: false })
    expect(wrapper.findAll('.modal-header')).toHaveLength(1)
    expect(wrapper.findAll('[role="dialog"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="detail-error-message"]').text()).toContain('Choose 2K.')
    expect(wrapper.text()).toContain('gpt-image-2')
    expect(wrapper.text()).toContain('A landscape')
  })

  it('closes using the shared dialog close button', async () => {
    const wrapper = mountDetails()
    await wrapper.get('button[aria-label="Close modal"]').trigger('click')
    expect(state.setDetailTaskId).toHaveBeenCalledOnce()
    expect(state.setDetailTaskId).toHaveBeenCalledWith(null)
  })

  it('closes when the common backdrop is clicked', async () => {
    const wrapper = mountDetails()
    await wrapper.get('.modal-overlay').trigger('click')
    expect(state.setDetailTaskId).toHaveBeenCalledWith(null)
  })

  it('closes once on Escape through the existing studio modal stack', () => {
    mountDetails()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(state.setDetailTaskId).toHaveBeenCalledOnce()
    expect(state.setDetailTaskId).toHaveBeenCalledWith(null)
  })

  it('uses the common loading spinner while a task is generating', async () => {
    state.tasks = [{ ...state.tasks[0], status: 'running', error: null }]
    const wrapper = mountDetails()
    await flushPromises()

    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
    expect(wrapper.text()).toContain('imageStudio.status.running')
    expect(wrapper.find('[data-testid="detail-error-message"]').exists()).toBe(false)
  })
})
