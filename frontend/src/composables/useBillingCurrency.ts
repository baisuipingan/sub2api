import { computed } from 'vue'
import {
  billingCurrencySymbol,
  creditsToDisplayAmount,
  displayToUSD,
  formatBillingAmount,
  formatBillingCredits,
  formatCompactBillingAmount,
  getCurrentBillingCurrencyView,
  usdToDisplayAmount,
} from '@/utils/billingCurrency'

export function useBillingCurrency() {
  const view = computed(getCurrentBillingCurrencyView)

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
    formatCompact: (amountUSD: number) =>
      formatCompactBillingAmount(amountUSD, view.value),
  }
}
