import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import PaymentView from '../PaymentView.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import { formatPaymentAmount } from '@/components/payment/currency'
import { setCurrentBillingCurrencySettings } from '@/utils/billingCurrency'
import AmountInput from '@/components/payment/AmountInput.vue'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'
import type { CheckoutInfoResponse, MethodLimit, RechargeQuote, SubscriptionPlan } from '@/types/payment'

const routeState = vi.hoisted(() => ({
  path: '/purchase',
  query: {} as Record<string, unknown>,
}))

const routerReplace = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())
const routerResolve = vi.hoisted(() => vi.fn(() => ({ href: '/payment/stripe?mock=1' })))
const createOrder = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())
const fetchActiveSubscriptions = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
const showError = vi.hoisted(() => vi.fn())
const showInfo = vi.hoisted(() => vi.fn())
const showWarning = vi.hoisted(() => vi.fn())
const getCheckoutInfo = vi.hoisted(() => vi.fn())
const getRechargeQuote = vi.hoisted(() => vi.fn())
const bridgeInvoke = vi.hoisted(() => vi.fn())
const translate = vi.hoisted(() => vi.fn((key: string) => key))
// Public settings live in a reactive holder so tests can flip feature flags after mount
// and exercise the watchers that react to them.
const appStoreState = vi.hoisted(() => ({
  setPublicSettings: (_value: Record<string, unknown> | undefined) => {},
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({
      replace: routerReplace,
      push: routerPush,
      resolve: routerResolve,
    }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: translate,
    }),
  }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: {
      username: 'demo-user',
      balance: 0,
    },
    refreshUser,
  }),
}))

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({
    createOrder,
  }),
}))

vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({
    activeSubscriptions: [],
    fetchActiveSubscriptions,
  }),
}))

vi.mock('@/stores', async () => {
  const { reactive } = await import('vue')
  const state = reactive({ cachedPublicSettings: undefined as Record<string, unknown> | undefined })
  appStoreState.setPublicSettings = (value) => {
    state.cachedPublicSettings = value
  }
  return {
    useAppStore: () => ({
      showError,
      showInfo,
      showWarning,
      get cachedPublicSettings() {
        return state.cachedPublicSettings
      },
    }),
  }
})

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getCheckoutInfo,
    getRechargeQuote,
  },
}))

vi.mock('@/utils/device', () => ({
  isMobileDevice: () => true,
}))

function checkoutInfoFixture(overrides: Partial<CheckoutInfoResponse> = {}) {
  const wxpayMethod: MethodLimit = {
    daily_limit: 0,
    daily_used: 0,
    daily_remaining: 0,
    single_min: 0,
    single_max: 0,
    fee_rate: 0,
  }
  const data: CheckoutInfoResponse = {
    methods: {
      wxpay: wxpayMethod,
    },
    recharge_methods: { wxpay: wxpayMethod },
    subscription_methods: { wxpay: wxpayMethod },
    platform_currency: 'CNY',
    platform_usd_to_cny: 6.75,
    billing_version: 11,
    recharge_min_amount: 0,
    recharge_max_amount: 0,
    global_min: 0,
    global_max: 0,
    plans: [],
    balance_disabled: false,
    balance_recharge_multiplier: 1,
    subscription_usd_to_cny_rate: 0,
    recharge_fee_rate: 0,
    help_text: '',
    help_image_url: '',
    stripe_publishable_key: '',
  }

  return {
    data: {
      ...data,
      ...overrides,
      recharge_methods: overrides.recharge_methods ?? overrides.methods ?? data.recharge_methods,
      subscription_methods: overrides.subscription_methods ?? overrides.methods ?? data.subscription_methods,
    },
  }
}

