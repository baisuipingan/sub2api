import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { DOMWrapper } from '@vue/test-utils'

const { updateAccountMock, showErrorMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: showErrorMock, showSuccess: vi.fn(), showInfo: vi.fn() })
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ get isSimpleMode() { return true } })
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      update: updateAccountMock,
      getGrokMediaEligibility: vi.fn(),
      updateGrokMediaEligibility: vi.fn(),
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn() }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import EditAccountModal from '../EditAccountModal.vue'
import Select from '@/components/common/Select.vue'

enableAutoUnmount(afterEach)

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function makeAccount(extra: Record<string, unknown> = {}) {
  return {
    id: 68,
    name: 'Image provider',
    notes: '',
    platform: 'openai',
    type: 'apikey',
    credentials: { api_key: 'sk-test', base_url: 'https://images.example.com' },
    credentials_status: { has_api_key: true },
    extra,
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  }
}

function mountModal(account: ReturnType<typeof makeAccount>) {
  return mount(EditAccountModal, {
    props: { show: true, account: account as never, groups: [], proxies: [] },
    global: {
      stubs: { BaseDialog: BaseDialogStub, Teleport: true, Transition: false }
    }
  })
}

type ModalWrapper = ReturnType<typeof mountModal>

function section(wrapper: ModalWrapper) {
  return wrapper.get('[data-testid="image-size-capability-section"]')
}

function rows(wrapper: ModalWrapper) {
  return section(wrapper).findAll('[data-testid="image-size-capability-row"]')
}

function modelInput(row: DOMWrapper<Element>) {
  return row.get('input[data-testid="image-size-model"]')
}

function upstreamInput(row: DOMWrapper<Element>) {
  return row.get('input[id^="image-size-upstream-"]')
}

async function addRow(wrapper: ModalWrapper) {
  await section(wrapper).findAll('button').find((button) => button.text() === 'admin.accounts.imageSizeCapability.add')!.trigger('click')
}

async function submit(wrapper: ModalWrapper) {
  await wrapper.get('form').trigger('submit')
  await flushPromises()
}

async function chooseTier(wrapper: ModalWrapper, index: number, tier: string) {
  const triggerId = rows(wrapper)[index].get('label[for^="image-size-tier-"]').attributes('for')
  const select = wrapper.findAllComponents(Select).find((component) => component.props('id') === triggerId)!
  await select.get('button.select-trigger').trigger('click')
  await select.findAll('[role="option"]').find((option) => option.text() === tier)!.trigger('click')
}

beforeEach(() => {
  updateAccountMock.mockReset()
  showErrorMock.mockReset()
  updateAccountMock.mockImplementation(async (_id: number, payload: Record<string, unknown>) => ({
    ...makeAccount(payload.extra as Record<string, unknown>),
    ...payload
  }))
})

