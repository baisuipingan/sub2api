import type { PaymentOrder, RechargeCurrency } from '@/types/payment'

export const DEFAULT_PAYMENT_CURRENCY = 'CNY'

const PAYMENT_CURRENCY_SYMBOLS: Record<string, string> = {
  USD: '$',
  CNY: '¥',
  RMB: '¥',
  EUR: '€',
  GBP: '£',
  JPY: '¥',
  HKD: 'HK$',
  TWD: 'NT$',
  KRW: '₩',
  AUD: 'A$',
  CAD: 'C$',
  SGD: 'S$',
  NZD: 'NZ$',
  MOP: 'MOP$',
  MYR: 'RM',
  THB: '฿',
  PHP: '₱',
  INR: '₹',
}

export function normalizePaymentCurrency(currency?: string | null): string {
  const normalized = String(currency || '').trim().toUpperCase()
  return /^[A-Z]{3}$/.test(normalized) ? normalized : DEFAULT_PAYMENT_CURRENCY
}

export function currencySymbol(currency?: string | null): string {
  const normalized = normalizePaymentCurrency(currency)
  return PAYMENT_CURRENCY_SYMBOLS[normalized] || normalized
}

function paymentCurrencyFractionDigits(currency: string): number {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
    }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

export function formatPaymentAmount(amount: number, currency?: string | null, locale?: string): string {
  const normalized = normalizePaymentCurrency(currency)
  const fractionDigits = paymentCurrencyFractionDigits(normalized)
  try {
    return new Intl.NumberFormat(locale || undefined, {
      style: 'currency',
      currency: normalized,
      currencyDisplay: 'narrowSymbol',
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(Number.isFinite(amount) ? amount : 0)
  } catch {
    return `${normalized} ${(Number.isFinite(amount) ? amount : 0).toFixed(fractionDigits)}`
  }
}

export function formatOrderCredit(order: PaymentOrder, locale?: string): string {
  return formatPaymentAmount(Number(order.credited_display_amount ?? order.amount), order.credited_display_currency || 'USD', locale)
}

export function formatOrderBonus(order: PaymentOrder, locale?: string): string {
  return formatPaymentAmount(Number(order.bonus_display_amount ?? order.bonus_amount ?? 0), order.credited_display_currency || 'USD', locale)
}

/** Quoted credit is shown only while a balance order can still be fulfilled. */
export function showOrderCredit(order: PaymentOrder): boolean {
  return order.order_type === 'balance' && (!!order.credited_at
    || order.status === 'PENDING' || order.status === 'PAID' || order.status === 'RECHARGING')
}

export function isSitePaymentCurrency(currency: unknown): currency is RechargeCurrency {
  return currency === 'USD' || currency === 'CNY'
}

/** USD and CNY use cents. The stored percentage rate has at most two decimals. */
export function fiatPaymentTotal(amount: number, feeRate: number): { fee: number; total: number } {
  const amountCents = BigInt(Math.round(amount * 100))
  const feeBasisPoints = BigInt(Math.round(Math.max(0, feeRate) * 100))
  const feeCents = (amountCents * feeBasisPoints + 9999n) / 10000n
  return { fee: Number(feeCents) / 100, total: Number(amountCents + feeCents) / 100 }
}