function checkoutInfoWithPlansFixture(options: {
  checkout?: Partial<CheckoutInfoResponse>
  method?: Partial<MethodLimit>
  plan?: Partial<SubscriptionPlan>
} = {}) {
  const base = checkoutInfoFixture(options.checkout).data
  const plan: SubscriptionPlan = {
    id: 7,
    group_id: 3,
    name: 'Starter',
    description: '',
    price: 128,
    currency: base.platform_currency,
    original_price: 0,
    validity_days: 30,
    validity_unit: 'day',
    rate_multiplier: 1,
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    features: [],
    group_platform: 'openai',
    sort_order: 1,
    for_sale: true,
    group_name: 'OpenAI',
    ...options.plan,
  }

  return {
    data: {
      ...base,
      subscription_methods: {
        ...base.subscription_methods,
        wxpay: {
          ...base.subscription_methods.wxpay,
          ...options.method,
        },
      },
      plans: [plan],
    },
  }
}

function jsapiOrderFixture(resumeToken: string) {
  return {
    order_id: 123,
    amount: 88,
    pay_amount: 88,
    fee_rate: 0,
    expires_at: '2099-01-01T00:10:00.000Z',
    payment_type: 'wxpay',
    out_trade_no: 'sub2_jsapi_123',
    result_type: 'jsapi_ready' as const,
    resume_token: resumeToken,
    jsapi: {
      appId: 'wx123',
      timeStamp: '1712345678',
      nonceStr: 'nonce',
      package: 'prepay_id=wx123',
      signType: 'RSA',
      paySign: 'signed',
    },
  }
}

function oauthOrderFixture() {
  return {
    order_id: 456,
    amount: 128,
    pay_amount: 128,
    fee_rate: 0,
    expires_at: '2099-01-01T00:10:00.000Z',
    payment_type: 'wxpay',
    result_type: 'oauth_required' as const,
    oauth: {
      authorize_url: '/api/v1/auth/oauth/wechat/payment/start?payment_type=wxpay&redirect=%2Fpurchase%3Ffrom%3Dwechat',
      appid: 'wx123',
      scope: 'snsapi_base',
      redirect_url: '/auth/wechat/payment/callback',
    },
  }
}

async function mountSubscriptionConfirm(options: Parameters<typeof checkoutInfoWithPlansFixture>[0] = {}) {
  vi.useRealTimers()
  routeState.path = '/purchase'
  routeState.query = {
    tab: 'subscription',
    group: '3',
  }
  routerReplace.mockReset().mockResolvedValue(undefined)
  routerPush.mockReset().mockResolvedValue(undefined)
  routerResolve.mockClear()
  createOrder.mockReset()
  refreshUser.mockReset()
  fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
  showError.mockReset()
  showInfo.mockReset()
  showWarning.mockReset()
  getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoWithPlansFixture(options))
  bridgeInvoke.mockReset()
  window.localStorage.clear()
  ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined

  const wrapper = shallowMount(PaymentView, {
    global: {
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
        Teleport: true,
        Transition: false,
      },
    },
  })
  await flushPromises()
  await flushPromises()
  return wrapper
}

async function mountSubscriptionPlanList(planCount: number) {
  vi.useRealTimers()
  routeState.path = '/purchase'
  routeState.query = { tab: 'subscription' }
  routerReplace.mockReset().mockResolvedValue(undefined)
  routerPush.mockReset().mockResolvedValue(undefined)
  routerResolve.mockClear()
  createOrder.mockReset()
  refreshUser.mockReset()
  fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
  showError.mockReset()
  showInfo.mockReset()
  showWarning.mockReset()
  const basePlan = checkoutInfoWithPlansFixture().data.plans[0]
  const plans = Array.from({ length: planCount }, (_, index) => ({
    ...basePlan,
    id: index + 1,
    name: `Plan ${index + 1}`,
  }))
  getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture({ plans }))
  bridgeInvoke.mockReset()
  window.localStorage.clear()
  ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined

  const wrapper = shallowMount(PaymentView, {
    global: {
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
        Teleport: true,
        Transition: false,
      },
    },
  })
  await flushPromises()
  await flushPromises()
  return wrapper
}

