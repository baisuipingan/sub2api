import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import WorkbenchOverviewView from '../WorkbenchOverviewView.vue'
import WorkbenchTasksView from '../WorkbenchTasksView.vue'
import { workbenchElapsedSeconds, workbenchUserIdFilter } from '../workbenchDisplay'
import type { WorkbenchOverview, WorkbenchTask } from '@/api/admin/workbench'
import { searchUsers } from '@/api/admin/usage'

vi.mock('@/api/admin/usage', async importOriginal => ({ ...await importOriginal<typeof import('@/api/admin/usage')>(), searchUsers: vi.fn() }))

const { getOverview, listTasks } = vi.hoisted(() => ({ getOverview: vi.fn(), listTasks: vi.fn() }))
vi.mock('@/api/admin/workbench', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/admin/workbench')>(),
  getWorkbenchOverview: getOverview,
  listWorkbenchTasks: listTasks,
}))

const TableStub = defineComponent({
  props: ['data', 'columns', 'loading', 'rowKey'],
  setup(props, { slots }) {
    return () => h('div', props.data.length
      ? props.data.map((row: WorkbenchTask) => {
        const key = typeof props.rowKey === 'function' ? props.rowKey(row) : row[props.rowKey as keyof WorkbenchTask]
        return h('div', { key, 'data-task-key': key }, props.columns.map((column: { key: string }) => slots['cell-' + column.key]?.({ row })))
      })
      : slots.empty?.())
  },
})
const SelectStub = defineComponent({
  inheritAttrs: false,
  props: ['modelValue', 'options'], emits: ['update:modelValue'],
  setup(props, { emit, attrs }) {
    return () => h('select', { ...attrs, value: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLSelectElement).value) }, props.options.map((option: { value: string; label: string }) => h('option', { value: option.value }, option.label)))
  },
})
function overview(total = 3): WorkbenchOverview {
  return {
    scope: 'studio_cloud_jobs', days: 7, since: '2026-11-01T00:00:00Z', until: '2026-11-08T00:00:00Z',
    tasks: { total, queued: 0, generating: 0, saving: 0, completed: 2, partial: 0, failed: 1, unknown: 0, cancelled: 0 },
    calls: { total: 5, completed: 4, failed: 1, unknown: 0, cancelled: 0, billing_pending: 0, billing_unknown: 0 },
    output_images: 4, success_rate: 2 / 3, queue: { queued: 1, generating: 2, saving: 3, unknown: 4 },
    gallery: { tasks: 8, output_images: 10, assets: 15, used_bytes: 1024 },
  }
}
const task: WorkbenchTask = {
  task_id: 'job-unknown', user_id: 9, user_email: 'user@example.test', model: 'custom-image-model', protocol: 'openai', status: 'unknown',
  image_count: 5, output_count: 2, call_count: 3, billing_pending: 1, billing_unknown: 1,
  created_at: '2026-11-08T00:00:00Z', updated_at: '2026-11-08T00:00:01Z', finished_at: '2026-11-08T00:00:01Z', elapsed_ms: 1000, error_code: 'UPSTREAM_RESULT_UNKNOWN',
}
const wrappers: ReturnType<typeof mount>[] = []
function render(component: typeof WorkbenchOverviewView | typeof WorkbenchTasksView) {
  const wrapper = mount(component, { global: {
    plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })],
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
      DataTable: TableStub,
      Select: SelectStub,
      RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      Pagination: true,
    },
  } })
  wrappers.push(wrapper)
  return wrapper
}
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }

describe('workbench management pages', () => {
  beforeEach(() => {
    getOverview.mockReset().mockResolvedValue(overview())
    vi.mocked(searchUsers).mockReset().mockResolvedValue([])
    listTasks.mockReset().mockResolvedValue({ items: [task], total: 1, page: 1, page_size: 20, pages: 1 })
  })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

  it('shows real aggregate values and keeps queue/gallery scope explicit', async () => {
    const wrapper = render(WorkbenchOverviewView)
    await flushPromises()
    expect(getOverview).toHaveBeenCalledWith(7, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('66.7%')
    expect(wrapper.text()).toContain('外部 API 调用不计入')
    expect(wrapper.text()).toContain('不受上方统计时段限制')
    expect(wrapper.text()).toContain('不是整个存储桶用量')
    expect(wrapper.text()).toContain('结果待确认')
  })

  it('ignores an older reporting-period response after a new request', async () => {
    const older = deferred<WorkbenchOverview>()
    const newer = deferred<WorkbenchOverview>()
    getOverview.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const wrapper = render(WorkbenchOverviewView)
    await wrapper.find('select').setValue('30')
    newer.resolve({ ...overview(987), days: 30 })
    await flushPromises()
    older.resolve(overview(123))
    await flushPromises()
    expect(wrapper.text()).toContain('987')
    expect(wrapper.text()).not.toContain('123')
  })

  it('does not present a failed aggregate request as zero activity', async () => {
    getOverview.mockRejectedValueOnce(new Error('network failure'))
    const wrapper = render(WorkbenchOverviewView)
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('无法读取')
    expect(wrapper.text()).not.toContain('0%')
    expect(wrapper.findAll('.stat-card')).toHaveLength(0)
  })

  it('selects searched users instead of treating typed text as an ID', async () => {
    vi.mocked(searchUsers).mockResolvedValue([{ id: 9, username: '测试昵称', email: 'user@example.test', deleted: false }])
    const wrapper = render(WorkbenchTasksView)
    await flushPromises()
    await wrapper.find('#workbench-user').setValue('测试昵称')
    await wrapper.find('form').trigger('submit')
    expect(listTasks).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('请从搜索结果中选择用户')
    await new Promise(resolve => setTimeout(resolve, 350))
    await flushPromises()
    expect(searchUsers).toHaveBeenCalledWith('测试昵称')
    await wrapper.get('[role="option"]').trigger('click')
    await flushPromises()
    expect(listTasks).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 9 }, expect.any(AbortSignal))
    await wrapper.find('#workbench-model').setValue(' custom-image-model ')
    await wrapper.find('select').setValue('unknown')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(listTasks).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 9, model: 'custom-image-model', status: 'unknown' }, expect.any(AbortSignal))
  })

  it('distinguishes image count, call count and uncertain billing without a generation action', async () => {
    const wrapper = render(WorkbenchTasksView)
    await flushPromises()
    expect(wrapper.text()).toContain('结果 2 / 请求 5 张')
    expect(wrapper.text()).toContain('3 次计划生成请求')
    expect(wrapper.text()).toContain('不代表已经扣费')
    expect(wrapper.text()).toContain('扣费情况待核实')
    expect(wrapper.text()).not.toContain('可能已计费')
    expect(wrapper.text()).toContain('UPSTREAM_RESULT_UNKNOWN')
    expect(wrapper.findAll('button').some(button => /重试|生成图片/.test(button.text()))).toBe(false)
  })

  it('aborts an outstanding query on unmount', async () => {
    const pending = deferred<WorkbenchOverview>()
    getOverview.mockReturnValueOnce(pending.promise)
    const wrapper = render(WorkbenchOverviewView)
    const signal = getOverview.mock.calls[0][1] as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })

  it('keeps identical task IDs from different users as independent table rows', async () => {
    listTasks.mockResolvedValueOnce({ items: [task, { ...task, user_id: 10, user_email: 'other@example.test' }], total: 2, page: 1, page_size: 20, pages: 1 })
    const wrapper = render(WorkbenchTasksView)
    await flushPromises()
    expect(wrapper.findAll('[data-task-key]').map(row => row.attributes('data-task-key'))).toEqual(['9:job-unknown', '10:job-unknown'])
    expect(wrapper.text()).toContain('user@example.test')
    expect(wrapper.text()).toContain('other@example.test')
  })
})

describe('workbench display input boundaries', () => {
  it('rejects unsafe user IDs and preserves exact positive IDs', () => {
    for (const value of ['-1', '0', '1.5', '1e3', 'Infinity', '9007199254740992']) expect(() => workbenchUserIdFilter(value)).toThrow()
    expect(workbenchUserIdFilter('')).toBeUndefined()
    expect(workbenchUserIdFilter(' 9 ')).toBe(9)
  })
  it('never invents an elapsed time if the backend does not know it', () => {
    expect(workbenchElapsedSeconds({ ...task, elapsed_ms: null })).toBeNull()
    expect(workbenchElapsedSeconds({ ...task, elapsed_ms: -1000 })).toBe(0)
  })
})
