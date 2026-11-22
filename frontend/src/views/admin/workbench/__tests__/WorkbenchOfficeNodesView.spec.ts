import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { OfficeNode, OfficeNodeInput } from '@/api/admin/workbenchOfficeNodes'
import zh from '@/i18n/locales/zh'
import WorkbenchOfficeNodesView from '../WorkbenchOfficeNodesView.vue'

const { list, create, update, check, remove, showSuccess, showError } = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), check: vi.fn(), remove: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/admin/workbenchOfficeNodes', () => ({
  workbenchOfficeNodesApi: { list, create, update, check, remove },
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))

const node: OfficeNode = {
  id: 'dd184e7a-92f9-4ac7-a79d-2fd72d760fe2', name: 'Office A', url: 'https://198.51.100.17',
  state: 'disabled', health: 'online', max_concurrent: 2, assigned: 0, token_configured: true,
  status: { node_id: 'node-a', version: '1', actions: ['read_docx'], concurrency: 1, running: 0, queued: 0, awaiting_upload: 0, storage_origins: [] },
}

const upgradedStatus = { ...node.status!, concurrency_revision: 1 }
const wrappers: ReturnType<typeof mount>[] = []

function render() {
  const wrapper = mount(WorkbenchOfficeNodesView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })],
      stubs: { AppLayout: { template: '<main><slot /></main>' }, Teleport: true, TotpStepUpDialog: true },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

function button(wrapper: ReturnType<typeof render>, name: string) {
  const match = wrapper.findAll('button').find(item => item.text() === name)
  if (!match) throw new Error(`Missing button ${name}`)
  return match
}

describe('workbench compute node management', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    list.mockResolvedValue([])
  })

  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('registers a disabled HTTPS IP node, checks it and enables it without resending its token', async () => {
    const token = 'a'.repeat(32)
    create.mockResolvedValue({ ...node, health: 'unchecked' })
    check.mockResolvedValue(node)
    update.mockImplementation(async (_id, payload: OfficeNodeInput) => ({ ...node, ...payload }))
    const wrapper = render()
    await flushPromises()

    await button(wrapper, '添加节点').trigger('click')
    await wrapper.get('#office-node-name').setValue(node.name)
    await wrapper.get('#office-node-url').setValue(node.url)
    await wrapper.get('#office-node-token').setValue(token)
    await wrapper.get('#office-node-concurrency').setValue('2')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith({ name: node.name, url: node.url, state: 'disabled', max_concurrent: 2, token })
    expect(check).toHaveBeenCalledWith(node.id)
    expect(wrapper.text()).toContain('在线')
    expect(wrapper.text()).toContain('旧版节点 · 当前生效 1')
    await button(wrapper, '启用调度').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(node.id, { name: node.name, url: node.url, state: 'active', max_concurrent: 2 })
    expect(wrapper.text()).toContain('接受新任务')
  })

  it('shows the single applied limit after raising and lowering it online', async () => {
    const active = { ...node, state: 'active', max_concurrent: 1, status: upgradedStatus }
    list.mockResolvedValue([active])
    update.mockImplementation(async (_id, payload: OfficeNodeInput) => ({
      ...active, ...payload, status: { ...upgradedStatus, concurrency: payload.max_concurrent, concurrency_revision: 2, running: 2 },
    }))
    const wrapper = render()
    await flushPromises()

    await button(wrapper, '编辑').trigger('click')
    await wrapper.get('#office-node-concurrency').setValue('3')
    expect(wrapper.text()).toContain('在线节点会同步设置')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(update).toHaveBeenCalledWith(node.id, { name: node.name, url: node.url, state: 'active', max_concurrent: 3 })
    expect(wrapper.text()).toContain('并发上限3')
    expect(wrapper.text()).not.toContain('待同步')

    await button(wrapper, '编辑').trigger('click')
    await wrapper.get('#office-node-concurrency').setValue('1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('并发上限1')
    expect(wrapper.text()).toContain('执行中2')
    expect(wrapper.text()).not.toContain('待同步')
  })

  it('shows the last reported limit while the new setting is pending', async () => {
    list.mockResolvedValue([{ ...node, max_concurrent: 3, status: upgradedStatus }])
    const wrapper = render()
    await flushPromises()

    expect(wrapper.text()).toContain('并发上限3')
    expect(wrapper.text()).toContain('待同步 · 节点当前为 1')
  })

  it('keeps registered configuration visible when the connection fails', async () => {
    list.mockResolvedValue([node])
    check.mockResolvedValue({ ...node, health: 'unavailable', status: undefined })
    const wrapper = render()
    await flushPromises()

    await button(wrapper, '测试连接').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('连接异常')
    expect(wrapper.text()).toContain('并发上限2')
    expect(wrapper.text()).toContain('待节点确认')
    expect(wrapper.text()).toContain(node.url)
    expect(showError).toHaveBeenCalledWith('无法验证节点连接，请检查地址、证书、令牌和服务状态。')
  })

  it('keeps stop and delete behind the drain state and assigned jobs', async () => {
    list.mockResolvedValue([{ ...node, state: 'active', assigned: 1 }])
    update.mockImplementation(async (_id, payload: OfficeNodeInput) => ({ ...node, ...payload, assigned: 1 }))
    const wrapper = render()
    await flushPromises()

    await button(wrapper, '停止派新任务').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(node.id, { name: node.name, url: node.url, state: 'draining', max_concurrent: 2 })
    expect(wrapper.text()).toContain('仅处理已有任务')
    expect(button(wrapper, '停用').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').some(item => item.text() === '删除')).toBe(false)
  })
})