describe('PaymentView help text', () => {
  beforeEach(() => {
    vi.useRealTimers()
    routeState.path = '/purchase'
    routeState.query = {}
    createOrder.mockReset()
    window.localStorage.clear()
  })

  async function mountHelp(help_text: string, help_image_url = '') {
    getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture({ help_text, help_image_url }))
    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    return wrapper
  }

  it('renders headings, emphasis, links, and lists in payment help without starting checkout', async () => {
    const wrapper = await mountHelp('## Recharge help\n\n**Read first**\n\n- [Contact support](https://example.com/help)')
    const help = wrapper.get('.markdown-body')
    expect(help.get('h2').text()).toBe('Recharge help')
    expect(help.get('strong').text()).toBe('Read first')
    expect(help.get('li a').attributes('href')).toBe('https://example.com/help')
    expect(createOrder).not.toHaveBeenCalled()
  })

  it('removes scripts, event handlers, and unsafe URLs from rendered help', async () => {
    const wrapper = await mountHelp([
      '<script>alert(1)</script>',
      '<img src="https://example.com/help.png" onerror="alert(1)">',
      '[Unsafe](javascript:alert%281%29)',
      '[Support](https://example.com/help)',
    ].join('\n\n'))
    const help = wrapper.get('.markdown-body')
    expect(help.find('script').exists()).toBe(false)
    expect(help.get('img').attributes('onerror')).toBeUndefined()
    expect(help.findAll('a').map(link => link.attributes('href'))).toEqual([undefined, 'https://example.com/help'])
  })

  it('keeps plain-text soft line breaks and the separate help image preview', async () => {
    const wrapper = await mountHelp('First line\nSecond line', 'https://example.com/help.png')
    const help = wrapper.get('.markdown-body')
    expect(help.get('p').text()).toBe('First line\nSecond line')
    expect(help.find('br').exists()).toBe(false)
    await wrapper.get('img').trigger('click')
    expect(wrapper.findAll('img')).toHaveLength(2)
    expect(wrapper.findAll('img')[1].attributes('src')).toBe('https://example.com/help.png')
  })

  it('keeps image-only help without an empty Markdown container', async () => {
    const wrapper = await mountHelp('', 'https://example.com/help.png')
    expect(wrapper.find('.markdown-body').exists()).toBe(false)
    expect(wrapper.get('img').attributes('src')).toBe('https://example.com/help.png')
  })
})

describe('PaymentView subscription plan grid', () => {
  it.each([3, 4, 6])('keeps %i plans on the existing mobile/tablet/desktop grid', async (planCount) => {
    const wrapper = await mountSubscriptionPlanList(planCount)
    const cards = wrapper.findAllComponents(SubscriptionPlanCard)

    expect(cards).toHaveLength(planCount)
    expect([...(cards[0].element.parentElement?.classList ?? [])]).toEqual(expect.arrayContaining([
      'grid',
      'grid-cols-1',
      'sm:grid-cols-2',
      'lg:grid-cols-3',
    ]))
  })
})

