import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

import PlanEditDialog from '../PlanEditDialog.vue'
import type { AdminGroup } from '@/types'
import type { RechargeCurrency, SubscriptionPlan } from '@/types/payment'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'payment.admin.subscriptionPaymentPreview') return `preview ${params?.total}`
      if (key === 'payment.admin.subscriptionPaymentPreviewWithFee') return `fee ${params?.feeRate}`
      return key
    },
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
  },
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: Boolean,
    title: String,
    width: String,
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: [String, Number],
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: String,
  },
  emits: ['update:modelValue'],
  setup(_props, { emit }) {
    const onChange = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      emit('update:modelValue', value === '' ? null : Number(value))
    }
    return { onChange }
  },
  template: `
    <select
      :value="modelValue ?? ''"
      @change="onChange"
    >
      <option value="">{{ placeholder }}</option>
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :data-platform="option.platform"
      >
        {{ option.label }}
      </option>
    </select>
  `,
})

const groupFixture = (overrides: Partial<AdminGroup>): AdminGroup => ({
  id: 1,
  name: 'OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'subscription',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: false,
  sort_order: 0,
  ...overrides,
})

function mountDialog({
  groups = [],
  paymentConfig = null,
  plan = null,
  platformCurrency = 'CNY',
}: {
  groups?: AdminGroup[]
  paymentConfig?: Record<string, unknown> | null
  plan?: SubscriptionPlan | null
  platformCurrency?: RechargeCurrency
} = {}) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan,
      groups,
      paymentConfig,
      platformCurrency,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog', () => {
  it('defaults the currency to the site and previews the actual fee-inclusive charge', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 7.15,
        recharge_fee_rate: 2.5,
      },
    })

    expect(wrapper.get<HTMLSelectElement>('#plan-currency').element.value).toBe('CNY')
    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview ¥10.24')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).not.toContain('¥73.22')

    await wrapper.get('#plan-currency').setValue('USD')
    expect(wrapper.text()).toContain('preview $10.24')
  })

  it('requires choosing the currency for a historical plan with an unknown price currency', async () => {
    const wrapper = mountDialog({
      paymentConfig: { recharge_fee_rate: 0 },
      plan: {
        id: 1,
        group_id: 1,
        name: 'Legacy',
        description: '',
        price: 12.99,
        currency: 'NZD',
        validity_days: 30,
        validity_unit: 'days',
        features: [],
        for_sale: true,
        sort_order: 0,
      },
    })
    expect(wrapper.get<HTMLSelectElement>('#plan-currency').element.value).toBe('')
    expect(wrapper.text()).toContain('payment.admin.confirmLegacyPlanCurrency')
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).not.toContain('preview')

    await wrapper.get('#plan-currency').setValue('USD')
    expect(wrapper.text()).toContain('preview $12.99')
  })

  it('allows composite subscription groups for payment plans', () => {
    const wrapper = mountDialog({
      groups: [
        groupFixture({
          id: 10,
          name: 'OpenAI + Claude + Gemini + Grok',
          platform: 'composite',
          rate_multiplier: 1.2,
          subscription_type: 'subscription',
        }),
        groupFixture({
          id: 11,
          name: 'Standard OpenAI',
          platform: 'openai',
          subscription_type: 'standard',
        }),
      ],
    })

    const options = wrapper.findAll('option').map(option => option.text())

    expect(options).toContain('OpenAI + Claude + Gemini + Grok — composite (1.2x)')
    expect(options).not.toContain('Standard OpenAI — openai (1x)')
  })
})