describe('EditAccountModal 按生图尺寸映射模型', () => {
  it('回显各档规则并为三个控件提供可见标签', async () => {
    const wrapper = mountModal(makeAccount({
      image_size_capabilities: { 'gpt-image-2': { '4k': 'provider-large', '2k': 'provider-medium' } }
    }))
    await flushPromises()

    expect(rows(wrapper)).toHaveLength(2)
    expect(rows(wrapper).map((row) => (modelInput(row).element as HTMLInputElement).value)).toEqual(['gpt-image-2', 'gpt-image-2'])
    expect(rows(wrapper).map((row) => (upstreamInput(row).element as HTMLInputElement).value)).toEqual(['provider-large', 'provider-medium'])
    expect(rows(wrapper)[0].findAll('label')).toHaveLength(3)
  })

  it('仅保存已配置档位并保留 extra 其它字段', async () => {
    const wrapper = mountModal(makeAccount({
      some_other_flag: true,
      image_size_capabilities: { 'gpt-image-2': { '4k': 'provider-large' } }
    }))
    await submit(wrapper)

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0][1].extra).toMatchObject({
      some_other_flag: true,
      image_size_capabilities: { 'gpt-image-2': { '4k': 'provider-large' } }
    })
  })

  it('空表不写入尺寸映射字段', async () => {
    const wrapper = mountModal(makeAccount({ some_other_flag: true }))
    await submit(wrapper)

    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities).toBeUndefined()
    expect(updateAccountMock.mock.calls[0][1].extra.some_other_flag).toBe(true)
  })

  it('添加规则后可选择 2K 并使用任意供应商模型名', async () => {
    const wrapper = mountModal(makeAccount())
    await addRow(wrapper)
    await chooseTier(wrapper, 0, '2K')
    await upstreamInput(rows(wrapper)[0]).setValue('vendor/illustrator-v2@large')
    await submit(wrapper)

    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities).toEqual({
      'gpt-image-2': { '2k': 'vendor/illustrator-v2@large' }
    })
  })

  it('取消后重开同账号恢复已保存规则', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '4k': 'saved-model' } } }))
    await upstreamInput(rows(wrapper)[0]).setValue('unsaved-model')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await submit(wrapper)

    expect((upstreamInput(rows(wrapper)[0]).element as HTMLInputElement).value).toBe('saved-model')
    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities['gpt-image-2']['4k']).toBe('saved-model')
  })

  it('相同账号 id 的新对象会刷新配置', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '4k': 'old-model' } } }))
    await wrapper.setProps({ account: makeAccount({ image_size_capabilities: { 'gpt-image-2': { '2k': 'new-model' } } }) as never })
    await submit(wrapper)

    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities).toEqual({ 'gpt-image-2': { '2k': 'new-model' } })
  })

  it('切换供应商时不保留另一个账号的规则', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '4k': 'provider-a' } } }))
    await wrapper.setProps({ account: { ...makeAccount(), id: 69 } as never })

    expect(rows(wrapper)).toHaveLength(0)
  })

  it.each([
    ['白名单', { 'gpt-image-2': 'gpt-image-2', 'gpt-5': 'gpt-5' }],
    ['模型映射', { 'gpt-image-2': 'provider-model', 'gpt-5': 'provider-chat' }]
  ])('从%s提供对外生图模型候选', async (_mode, mapping) => {
    const account = makeAccount()
    Object.assign(account.credentials, { model_mapping: mapping })
    const wrapper = mountModal(account)
    expect(section(wrapper).findAll('datalist option').map((option) => option.attributes('value'))).toEqual(['gpt-image-2'])
    await addRow(wrapper)

    expect((modelInput(rows(wrapper)[0]).element as HTMLInputElement).value).toBe('gpt-image-2')
  })

  it('非 API Key 账号不显示按尺寸映射编辑器', async () => {
    const wrapper = mountModal({ ...makeAccount(), type: 'oauth' })
    await flushPromises()

    expect(wrapper.find('[data-testid="image-size-capability-section"]').exists()).toBe(false)
  })

  it('读取数组写法并规范化保存为对象', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: {
      ' gpt-image-2 ': [{ tier: ' 2K ', upstream: ' provider-medium ' }, { tier: '4k', upstream: 'provider-large' }]
    } }))
    expect(rows(wrapper)).toHaveLength(2)
    await submit(wrapper)

    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities).toEqual({
      'gpt-image-2': { '2k': 'provider-medium', '4k': 'provider-large' }
    })
  })

  it('重复模型与档位显示错误并阻止保存', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '2k': 'first-model' } } }))
    await addRow(wrapper)
    await modelInput(rows(wrapper)[1]).setValue(' gpt-image-2 ')
    await chooseTier(wrapper, 1, '2K')
    await upstreamInput(rows(wrapper)[1]).setValue('second-model')
    await submit(wrapper)

    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.imageSizeCapability.validationError')
    expect(rows(wrapper).map((row) => row.get('[role="alert"]').text())).toEqual([
      'admin.accounts.imageSizeCapability.duplicateRule', 'admin.accounts.imageSizeCapability.duplicateRule'
    ])
  })

  it.each(['model', 'upstream'])('不完整的 %s 字段显示错误并阻止保存', async (field) => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '2k': 'provider-medium' } } }))
    await (field === 'model' ? modelInput(rows(wrapper)[0]) : upstreamInput(rows(wrapper)[0])).setValue(' ')
    await submit(wrapper)

    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(rows(wrapper)[0].get('[role="alert"]').text()).toBe('admin.accounts.imageSizeCapability.incompleteRule')
  })

  it('新规则未填写完整时不能被静默丢弃，补全后可保存', async () => {
    const wrapper = mountModal(makeAccount())
    await addRow(wrapper)
    await submit(wrapper)
    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(rows(wrapper)[0].find('[role="alert"]').exists()).toBe(true)

    await upstreamInput(rows(wrapper)[0]).setValue('provider-model')
    expect(rows(wrapper)[0].find('[role="alert"]').exists()).toBe(false)
    await submit(wrapper)
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
  })

  it('不支持的旧档位要求修正后再保存', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '8k': 'old-model' } } }))
    await submit(wrapper)

    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(rows(wrapper)[0].get('[role="alert"]').text()).toBe('admin.accounts.imageSizeCapability.invalidTier')
  })

  it('不完整的旧数组规则显示出来并要求补全', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': [{ tier: '4k' }] } }))
    expect(rows(wrapper)).toHaveLength(1)
    await submit(wrapper)

    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(rows(wrapper)[0].get('[role="alert"]').text()).toBe('admin.accounts.imageSizeCapability.incompleteRule')
  })

  it('删除最后一条规则清除尺寸覆盖并保留其它配置', async () => {
    const wrapper = mountModal(makeAccount({ some_other_flag: true, image_size_capabilities: { 'gpt-image-2': { '4k': 'provider-large' } } }))
    await rows(wrapper)[0].get('button').trigger('click')
    expect(rows(wrapper)).toHaveLength(0)
    expect(section(wrapper).text()).toContain('admin.accounts.imageSizeCapability.emptyHint')
    await submit(wrapper)

    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities).toBeUndefined()
    expect(updateAccountMock.mock.calls[0][1].extra.some_other_flag).toBe(true)
  })

  it('删除前面的行后，后续行已选择的档位与上游模型仍对应', async () => {
    const wrapper = mountModal(makeAccount({ image_size_capabilities: { 'gpt-image-2': { '1k': 'small-model', '2k': 'large-model' } } }))
    await chooseTier(wrapper, 1, '4K')
    await rows(wrapper)[0].get('button').trigger('click')
    expect(rows(wrapper)[0].get('button.select-trigger').text()).toBe('4K')
    await submit(wrapper)

    expect(updateAccountMock.mock.calls[0][1].extra.image_size_capabilities).toEqual({ 'gpt-image-2': { '4k': 'large-model' } })
  })
})