describe('PaymentView platform recharge quote', () => {
  beforeEach(() => {
    routeState.path = '/purchase'
    routeState.query = {}
    window.localStorage.clear()
    getRechargeQuote.mockReset()
    createOrder.mockReset()
    showWarning.mockReset()
    setCurrentBillingCurrencySettings({ billing_display_currency: 'USD', billing_usd_to_cny: 7.2 })
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    setCurrentBillingCurrencySettings()
  })

  const availableMethod = { ...checkoutInfoFixture().data.methods.wxpay, currency: 'CNY' }

  function quoteFixture(overrides: Partial<RechargeQuote> = {}): RechargeQuote {
    return {
      amount: 10, currency: 'CNY', min_amount: 1, max_amount: 1000,
      pay_amount: 10.25, fee_amount: 0.25, fee_rate: 2.5,
      credited_amount: 1.48148148, credited_credits: '148148148', credited_display_amount: 10,
      bonus_amount: 0, bonus_display_amount: 0, billing_version: 11,
      quote_id: 'quote-10', expires_at: new Date(Date.now() + 60_000).toISOString(),
      ...overrides,
    }
  }

  async function mountCheckout() {
    const wrapper = shallowMount(PaymentView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Teleport: true } },
    })
    await flushPromises()
    return wrapper
  }

  async function enterAmount(wrapper: Awaited<ReturnType<typeof mountCheckout>>, amount: number) {
    wrapper.getComponent(AmountInput).vm.$emit('update:modelValue', amount)
    await wrapper.vm.$nextTick()
    await vi.advanceTimersByTimeAsync(250)
  }

  it('follows checkout currency and submits the exact approved quote for a fiat gateway', async () => {
    getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture({
      methods: { wxpay: availableMethod, alipay: availableMethod },
      recharge_methods: { wxpay: availableMethod },
    }))
    getRechargeQuote.mockResolvedValue({ data: quoteFixture() })
    createOrder.mockResolvedValue({
      order_id: 8, amount: 1.48, pay_amount: 10.25, fee_rate: 2.5, currency: 'CNY',
      qr_code: 'test-order', expires_at: '2099-01-01T00:00:00Z',
    })
    const wrapper = await mountCheckout()
    const submitButton = () => wrapper.get('button.btn.w-full')
    expect(wrapper.getComponent(AmountInput).props('currency')).toBe('CNY')
    expect(wrapper.find('input[name="recharge-currency"]').exists()).toBe(false)
    expect(wrapper.getComponent(PaymentMethodSelector).props('methods')).toEqual([
      expect.objectContaining({ type: 'wxpay', available: true }),
    ])
    expect(submitButton().attributes('disabled')).toBeDefined()

    await enterAmount(wrapper, 10)
    expect(getRechargeQuote).toHaveBeenCalledWith(
      { amount: 10, payment_type: 'wxpay', currency: 'CNY', billing_version: 11 }, expect.any(AbortSignal),
    )
    expect(submitButton().text()).toContain(formatPaymentAmount(10.25, 'CNY'))
    expect(wrapper.get('[data-testid="recharge-credited-row"]').text()).toContain(formatPaymentAmount(10, 'CNY'))
    await submitButton().trigger('click')
    expect(createOrder).toHaveBeenCalledWith(expect.objectContaining({
      amount: 10, payment_type: 'wxpay', currency: 'CNY', order_type: 'balance', quote_id: 'quote-10',
    }))
    wrapper.unmount()
  })

  it('ignores a quote that arrives after the amount changed', async () => {
    getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture({
      platform_currency: 'USD',
      recharge_methods: { usdt_bsc: availableMethod },
    }))
    let resolveOldQuote: (value: { data: RechargeQuote }) => void = () => {}
    getRechargeQuote.mockImplementationOnce(() => new Promise(resolve => { resolveOldQuote = resolve }))
      .mockResolvedValueOnce({ data: quoteFixture({
        currency: 'USD', amount: 20, pay_amount: 20.50, crypto_amount: '20.50', fee_amount: 0.50, credited_display_amount: 20, quote_id: 'quote-20',
      }) })
    const wrapper = await mountCheckout()
    const submitButton = () => wrapper.get('button.btn.w-full')

    await enterAmount(wrapper, 10)
    expect(submitButton().attributes('disabled')).toBeDefined()
    await enterAmount(wrapper, 20)
    expect(getRechargeQuote.mock.calls[0][1].aborted).toBe(true)
    resolveOldQuote({ data: quoteFixture({ currency: 'USD' }) })
    await flushPromises()

    expect(wrapper.getComponent(AmountInput).props('currency')).toBe('USD')
    expect(submitButton().text()).toContain('$20.50')
    expect(wrapper.get('[data-testid="recharge-crypto-due"]').text()).toContain('20.50 USDT')
    expect(wrapper.get('[data-testid="recharge-credited-row"]').text()).toContain('$20.00')
    wrapper.unmount()
  })

  it('keeps a busy USDT payment blocked until the user refreshes and confirms a fresh quote', async () => {
    getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture({
      platform_currency: 'USD',
      recharge_methods: { usdt_bsc: availableMethod },
    }))
    const initialQuote = quoteFixture({
      currency: 'USD',
      crypto_amount: '10.25',
    })
    getRechargeQuote.mockResolvedValueOnce({ data: initialQuote })
    createOrder.mockRejectedValueOnce({ reason: 'USDT_AMOUNT_BUSY' })
    const wrapper = await mountCheckout()
    const submitButton = () => wrapper.get('button.btn.w-full')

    await enterAmount(wrapper, 10)
    await submitButton().trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toBe('payment.errors.USDT_AMOUNT_BUSY')
    expect(submitButton().attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(wrapper.get('[role="alert"]').text()).toBe('payment.errors.USDT_AMOUNT_BUSY')
    expect(getRechargeQuote).toHaveBeenCalledTimes(1)

    getRechargeQuote.mockResolvedValueOnce({ data: quoteFixture({
      currency: 'USD',
      crypto_amount: '10.25',
      quote_id: 'fresh-quote',
    }) })
    const refreshButton = wrapper.findAll('button').find(button => button.text() === 'payment.refreshRechargeQuote')
    expect(refreshButton).toBeDefined()
    await refreshButton!.trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(250)

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(submitButton().attributes('disabled')).toBeUndefined()
    expect(createOrder).toHaveBeenCalledTimes(1)

    createOrder.mockResolvedValueOnce({
      order_id: 8, amount: 10, pay_amount: 10.25, fee_rate: 2.5, currency: 'USD',
      qr_code: 'test-order', expires_at: '2099-01-01T00:00:00Z',
    })
    await submitButton().trigger('click')
    expect(createOrder).toHaveBeenLastCalledWith(expect.objectContaining({ quote_id: 'fresh-quote' }))
    wrapper.unmount()
  })

  it('clears stale input when the platform currency changed before submitting', async () => {
    getCheckoutInfo.mockReset()
      .mockResolvedValueOnce(checkoutInfoFixture())
      .mockResolvedValueOnce(checkoutInfoFixture({ platform_currency: 'USD', billing_version: 12, recharge_methods: { usdt_bsc: availableMethod } }))
    getRechargeQuote.mockResolvedValue({ data: quoteFixture() })
    createOrder.mockRejectedValue({ reason: 'RECHARGE_QUOTE_CHANGED' })
    const wrapper = await mountCheckout()

    await enterAmount(wrapper, 10)
    await wrapper.get('button.btn.w-full').trigger('click')
    await flushPromises()

    expect(getCheckoutInfo).toHaveBeenCalledTimes(2)
    expect(createOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.getComponent(AmountInput).props('currency')).toBe('USD')
    expect(wrapper.getComponent(AmountInput).props('modelValue')).toBeNull()
    expect(wrapper.get('button.btn.w-full').attributes('disabled')).toBeDefined()
    expect(showWarning).toHaveBeenCalledWith('payment.rechargeQuoteStale')
    wrapper.unmount()
  })
})

