import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TaskProgress from '../TaskProgress.vue'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'

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
    const process = wrapper.get<HTMLDetailsElement>('.bs-chat-turn-process')
    expect(process.element.open).toBe(false)
    await process.get('summary').trigger('click')
    expect(process.element.open).toBe(true)
    const details = process.get<HTMLDetailsElement>('.bs-chat-turn-process-body > details')
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
    const process = wrapper.get<HTMLDetailsElement>('.bs-chat-turn-process')
    expect(process.element.open).toBe(true)
    const details = process.get<HTMLDetailsElement>('.bs-chat-turn-process-body > details')
    expect((details.element as HTMLDetailsElement).open).toBe(false)
    expect(details.get('summary').text()).toContain('1 项')
    expect(details.get('li').text()).toContain('进行中')
    wrapper.unmount()
  })
  it('keeps status at the live edge through model and tool transitions', async () => {
    const run: ChatRun = { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'running', browser_driven: true, created_at: '', updated_at: '' }
    const events: ChatEvent[] = []
    const wrapper = mount(TaskProgress, { props: { run, events, awaitingFinalAnswer: true, reasoningSummary: '推理过程' },
      slots: { default: '<p data-answer>最终回答</p><button data-file>下载文件</button>' } })
    const process = wrapper.get<HTMLDetailsElement>('.bs-chat-turn-process')
    async function append(type: string, operation_id: string, payload: ChatEvent['payload'] = {}) {
      const seq = events.length + 1
      events.push({ id: String(seq), run_id: run.id, sequence: seq, type, payload: { operation_id, ...payload }, created_at: '' })
      await wrapper.setProps({ events: [...events] })
    }
    expect(wrapper.get('[role="status"]').text()).toBe('正在生成回答…')
    await process.get('summary').trigger('click')
    expect(process.element.open).toBe(true)
    await append('model.started', 'first')
    await append('assistant.delta', 'first', { text: '我先查天气。' })
    await append('model.completed', 'first', { tool_count: 1 })
    expect(wrapper.get('[role="status"]').text()).toBe('准备调用工具…')
    await append('tool.started', 'search', { title: '联网搜索', preview: '天气预报' })
    expect(wrapper.get('[role="status"]').text()).toBe('联网搜索')
    await append('tool.completed', 'search')
    expect(wrapper.get('.tool-preview').text()).toBe('天气预报')
    expect(wrapper.get('[role="status"]').text()).toBe('正在整理工具结果…')
    await append('model.started', 'second')
    expect(wrapper.get('[role="status"]').text()).toBe('正在生成回答…')
    await append('assistant.delta', 'second', { text: '核对后生成文档。' })
    await append('model.completed', 'second', { tool_count: 1 })
    await append('tool.completed', 'doc', { title: '生成文档' })
    const children = process.get('.bs-chat-turn-process-body').element.children
    expect([...children].map(child => child.tagName)).toEqual(['P', 'DETAILS', 'P', 'DETAILS', 'DETAILS', 'DIV'])
    expect(wrapper.findAll('.bs-chat-step-commentary').map(block => block.text())).toEqual(['我先查天气。', '核对后生成文档。'])
    expect(process.find('[data-answer], [data-file]').exists()).toBe(false)
    await wrapper.setProps({ connectionInterrupted: true })
    expect(wrapper.get('[role="status"]').text()).toBe('连接暂时异常，等待同步')
    expect(wrapper.get('[role="status"]').find('.bs-chat-spin').exists()).toBe(false)
    await wrapper.setProps({ run: { ...run, status: 'completed' }, connectionInterrupted: false })
    expect(process.element.open).toBe(true)
    expect(wrapper.get('[role="status"]').text()).toBe('正在整理回答…')
    await wrapper.setProps({ awaitingFinalAnswer: false })
    expect(process.element.open).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(await wrapper.get('[data-answer]').isVisible()).toBe(true)
    expect(await wrapper.get('[data-file]').isVisible()).toBe(true)
    await process.get('summary').trigger('click')
    expect(process.element.open).toBe(true)
    expect(process.text()).toContain('推理过程')
    wrapper.unmount()
  })
  it('corrects historical search status and lazily displays the recorded reasons', async () => {
    const run = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'completed' as const,
      browser_driven: true, tool_calls: 2, created_at: '', updated_at: '' }
    const query = '上海天气预报和未来七天的气温变化，以及周末的降水概率'
    const failed = { id: 'search-1', name: 'web_search', title: '联网搜索', state: 'failed', preview: query }
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
    const process = wrapper.get<HTMLDetailsElement>('.bs-chat-turn-process')
    await process.get('summary').trigger('click')
    await flushPromises()
    const section = process.get<HTMLDetailsElement>('.bs-chat-turn-process-body > details')
    expect(section.get('.bs-chat-tool-group-preview').text()).toBe(query)
    ;(section.element as HTMLDetailsElement).open = true
    await section.trigger('toggle')
    await flushPromises()
    const steps = wrapper.findAll('li details')
    expect(steps).toHaveLength(2)
    expect(steps[0]?.get('summary').text()).toContain('失败')
    expect(steps[0]?.get('.tool-preview').text()).toBe(query)
    expect(mocks.toolDetail).not.toHaveBeenCalled()
    for (const step of steps) {
      const details = step.element as HTMLDetailsElement
      details.open = true
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
