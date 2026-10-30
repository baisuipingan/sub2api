import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

/**
 * 结果面板是用户判断「这次出了什么问题」的唯一入口。
 *
 * 这里锁住三处曾经缺失、后来补齐的行为：
 *  1. 失败原因必须显示在主面板（而不是只能去详情弹窗看）
 *  2. 失败任务必须有重试入口
 *  3. 运行中要有实时计时，而不是一直显示「耗时计算中」
 *  4. 部分图片失败要提示，不能静默少图
 */

const { state, retryTask, reuseConfig, editOutputs, editOutputMask } = vi.hoisted(() => ({
  state: {
    imagePricing: {} as Record<string, Record<string, number>>,
    catalogStatus: 'ready' as const,
    modelCatalog: [
    {
      id: 'gpt-image-2',
      name: 'GPT Image 2',
      api: 'openai',
      requestModels: { auto: 'gpt-image-2', '1K': 'gpt-image-2' },
      capabilities: { resolutions: ['auto', '1K'], imageCount: { min: 1, max: 1 }, referenceImages: 0 },
    },
  ] as unknown[],
    inputImages: [] as unknown[],
    tasks: [] as unknown[],
    streamPreviews: {} as Record<string, string>,
    settings: {
      miloRouter: { connectionStatus: 'connected' as const },
      selectedModelId: 'gpt-image-2',
    },
    setDetailTaskId: vi.fn(),
    openFavoritePicker: vi.fn(),
    setShowSettings: vi.fn(),
    showToast: vi.fn(),
  },
  retryTask: vi.fn(async () => {}),
  reuseConfig: vi.fn(),
  editOutputs: vi.fn(),
  editOutputMask: vi.fn(),
}))

// vi.mock 会被提升到文件顶部，工厂里只能用 hoisted 定义的变量
vi.mock('../../stores/store', () => ({
  storeState: state,
  retryTask,
  reuseConfig,
  editOutputs,
  editOutputMask,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    // 保留插值参数：否则「已等待 N 秒」的 N 变化在断言里不可见
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key,
    }),
  }
})

import StudioActiveResult from '../StudioActiveResult.vue'
import type { TaskRecord } from '../../types'

function makeTask(overrides: Partial<TaskRecord> = {}): TaskRecord {
  return {
    id: 't1',
    modelId: 'gpt-image-2',
    modelName: 'GPT Image 2',
    apiModel: 'gpt-image-2',
    prompt: '一只猫',
    params: { size: 'auto', resolution: 'auto', n: 1 } as TaskRecord['params'],
    status: 'done',
    outputImages: [{ id: 'img1', url: 'data:image/png;base64,x' }] as TaskRecord['outputImages'],
    createdAt: Date.now(),
    ...overrides,
  } as TaskRecord
}

function mountPanel(task: TaskRecord) {
  return mount(StudioActiveResult, {
    props: { task },
    global: { stubs: { StudioResultImage: true, StudioIcon: true } },
  })
}

describe('StudioActiveResult', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-30T10:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('失败任务在主面板显示失败原因', async () => {
    const wrapper = mountPanel(makeTask({ status: 'error', outputImages: [], error: 'timeout' }))
    await nextTick()
    // 文案来自 apiErrors 的分类，timeout → errors.timeout
    expect(wrapper.text()).toContain('imageStudio.errors.timeout')
  })

  it('失败任务提供重试按钮（此前只对已完成任务渲染）', async () => {
    const wrapper = mountPanel(makeTask({ status: 'error', outputImages: [], error: 'boom' }))
    await nextTick()

    const retry = wrapper.findAll('button').find((btn) => btn.text().includes('imageStudio.task.retry'))
    expect(retry).toBeTruthy()

    await retry!.trigger('click')
    await nextTick()
    expect(retryTask).toHaveBeenCalledTimes(1)
  })

  it('运行中按秒刷新已等待时间', async () => {
    const createdAt = Date.now()
    const wrapper = mountPanel(makeTask({ status: 'running', outputImages: [], createdAt }))
    await nextTick()

    expect(wrapper.text()).toContain('imageStudio.task.elapsedSeconds')

    // 5 秒后仍显示「耗时」标签，但秒数应已变化（这里断言组件重算，不硬编码文案）
    expect(wrapper.text()).toContain('"seconds":0')

    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(wrapper.text()).toContain('"seconds":5')
  })

  it('完成后不再跳秒（定时器停止）', async () => {
    const wrapper = mountPanel(makeTask({ status: 'done', elapsed: 4200 }))
    await nextTick()
    const snapshot = wrapper.html()

    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(wrapper.html()).toBe(snapshot)
  })

  it('部分图片失败时给出提示，而不是静默少图', async () => {
    const wrapper = mountPanel(
      makeTask({
        status: 'done',
        params: { size: 'auto', resolution: 'auto', n: 4 } as TaskRecord['params'],
        outputImages: [{ id: 'img1', url: 'data:image/png;base64,x' }] as TaskRecord['outputImages'],
        outputErrors: [{ requestIndex: 1, error: 'timeout' }],
      }),
    )
    await nextTick()
    expect(wrapper.text()).toContain('imageStudio.result.partialFailed')
  })

  it('全部成功时不显示部分失败提示', async () => {
    const wrapper = mountPanel(makeTask())
    await nextTick()
    expect(wrapper.text()).not.toContain('imageStudio.result.partialFailed')
  })
})