describe('PaymentView subscription confirmation amounts', () => {
  it('uses the published CNY price for display and creation even with a legacy exchange rate', async () => {
    const wrapper = await mountSubscriptionConfirm({
      checkout: {
        balance_recharge_multiplier: 0.14,
        subscription_usd_to_cny_rate: 7.15,
        recharge_fee_rate: 2.5,
      },
      method: { currency: 'CNY' },
      plan: { price: 9.99, original_price: 12.99 },
    })
    createOrder.mockResolvedValue({ ...oauthOrderFixture(), result_type: 'order_created', qr_code: 'test-qr' })
    const paymentPrice = formatPaymentAmount(9.99, 'CNY')
    expect(wrapper.text()).toContain(paymentPrice)
    expect(wrapper.text()).toContain(formatPaymentAmount(12.99, 'CNY'))
    expect(wrapper.text()).not.toContain(formatPaymentAmount(71.43, 'CNY'))
    await wrapper.get('button.btn.w-full').trigger('click')
    expect(createOrder).toHaveBeenCalledWith(expect.objectContaining({
      amount: 9.99, currency: 'CNY', order_type: 'subscription', plan_id: 7,
      billing_version: 11, expected_pay_amount: 10.24,
    }))
  })

  it('reloads a changed plan price without placing a new order', async () => {
    const wrapper = await mountSubscriptionConfirm({ plan: { price: 7.99 } })
    getCheckoutInfo.mockResolvedValueOnce(checkoutInfoWithPlansFixture({ plan: { price: 8.99 } }))
    createOrder.mockRejectedValueOnce({ reason: 'SUBSCRIPTION_PRICE_CHANGED' })
    await wrapper.get('button.btn.w-full').trigger('click')
    await flushPromises()

    expect(createOrder).toHaveBeenCalledTimes(1)
    expect(showWarning).toHaveBeenCalledWith('payment.subscriptionPriceChanged')
    expect(wrapper.get('button.btn.w-full').text()).toContain(formatPaymentAmount(8.99, 'CNY'))
  })

  it('shows the published USD price even when the app has a different cached currency', async () => {
    setCurrentBillingCurrencySettings({ billing_display_currency: 'CNY' })
    const wrapper = await mountSubscriptionConfirm({
      checkout: { platform_currency: 'USD' },
      method: { currency: 'USD' },
      plan: { price: 7.99, original_price: 9.99 },
    })

    expect(wrapper.text()).toContain(formatPaymentAmount(7.99, 'USD'))
    expect(wrapper.text()).toContain(formatPaymentAmount(9.99, 'USD'))
    setCurrentBillingCurrencySettings()
  })

  it('adds the configured fee to the published price', async () => {
    const wrapper = await mountSubscriptionConfirm({
      checkout: { recharge_fee_rate: 2.5 },
      method: { currency: 'CNY' },
      plan: { price: 9.99 },
    })

    const text = wrapper.text()
    const price = formatPaymentAmount(9.99, 'CNY')
    const fee = formatPaymentAmount(0.25, 'CNY')
    const total = formatPaymentAmount(10.24, 'CNY')

    expect(text).toContain(price)
    expect(text).toContain(fee)
    expect(text).toContain(total)
    expect(wrapper.findAll('button').some(button => button.text().includes(total))).toBe(true)
  })
})

