import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import WorkbenchChatRunsView from '../WorkbenchChatRunsView.vue'
import WorkbenchResourcesView from '../WorkbenchResourcesView.vue'
const mocks = vi.hoisted(() => ({ runs: vi.fn(), detail: vi.fn(), messages: vi.fn(), resources: vi.fn(), access: vi.fn() }))
vi.mock('@/api/admin/workbench', async original => ({ ...await original<typeof import('@/api/admin/workbench')>(), listWorkbenchChatRuns: mocks.runs, getWorkbenchChatRun: mocks.detail, getWorkbenchConversationPage: mocks.messages, listWorkbenchChatResources: mocks.resources, getWorkbenchChatResourceAccess: mocks.access }))
const Table = defineComponent({ props: ['data', 'columns'], setup(props, { slots }) { return () => h('div', props.data.map((row: unknown) => h('div', props.columns.map((col: { key: string }) => slots[`cell-${col.key}`]?.({ row }))))) } })
const Select = defineComponent({ props: ['modelValue', 'options'], emits: ['update:modelValue'], setup(props, { attrs, emit }) { return () => h('select', { ...attrs, value: props.modelValue, onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLSelectElement).value) }, props.options.map((option: { value: string; label: string }) => h('option', { value: option.value }, option.label))) } })
const User = defineComponent({ props: ['modelValue'], emits: ['update:modelValue', 'change'], setup(_, { emit, expose }) { expose({ hasUnselectedQuery: () => false, clear: () => {} }); return () => h('button', { type: 'button', 'data-user': '', onClick: () => { emit('update:modelValue', 9); emit('change', { id: 9 }) } }, '选择用户') } })
const wrappers: ReturnType<typeof mount>[] = []
function render(component: typeof WorkbenchChatRunsView | typeof WorkbenchResourcesView) { const wrapper = mount(component, { global: { plugins: [createPinia(), createI18n({ legacy: false, locale: 'zh', messages: { zh } })], stubs: { AppLayout: { template: '<div><slot /></div>' }, TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' }, DataTable: Table, Select, UserSearchSelect: User, Pagination: true, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, ResourcePreview: true } } }); wrappers.push(wrapper); return wrapper }
const run = { id: 'run', conversation_id: 'c', user_id: 9, user_email: 'user@example.test', user_name: '昵称', model: 'text-model', mode: 'office', status: 'failed', model_calls: 2, tool_calls: 1, input_tokens: 12, output_tokens: 20, charge_credits: 0, created_at: '2026-11-19T00:00:00Z', error_message: '上游请求失败' }
const file = { id: 'resource', version_id: 'v1', name: '测试.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', source: 'generated', bytes: 100, user_id: 9, user_name: '昵称', user_email: 'user@example.test', created_at: '2026-11-19T00:00:00Z' }
describe('workbench conversation administrator views', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.runs.mockResolvedValue({ items: [run], total: 1, page: 1, pages: 1 }); mocks.resources.mockResolvedValue({ items: [file], total: 1, page: 1, pages: 1 }); mocks.detail.mockResolvedValue({ run, user_email: run.user_email, user_name: run.user_name, operations: [{ id: 'op', kind: 'model', state: 'completed', created_at: run.created_at, usage: { settled: true, input_tokens: 12, output_tokens: 20, cache_read_tokens: 4, charge_credits: 0, upstreams: [{ account_id: 42, account_name: '真实上游', upstream_model: 'upstream-model', outcome: 'usage' }] } }] }) })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
  it('uses a selected user and does not send empty filter fields', async () => { const wrapper = render(WorkbenchChatRunsView); await flushPromises(); await wrapper.get('[data-user]').trigger('click'); await flushPromises(); expect(mocks.runs).toHaveBeenLastCalledWith({ page: 1, page_size: 20, days: 7, user_id: 9 }, expect.any(AbortSignal)) })
  it('loads the conversation and compact trace, and prepends an earlier page', async () => {
    const prompt = { id: 'question', run_id: 'run', role: 'user', sequence: 3, content: { text: '现在的问题' }, created_at: run.created_at }
    const reply = { id: 'reply', run_id: 'run', role: 'assistant', sequence: 4, content: { text: '助手的回复' }, created_at: run.created_at }
    const earlier = { ...prompt, id: 'older', run_id: 'old-run', sequence: 1, content: { text: '之前的问题' } }
    mocks.messages.mockResolvedValueOnce({ conversation_title: '项目会话', messages: [prompt, reply], runs: [run], has_more: true, next_before: 3, selected_user_message: prompt })
    mocks.messages.mockResolvedValueOnce({ conversation_title: '项目会话', messages: [earlier], runs: [], has_more: false })

    const wrapper = render(WorkbenchChatRunsView)
    await flushPromises()
    expect(mocks.detail).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(button => button.text() === '查看会话')!.trigger('click')
    await flushPromises()
    expect(mocks.detail).toHaveBeenCalledWith('run', expect.any(AbortSignal))
    expect(mocks.messages).toHaveBeenCalledWith('run', undefined, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('现在的问题')
    expect(wrapper.text()).toContain('助手的回复')
    const modelStep = wrapper.get('aside details')
    expect(modelStep.attributes('open')).toBeUndefined()
    expect(modelStep.text()).toContain('真实上游')
    expect(modelStep.text()).toContain('缓存命中 4')

    await wrapper.findAll('button').find(button => button.text() === '加载更早的消息')!.trigger('click')
    await flushPromises()
    expect(mocks.messages).toHaveBeenLastCalledWith('run', 3, expect.any(AbortSignal))
    expect(wrapper.findAll('[data-message-id]').map(item => item.attributes('data-message-id'))).toEqual(['older', 'question', 'reply'])
    await wrapper.get('[data-message-id="older"] button').trigger('click')
    expect(mocks.detail).toHaveBeenLastCalledWith('old-run', expect.any(AbortSignal))
  })
  it('lists resources across users and filters by the selected user', async () => {
    const image = { ...file, id: 'image', version_id: 'v2', name: '图表.png', mime: 'image/png', user_id: 12, user_name: '另一位用户' }
    mocks.resources.mockResolvedValueOnce({ items: [file, image], total: 2, page: 1, pages: 1 })
    const wrapper = render(WorkbenchResourcesView)
    await flushPromises()
    expect(mocks.resources).toHaveBeenLastCalledWith({ page: 1, page_size: 20 }, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('测试.docx')
    expect(wrapper.text()).toContain('图表.png')
    expect(wrapper.text()).toContain('另一位用户')
    expect(wrapper.text()).toContain('助手生成')
    expect(wrapper.text()).not.toContain('云端图库')
    expect(wrapper.text()).not.toContain('删除文件')
    expect(mocks.access).not.toHaveBeenCalled()

    await wrapper.get('[data-user]').trigger('click')
    await flushPromises()
    expect(mocks.resources).toHaveBeenLastCalledWith({ page: 1, page_size: 20, user_id: 9 }, expect.any(AbortSignal))
    expect(wrapper.text()).not.toContain('图表.png')
    await wrapper.findAll('button').find(button => button.text() === '查看文件')!.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ResourcePreview' }).exists()).toBe(true)
    expect(mocks.access).not.toHaveBeenCalled()
  })
  it('contains long metadata within the file cells and displays a short format', async () => {
    const name = `${'很长的测试文件名'.repeat(30)}.docx`
    const email = `${'long'.repeat(36)}@example.test`
    mocks.resources.mockResolvedValue({ items: [{ ...file, name, user_email: email }], total: 1, page: 1, pages: 1 })
    const wrapper = render(WorkbenchResourcesView)
    await flushPromises()
    const filename = wrapper.get(`button[title="${name}"]`)
    expect(filename.get('span').classes()).toContain('truncate')
    expect(wrapper.get(`[title="${email}"]`).classes()).toContain('truncate')
    expect(wrapper.get(`[title="${file.mime}"]`).text()).toBe('DOCX')
    expect(wrapper.text()).not.toContain(file.mime)
    await filename.trigger('click')
    expect(wrapper.findComponent({ name: 'ResourcePreview' }).exists()).toBe(true)
  })
})
