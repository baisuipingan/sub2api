import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { DOMWrapper } from '@vue/test-utils'

const { createAccountMock, showErrorMock } = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: showErrorMock, showSuccess: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn() })
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ get isSimpleMode() { return true } })
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      probeUpstreamBilling: vi.fn().mockResolvedValue({}),
      syncUpstreamModels: vi.fn().mockResolvedValue({ models: [], metadata: {} }),
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import CreateAccountModal from '../CreateAccountModal.vue'
import ImageSizeCapabilityEditor from '../ImageSizeCapabilityEditor.vue'
import Select from '@/components/common/Select.vue'

enableAutoUnmount(afterEach)

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, groups: [], proxies: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Teleport: true,
        Transition: false,
        OAuthAuthorizationFlow: true,
        ConfirmDialog: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        QuotaLimitCard: true
      }
    }
  })
}

type ModalWrapper = ReturnType<typeof mountModal>

async function clickButton(wrapper: ModalWrapper, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button, `button containing ${text}`).toBeDefined()
  await button!.trigger('click')
}

async function openAPIKeyForm(wrapper: ModalWrapper) {
  await clickButton(wrapper, 'OpenAI')
  await clickButton(wrapper, 'API Key')
  await flushPromises()
}

async function fillCredentials(wrapper: ModalWrapper) {
  await wrapper.get('[data-tour="account-form-name"]').setValue('Image provider')
  await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-image-provider')
}

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

async function chooseTier(wrapper: ModalWrapper, index: number, tier: string) {
  const triggerId = rows(wrapper)[index].get('label[for^="image-size-tier-"]').attributes('for')
  const select = wrapper.findAllComponents(Select).find((component) => component.props('id') === triggerId)!
  await select.get('button.select-trigger').trigger('click')
  await select.findAll('[role="option"]').find((option) => option.text() === tier)!.trigger('click')
}

async function addRule(wrapper: ModalWrapper, tier: string, upstream: string) {
  await section(wrapper).findAll('button').find((button) => button.text() === 'admin.accounts.imageSizeCapability.add')!.trigger('click')
  const index = rows(wrapper).length - 1
  await modelInput(rows(wrapper)[index]).setValue('gpt-image-2')
  await chooseTier(wrapper, index, tier)
  await upstreamInput(rows(wrapper)[index]).setValue(upstream)
}

async function submit(wrapper: ModalWrapper) {
  await wrapper.get('form#create-account-form').trigger('submit')
  await flushPromises()
}

beforeEach(() => {
  createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'openai', type: 'apikey' })
  showErrorMock.mockReset()
})

describe('CreateAccountModal 按生图尺寸映射模型', () => {
  it('仅在 OpenAI API Key 表单显示与编辑弹窗共用的编辑器', async () => {
    const wrapper = mountModal()
    expect(wrapper.findComponent(ImageSizeCapabilityEditor).exists()).toBe(false)

    await clickButton(wrapper, 'OpenAI')
    expect(wrapper.findComponent(ImageSizeCapabilityEditor).exists()).toBe(false)
    await clickButton(wrapper, 'API Key')
    expect(wrapper.findComponent(ImageSizeCapabilityEditor).exists()).toBe(true)
    await clickButton(wrapper, 'OAuth')
    expect(wrapper.findComponent(ImageSizeCapabilityEditor).exists()).toBe(false)
    await clickButton(wrapper, 'API Key')
    await clickButton(wrapper, 'Gemini')
    expect(wrapper.findComponent(ImageSizeCapabilityEditor).exists()).toBe(false)
  })

  it('创建请求保存 2K / 4K 规则，同时保留其它 OpenAI extra 配置', async () => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await fillCredentials(wrapper)
    await addRule(wrapper, '2K', 'vendor/image-medium')
    await addRule(wrapper, '4K', 'vendor/image-large')
    await submit(wrapper)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0][0]).toMatchObject({
      platform: 'openai',
      type: 'apikey',
      credentials: { api_key: 'sk-image-provider' },
      extra: {
        image_size_capabilities: {
          'gpt-image-2': { '2k': 'vendor/image-medium', '4k': 'vendor/image-large' }
        },
        openai_apikey_responses_websockets_v2_enabled: false
      }
    })
  })

  it('未配置规则时不写入尺寸映射字段', async () => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0][0].extra ?? {}).not.toHaveProperty('image_size_capabilities')
  })

  it('相同模型与档位重复时阻止创建并标明两条规则', async () => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await fillCredentials(wrapper)
    await addRule(wrapper, '2K', 'provider-first')
    await addRule(wrapper, '2K', 'provider-second')
    await modelInput(rows(wrapper)[1]).setValue(' gpt-image-2 ')
    await submit(wrapper)

    expect(createAccountMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.imageSizeCapability.validationError')
    expect(rows(wrapper).map((row) => row.get('[role="alert"]').text())).toEqual([
      'admin.accounts.imageSizeCapability.duplicateRule',
      'admin.accounts.imageSizeCapability.duplicateRule'
    ])
  })

  it.each(['model', 'upstream'])('漏填 %s 时阻止创建，补全后可创建', async (field) => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await fillCredentials(wrapper)
    await addRule(wrapper, '2K', 'provider-medium')
    const input = field === 'model' ? modelInput(rows(wrapper)[0]) : upstreamInput(rows(wrapper)[0])
    await input.setValue(' ')
    await submit(wrapper)

    expect(createAccountMock).not.toHaveBeenCalled()
    expect(rows(wrapper)[0].get('[role="alert"]').text()).toBe('admin.accounts.imageSizeCapability.incompleteRule')
    await input.setValue(field === 'model' ? 'gpt-image-2' : 'provider-medium')
    expect(rows(wrapper)[0].find('[role="alert"]').exists()).toBe(false)
    await submit(wrapper)
    expect(createAccountMock).toHaveBeenCalledTimes(1)
  })

  it('关闭后重新打开会清空未保存规则与校验状态', async () => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await fillCredentials(wrapper)
    await addRule(wrapper, '2K', '')
    await submit(wrapper)
    expect(rows(wrapper)[0].find('[role="alert"]').exists()).toBe(true)

    await clickButton(wrapper, 'common.cancel')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await openAPIKeyForm(wrapper)
    expect(rows(wrapper)).toHaveLength(0)
    expect(section(wrapper).find('[role="alert"]').exists()).toBe(false)
    await fillCredentials(wrapper)
    await submit(wrapper)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0][0].extra ?? {}).not.toHaveProperty('image_size_capabilities')
  })

  it('切换到其它平台时不验证或提交 OpenAI 尺寸规则', async () => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await fillCredentials(wrapper)
    await addRule(wrapper, '4K', '')
    await clickButton(wrapper, 'Gemini')
    await submit(wrapper)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0][0].platform).toBe('gemini')
    expect(createAccountMock.mock.calls[0][0].extra ?? {}).not.toHaveProperty('image_size_capabilities')
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('切换平台后返回 OpenAI 时不会恢复上一平台切换前的规则', async () => {
    const wrapper = mountModal()
    await openAPIKeyForm(wrapper)
    await addRule(wrapper, '2K', 'provider-medium')
    await clickButton(wrapper, 'Gemini')
    await clickButton(wrapper, 'OpenAI')

    expect(rows(wrapper)).toHaveLength(0)
  })
})