describe('PaymentView payment recovery', () => {
  beforeEach(() => {
    vi.useRealTimers()
    routeState.path = '/purchase'
    routeState.query = {}
    routerReplace.mockReset().mockResolvedValue(undefined)
    routerPush.mockReset().mockResolvedValue(undefined)
    routerResolve.mockClear()
    createOrder.mockReset()
    refreshUser.mockReset()
    fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
    showError.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    bridgeInvoke.mockReset()
    window.localStorage.clear()
    ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined
  })

  it('restores a custom EasyPay method as the selected payment method', async () => {
    getCheckoutInfo.mockResolvedValue(checkoutInfoFixture({
      methods: {
        wxpay: checkoutInfoFixture().data.methods.wxpay,
        ldc: {
          daily_limit: 0,
          daily_used: 0,
          daily_remaining: 0,
          single_min: 0,
          single_max: 0,
          fee_rate: 0,
          available: true,
          display_name: 'LDC Pay',
        },
      },
    }))
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 888,
      amount: 66,
      qrCode: 'ldc-qr',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'ldc',
      payUrl: 'https://pay.example.com/ldc',
      outTradeNo: 'sub2_ldc_888',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 66,
      orderType: 'balance',
      paymentMode: 'popup',
      resumeToken: '',
      createdAt: Date.now(),
    }))

    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
          PaymentStatusPanel: {
            template: '<button data-test="payment-done" @click="$emit(\'done\')" />',
          },
          PaymentMethodSelector: {
            props: ['selected'],
            template: '<div data-test="method-selector">{{ selected }}</div>',
          },
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()
    await wrapper.find('[data-test="payment-done"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="method-selector"]').text()).toBe('ldc')
  })
})

