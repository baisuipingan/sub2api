import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import type { TaskRecord } from '../../types'
import StudioHistoryRail from '../StudioHistoryRail.vue'
import { storeState as state } from '../../stores/store'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)
afterEach(() => {
  state.galleryUserId = null
  state.gallerySyncStatus = {}
  state.galleryStats = null
  vi.restoreAllMocks()
})

function task(id: string, images: string[], status: TaskRecord['status'] = 'done'): TaskRecord {
  return {
    id,
    prompt: id,
    params: { size: 'auto', resolution: 'auto', n: images.length },
    status,
    inputImageIds: [],
    outputImages: images,
    error: null,
    createdAt: Date.now(),
    finishedAt: null,
    elapsed: null,
  } as TaskRecord
}

function mountRail(collapsed: boolean) {
  return mount(StudioHistoryRail, {
    props: {
      tasks: [task('batch', ['image-1', 'image-2', 'image-3', 'image-4', 'image-5']), task('other', ['other-image'])],
      activeTaskId: 'batch',
      collapsed,
    },
    global: { stubs: { StudioIcon: true, StudioImageThumbnail: true, LoadingSpinner: true } },
  })
}

describe('StudioHistoryRail', () => {
  it('adds a compact cloud attention cue without another toolbar row', async () => {
    state.galleryUserId = 7
    state.galleryStats = { tasks: 18, done_tasks: 17, failed_tasks: 1, output_images: 20, assets: 22, used_bytes: 100, max_tasks: 1000, max_assets: 500, max_bytes: 1 << 30 }
    state.gallerySyncStatus = { batch: 'error', other: 'pending' }
    const openSettings = vi.spyOn(state, 'setShowSettings')
    const wrapper = mountRail(false)
    const summary = wrapper.get('[data-testid="studio-history-sync-summary"]')
    expect(summary.attributes('title')).toContain('imageStudio.gallery.statusSummary')
    expect(summary.attributes('title')).toContain('imageStudio.gallery.historyLoadedCount')
    expect(wrapper.get('[data-studio-history-task-id="batch"] [title="imageStudio.gallery.syncFailed"]').exists()).toBe(true)
    await summary.trigger('click')
    expect(openSettings).toHaveBeenCalledWith(true)
  })

  it('keeps failed tasks without images visible for history deletion', () => {
    const wrapper = mountRail(false)
    void wrapper.setProps({ tasks: [task('failed', [], 'error')] })
    return wrapper.vm.$nextTick().then(() => {
      expect(wrapper.get('[data-studio-history-task-id="failed"]').attributes('aria-label')).toBe('failed')
    })
  })
  it('keeps one selected history item per task when another image in the batch is shown', async () => {
    const wrapper = mountRail(false)
    const entries = wrapper.findAll('[data-testid="studio-history-items"] > button')
    expect(entries).toHaveLength(2)
    expect(entries[0].attributes('aria-current')).toBe('true')
    expect(entries[0].classes()).toContain('ring-primary-500')
    expect(entries[1].attributes('aria-current')).toBeUndefined()

    await entries[0].trigger('click')
    expect(wrapper.emitted('select')).toEqual([['batch', 'image-1']])
  })

  it('shows a horizontal mobile list even when the desktop rail is collapsed', () => {
    const wrapper = mountRail(true)
    const rail = wrapper.get('[data-testid="studio-history-rail"]')
    const list = wrapper.get('[data-testid="studio-history-items"]')
    expect(rail.classes()).toContain('w-full')
    expect(rail.classes()).toContain('lg:w-10')
    expect(list.classes()).toContain('overflow-x-auto')
    expect(list.classes()).toContain('lg:overflow-y-auto')
    expect(list.classes()).toContain('lg:hidden')
    expect(list.findAll('button')).toHaveLength(2)
    expect(wrapper.get('button[aria-expanded]').classes()).toContain('hidden')
    expect(wrapper.get('button[aria-expanded]').classes()).toContain('lg:flex')
  })
})
