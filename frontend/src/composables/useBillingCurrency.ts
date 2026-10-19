import { computed } from 'vue'
import { getActivePinia } from 'pinia'
import { useAppStore } from '@/stores'
import {
  billingCurrencySymbol,
  creditsToDisplayAmount,
  displayToUSD,
  formatBillingAmount,
  formatBillingCredits,
  resolveBillingCurrencyView,
  usdToDisplayAmount,
} from '@/utils/billingCurrency'

export function useBillingCurrency() {
  const appStore = getActivePinia() ? useAppStore() : null
  const view = computed(() => {
    const settings =
      appStore?.cachedPublicSettings ??
      (typeof window !== 'undefined' ? window.__APP_CONFIG__ : null)
    return resolveBillingCurrencyView(settings)
  })

  return {
    view,
    symbol: computed(() => billingCurrencySymbol(view.value.displayCurrency)),
    usdToDisplay: (amountUSD: number) => usdToDisplayAmount(amountUSD, view.value),
    creditsToDisplay: (credits: number) => creditsToDisplayAmount(credits, view.value),
    displayToUSD: (amount: number) => displayToUSD(amount, view.value),
    formatUSD: (amountUSD: number, fractionDigits = 2) =>
      formatBillingAmount(amountUSD, view.value, fractionDigits),
    formatCredits: (credits: number, fractionDigits = 2) =>
      formatBillingCredits(credits, view.value, fractionDigits),
  }
}