describe('PaymentView WeChat JSAPI flow', () => {
  beforeEach(() => {
    routeState.path = '/purchase'
    routeState.query = {
      wechat_resume: '1',
      wechat_resume_token: 'resume-token-123',
    }
    routerReplace.mockReset().mockResolvedValue(undefined)
    routerPush.mockReset().mockResolvedValue(undefined)
    routerResolve.mockClear()
    createOrder.mockReset()
    refreshUser.mockReset()
    fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
    showError.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture())
    bridgeInvoke.mockReset()
    window.localStorage.clear()
    ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = {
      invoke: bridgeInvoke,
    }
  })

  it('resets payment state and redirects to /payment/result after JSAPI reports success', async () => {
    createOrder.mockResolvedValue(jsapiOrderFixture('resume-token-123'))
    bridgeInvoke.mockImplementation((_action, _payload, callback) => {
      callback({ err_msg: 'get_brand_wcpay_request:ok' })
    })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(routerReplace).toHaveBeenCalledWith({ path: '/purchase', query: {} })
    expect(routerPush).toHaveBeenCalledWith({
      path: '/payment/result',
      query: {
        order_id: '123',
        out_trade_no: 'sub2_jsapi_123',
        resume_token: 'resume-token-123',
      },
    })
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('resets payment state when JSAPI reports cancellation', async () => {
    createOrder.mockResolvedValue(jsapiOrderFixture('resume-token-cancel'))
    bridgeInvoke.mockImplementation((_action, _payload, callback) => {
      callback({ err_msg: 'get_brand_wcpay_request:cancel' })
    })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(showInfo).toHaveBeenCalledWith('payment.qr.cancelled')
    expect(routerPush).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('clears stale recovery state when JSAPI never becomes available', async () => {
    vi.useFakeTimers()
    createOrder.mockResolvedValue(jsapiOrderFixture('resume-token-missing-bridge'))
    ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined

    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    await flushPromises()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      'payment.errors.wechatJsapiUnavailable payment.errors.wechatOpenInWeChatHint',
    )
    expect(routerPush).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    expect(wrapper.html()).not.toContain('payment-status-panel-stub')
  })

  it('clears a stale recovery snapshot before handling wechat resume callback params', async () => {
    createOrder.mockRejectedValueOnce(new Error('resume failed'))
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 999,
      amount: 66,
      qrCode: 'stale-qr',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'alipay',
      payUrl: 'https://pay.example.com/stale',
      outTradeNo: 'stale-out-trade-no',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 66,
      orderType: 'balance',
      paymentMode: 'popup',
      resumeToken: '',
      createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    }))

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(createOrder).toHaveBeenCalledWith(expect.objectContaining({
      wechat_resume_token: 'resume-token-123',
    }))
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('keeps subscription resume context for token-only WeChat callbacks', async () => {
    routeState.query = {
      wechat_resume: '1',
      wechat_resume_token: 'resume-subscription-7',
      payment_type: 'wxpay_direct',
      order_type: 'subscription',
      plan_id: '7',
    }
    getCheckoutInfo.mockResolvedValue(checkoutInfoWithPlansFixture())
    createOrder.mockResolvedValue(oauthOrderFixture())

    const originalLocation = window.location
    const locationState = {
      href: 'http://localhost/purchase',
      origin: 'http://localhost',
    }
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState,
    })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(routerReplace).toHaveBeenCalledWith({ path: '/purchase', query: {} })
    expect(createOrder).toHaveBeenCalledWith(expect.objectContaining({
      payment_type: 'wxpay',
      order_type: 'subscription',
      plan_id: 7,
      wechat_resume_token: 'resume-subscription-7',
    }))
    expect(locationState.href).toContain('/api/v1/auth/oauth/wechat/payment/start?')
    expect(new URL(locationState.href, 'http://localhost').searchParams.get('redirect')).toBe(
      '/purchase?from=wechat&payment_type=wxpay&order_type=subscription&plan_id=7',
    )

    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    })
  })

  it('falls back to QR flow when mobile WeChat payment is unavailable', async () => {
    routeState.query = {
      wechat_resume: '1',
      wechat_resume_token: 'resume-token-h5',
      payment_type: 'wxpay_direct',
    }
    createOrder
      .mockRejectedValueOnce({ reason: 'WECHAT_H5_NOT_AUTHORIZED' })
      .mockResolvedValueOnce({
        order_id: 778,
        amount: 88,
        pay_amount: 88,
        fee_rate: 0,
        expires_at: '2099-01-01T00:10:00.000Z',
        payment_type: 'wxpay',
        qr_code: 'weixin://wxpay/bizpayurl?pr=fallback-native',
        out_trade_no: 'sub2_qr_778',
      })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(createOrder).toHaveBeenNthCalledWith(1, expect.objectContaining({
      payment_type: 'wxpay',
      is_mobile: true,
      wechat_resume_token: 'resume-token-h5',
    }))
    expect(createOrder).toHaveBeenNthCalledWith(2, expect.objectContaining({
      payment_type: 'wxpay',
      is_mobile: false,
      payment_source: 'hosted_redirect',
    }))
    expect(showWarning).toHaveBeenCalledWith('payment.errors.mobilePaymentFallbackToQr')
    expect(showError).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toContain('weixin://wxpay/bizpayurl?pr=fallback-native')
  })
})

