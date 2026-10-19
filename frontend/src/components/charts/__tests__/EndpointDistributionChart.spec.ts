import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import EndpointDistributionChart from '../EndpointDistributionChart.vue'
import { setCurrentBillingCurrencySettings } from '@/utils/billingCurrency'

const messages: Record<string, string> = {
  'usage.endpointDistribution': 'Endpoint Distribution',
  'usage.endpoint': 'Endpoint',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

const endpointStats = [
  { endpoint: '/v1/responses', requests: 8, total_tokens: 1000, actual_cost: 0.2, cost: 0.4 },
  { endpoint: '/v1/messages', requests: 3, total_tokens: 500, actual_cost: 1.4, cost: 2 },
]

describe('EndpointDistributionChart', () => {
  it('renders actual and standard cost rows in CNY after USD conversion', () => {
    const previousConfig = window.__APP_CONFIG__
    window.__APP_CONFIG__ = {
      ...(previousConfig ?? {}),
      billing_display_currency: 'CNY',
      billing_usd_to_cny: 7.2,
    } as any
    setCurrentBillingCurrencySettings(window.__APP_CONFIG__)

    try {
      const wrapper = mount(EndpointDistributionChart, {
        props: { endpointStats, metric: 'actual_cost', enableBreakdown: false },
        global: { stubs: { LoadingSpinner: true } },
      })

      expect(wrapper.findAll('tbody tr')[0].text()).toContain('¥10.08')
      expect(wrapper.findAll('tbody tr')[0].text()).toContain('¥14.40')
      expect(wrapper.text()).not.toContain('$1.40')
    } finally {
      window.__APP_CONFIG__ = previousConfig
      setCurrentBillingCurrencySettings(previousConfig)
    }
  })
})
