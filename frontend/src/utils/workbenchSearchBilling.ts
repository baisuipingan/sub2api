import { getLocale } from '@/i18n'
import type { ChatSearchCharge } from '@/types/workbenchChat'

export function formatSearchCharge(charge: ChatSearchCharge, currency = charge.billing_currency) {
  const useCNY = currency === 'CNY' && charge.usd_to_cny_rate > 0
  return new Intl.NumberFormat(getLocale(), {
    style: 'currency',
    currency: useCNY ? 'CNY' : 'USD',
    currencyDisplay: 'code',
    maximumFractionDigits: 10,
  }).format(useCNY ? charge.amount_usd * charge.usd_to_cny_rate : charge.amount_usd)
}