describe('PaymentView subscription feature flag', () => {
  afterEach(() => {
    appStoreState.setPublicSettings(undefined)
  })

  function tabLabels(wrapper: Awaited<ReturnType<typeof mountSubscriptionPlanList>>) {
    return wrapper
      .findAll('button')
      .map((button) => button.text())
      .filter((text) => text === 'payment.tabTopUp' || text === 'payment.tabSubscribe')
  }

  it('keeps the top-up / subscribe switcher when subscription_enabled is absent (opt-out default)', async () => {
    const wrapper = await mountSubscriptionPlanList(2)

    expect(tabLabels(wrapper)).toEqual(['payment.tabTopUp', 'payment.tabSubscribe'])
    expect(wrapper.findAllComponents(SubscriptionPlanCard)).toHaveLength(2)
  })

  it('drops the subscribe tab, hides the switcher and ignores ?tab=subscription when subscriptions are disabled', async () => {
    appStoreState.setPublicSettings({ subscription_enabled: false })
    const wrapper = await mountSubscriptionPlanList(2)

    expect(tabLabels(wrapper)).toEqual([])
    expect(wrapper.findAllComponents(SubscriptionPlanCard)).toHaveLength(0)
    expect(wrapper.text()).toContain('payment.rechargeAccount')
  })

  it('shows an unavailable notice instead of a doomed top-up form when balance recharge is disabled too', async () => {
    appStoreState.setPublicSettings({ subscription_enabled: false })
    const wrapper = await mountSubscriptionConfirm({ checkout: { balance_disabled: true } })

    expect(tabLabels(wrapper)).toEqual([])
    expect(wrapper.findAllComponents(SubscriptionPlanCard)).toHaveLength(0)
    expect(wrapper.text()).not.toContain('payment.confirmSubscription')
    expect(wrapper.text()).not.toContain('payment.rechargeAccount')
    expect(wrapper.text()).toContain('payment.billingUnavailable')
    wrapper.unmount()
  })

  it('falls back from the subscribe tab to top-up when the flag flips off after mount', async () => {
    const wrapper = await mountSubscriptionPlanList(2)
    expect(wrapper.findAllComponents(SubscriptionPlanCard)).toHaveLength(2)

    appStoreState.setPublicSettings({ subscription_enabled: false })
    await flushPromises()

    expect(tabLabels(wrapper)).toEqual([])
    expect(wrapper.findAllComponents(SubscriptionPlanCard)).toHaveLength(0)
    expect(wrapper.text()).toContain('payment.rechargeAccount')
    wrapper.unmount()
  })

  it('enters the subscribe tab when a subscription-only site turns subscriptions back on', async () => {
    appStoreState.setPublicSettings({ subscription_enabled: false })
    const wrapper = await mountSubscriptionConfirm({ checkout: { balance_disabled: true } })
    expect(wrapper.text()).toContain('payment.billingUnavailable')

    appStoreState.setPublicSettings({ subscription_enabled: true })
    await flushPromises()

    expect(wrapper.text()).not.toContain('payment.billingUnavailable')
    expect(wrapper.text()).not.toContain('payment.rechargeAccount')
    expect(wrapper.findAllComponents(SubscriptionPlanCard).length).toBeGreaterThan(0)
    wrapper.unmount()
  })
})
