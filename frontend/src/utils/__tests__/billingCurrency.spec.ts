import { describe, expect, it } from 'vitest'
import {
  creditsToDisplayAmount,
  displayToUSD,
  formatBillingAmount,
  formatCompactBillingAmount,
  resolveBillingCurrencyView,
  usdToDisplayAmount,
} from '../billingCurrency'

describe('billing currency display', () => {
  it('defaults to the legacy USD view when public settings are absent', () => {
    const view = resolveBillingCurrencyView(null)
    expect(view.engine).toBe('legacy')
    expect(view.displayCurrency).toBe('USD')
    expect(usdToDisplayAmount(1.5, view)).toBe(1.5)
  })

  it('converts USD-equivalent and credit amounts for CNY display', () => {
    const view = resolveBillingCurrencyView({
      billing_currency_enabled: true,
      billing_engine: 'credits',
      billing_display_currency: 'CNY',
      billing_usd_to_cny: 7.2,
      billing_credits_per_usd: 100_000_000,
    })
    expect(usdToDisplayAmount(0.24, view)).toBeCloseTo(1.728, 10)
    expect(creditsToDisplayAmount(24_000_000, view)).toBeCloseTo(1.728, 10)
    expect(displayToUSD(1.728, view)).toBeCloseTo(0.24, 10)
    expect(formatBillingAmount(0.24, view, 2)).toContain('1.73')
  })

  it('formats compact costs after converting from USD to CNY', () => {
    const view = resolveBillingCurrencyView({
      billing_display_currency: 'CNY',
      billing_usd_to_cny: 7.2,
    })

    expect(formatCompactBillingAmount(0.1, view)).toContain('0.72')
    expect(formatCompactBillingAmount(200, view)).toMatch(/^¥1\.44K$/)
  })

  it('keeps recharge and display conversion independent', () => {
    const view = resolveBillingCurrencyView({
      billing_display_currency: 'CNY',
      billing_usd_to_cny: 7.5,
    })
    expect(usdToDisplayAmount(1, view)).toBe(7.5)
  })
})
