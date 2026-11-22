import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TaskProgress from '../TaskProgress.vue'

const mocks = vi.hoisted(() => ({ loadRunEvents: vi.fn(), tools: vi.fn(), toolDetail: vi.fn() }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: { tools: mocks.tools, toolDetail: mocks.toolDetail } }))
vi.mock('@/stores/workbenchChat', () => ({ chatError: () => '请求失败', useWorkbenchChatStore: () => ({ loadRunEvents: mocks.loadRunEvents }) }))

describe('TaskProgress terminal status', () => {
  it('shows cancellation even when a previous uncertain result retains its error details', () => {
    const wrapper = mount(TaskProgress, { props: {
      run: { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'cancelled', created_at: '', updated_at: '', error_code: 'RECOVERY_CONFIRMATION_REQUIRED', error_message: 'Previous execution interrupted' },
      events: [],
    }, global: { stubs: { Icon: true, ClarifyCard: true } } })
    expect(wrapper.get('[role="status"]').text()).toBe('已停止本次任务')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('终止本次任务')
    wrapper.unmount()
  })
  it('makes a failed lazy process request visible without changing the run outcome', async () => {
    mocks.loadRunEvents.mockRejectedValueOnce(new Error('network'))
    const wrapper = mount(TaskProgress, { props: {
      run: { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'completed', mode: 'office', created_at: '', updated_at: '' }, events: [],
    } })
    const details = wrapper.get('details')
    expect((details.element as HTMLDetailsElement).open).toBe(false)
    ;(details.element as HTMLDetailsElement).open = true
    await details.trigger('toggle')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('请求失败')
    expect(wrapper.text()).not.toContain('本次任务未完成')
    await wrapper.setProps({ run: { ...wrapper.props('run'), id: 'another-run' } })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('keeps native tool steps folded under their own answer', () => {
    const run = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'running' as const, browser_driven: true, created_at: '', updated_at: '' }
    const wrapper = mount(TaskProgress, { props: {
      run,
      events: [{ id: 'tool', run_id: run.id, sequence: 1, type: 'tool.started', payload: { operation_id: 'search', title: '搜索资料' }, created_at: '' }],
    } })
    expect(wrapper.get('[role="status"]').text()).toBe('搜索资料')
    const details = wrapper.get('details')
    expect((details.element as HTMLDetailsElement).open).toBe(false)
    expect(details.get('summary').text()).toContain('1 项')
    expect(details.get('li').text()).toContain('进行中')
    wrapper.unmount()
  })
  it('corrects historical search status and lazily displays the recorded reasons', async () => {
    const run = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'completed' as const,
      browser_driven: true, tool_calls: 2, created_at: '', updated_at: '' }
    const failed = { id: 'search-1', name: 'web_search', title: '联网搜索', state: 'failed' }
    const empty = { id: 'search-2', name: 'web_search', title: '联网搜索', state: 'completed' }
    mocks.tools.mockResolvedValueOnce([failed, empty])
    mocks.toolDetail.mockResolvedValueOnce({ ...failed, query: '上海天气', provider: 'brave', error_message: '搜索供应商响应超时' })
    mocks.toolDetail.mockResolvedValueOnce({ ...empty, query: '另一个关键词', provider: 'deepseek', result_count: 2, search_charge: {
      run_id: run.id, operation_id: empty.id, provider: 'deepseek', amount_usd: 0.005,
      amount_credits: 500000, billing_currency: 'CNY', usd_to_cny_rate: 7.2, created_at: '',
    } })
    const wrapper = mount(TaskProgress, { props: {
      run,
      events: [{ id: 'legacy', run_id: run.id, sequence: 1, type: 'tool.completed', payload: { operation_id: failed.id, title: failed.title, status: 'completed' }, created_at: '' }],
    } })
    const section = wrapper.get('details')
    ;(section.element as HTMLDetailsElement).open = true
    await section.trigger('toggle')
    await flushPromises()
    const steps = wrapper.findAll('li details')
    expect(steps).toHaveLength(2)
    expect(steps[0]?.get('summary').text()).toContain('失败')
    expect(mocks.toolDetail).not.toHaveBeenCalled()
    for (const step of steps) {
      ;(step.element as HTMLDetailsElement).open = true
      await step.trigger('toggle')
      await flushPromises()
    }
    expect(steps[0]?.text()).toContain('搜索供应商响应超时')
    expect(steps[0]?.text()).toContain('上海天气')
    expect(steps[1]?.text()).toContain('2 条')
    expect(steps[1]?.text()).toContain('本次搜索扣费')
    expect(steps[1]?.text()).toMatch(/CNY\s+0\.036/)
    wrapper.unmount()
  })
})
