import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

// fork: image-size-routing
//
// 这个测试锁住「保存后不回显」这个真实故障：
// 提交时 updatePayload.extra 里必须带上 image_size_capabilities，
// 否则后端收不到、数据库不落库，重新打开弹窗自然是空的。

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

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function makeAccount(extra: Record<string, unknown> = {}) {
  return {
    id: 68,
    name: 'Editor-Openai-生图',
    notes: '',
    platform: 'openai',
    type: 'apikey',
    credentials: { api_key: 'sk-test', base_url: 'https://api.editor.vip' },
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
    props: { show: true, account: account as never, groups: [] as never },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Teleport: true,
        Transition: false
      }
    }
  })
}

beforeEach(() => {
  updateAccountMock.mockReset()
  showErrorMock.mockReset()
  updateAccountMock.mockImplementation(async (_id: number, payload: Record<string, unknown>) => ({
    ...makeAccount(payload.extra as Record<string, unknown>),
    ...payload
  }))
})

describe('EditAccountModal 生图尺寸能力', () => {
  it('打开时读取已有的 image_size_capabilities 并回显', async () => {
    const wrapper = mountModal(
      makeAccount({
        image_size_capabilities: {
          'gpt-image-2': { '4k': 'gpt-image-2-4K', '2k': 'gpt-image-2-2K' }
        }
      })
    )
    await wrapper.vm.$nextTick()
    await new Promise((r) => setTimeout(r, 0))

    const section = wrapper.find('[data-testid="image-size-capability-section"]')
    expect(section.exists()).toBe(true)

    // 两行数据都要回显出来
    const modelInputs = section.findAll('input[list="image-size-capability-models"]')
    expect(modelInputs.length).toBe(2)

    const values = modelInputs.map((input) => (input.element as HTMLInputElement).value)
    expect(values).toEqual(['gpt-image-2', 'gpt-image-2'])
  })

  it('提交时 payload.extra 必须包含 image_size_capabilities', async () => {
    const wrapper = mountModal(
      makeAccount({
        image_size_capabilities: { 'gpt-image-2': { '4k': 'gpt-image-2-4K' } }
      })
    )
    await wrapper.vm.$nextTick()
    await new Promise((r) => setTimeout(r, 0))

    await wrapper.find('form').trigger('submit')
    await new Promise((r) => setTimeout(r, 0))

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const [, payload] = updateAccountMock.mock.calls[0]
    expect(payload.extra).toBeTruthy()
    expect(payload.extra.image_size_capabilities).toEqual({
      'gpt-image-2': { '4k': 'gpt-image-2-4K' }
    })
  })

  it('空配置时不写入该字段，且不破坏 extra 里的其它键', async () => {
    const wrapper = mountModal(makeAccount({ some_other_flag: true }))
    await wrapper.vm.$nextTick()
    await new Promise((r) => setTimeout(r, 0))

    await wrapper.find('form').trigger('submit')
    await new Promise((r) => setTimeout(r, 0))

    const [, payload] = updateAccountMock.mock.calls[0]
    expect(payload.extra).toBeTruthy()
    expect(payload.extra.image_size_capabilities).toBeUndefined()
    expect(payload.extra.some_other_flag).toBe(true)
  })
})
