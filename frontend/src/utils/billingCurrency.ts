export type BillingDisplayCurrency = "USD" | "CNY";
export type BillingEngine = "legacy" | "shadow" | "credits";

export interface BillingCurrencyView {
  enabled: boolean;
  engine: BillingEngine;
  creditsPerUSD: number;
  displayCurrency: BillingDisplayCurrency;
  usdToCny: number;
}

export interface BillingCurrencySettingsLike {
  billing_currency_enabled?: boolean | null;
  billing_engine?: string | null;
  billing_credits_per_usd?: number | null;
  billing_display_currency?: string | null;
  billing_usd_to_cny?: number | null;
}

export const DEFAULT_BILLING_CURRENCY_VIEW: BillingCurrencyView = {
  enabled: false,
  engine: "legacy",
  creditsPerUSD: 100_000_000,
  displayCurrency: "USD",
  usdToCny: 7.2,
};

export function resolveBillingCurrencyView(
  settings?: BillingCurrencySettingsLike | null,
): BillingCurrencyView {
  const enabled = settings?.billing_currency_enabled === true;
  const rawEngine = String(settings?.billing_engine || "").toLowerCase();
  const engine: BillingEngine =
    rawEngine === "shadow" || rawEngine === "credits" ? rawEngine : "legacy";
  const displayCurrency: BillingDisplayCurrency =
    String(settings?.billing_display_currency || "").toUpperCase() === "CNY"
      ? "CNY"
      : "USD";
  const usdToCny = Number(settings?.billing_usd_to_cny);
  const creditsPerUSD = Number(settings?.billing_credits_per_usd);
  return {
    enabled,
    engine,
    creditsPerUSD:
      Number.isFinite(creditsPerUSD) && creditsPerUSD > 0
        ? creditsPerUSD
        : DEFAULT_BILLING_CURRENCY_VIEW.creditsPerUSD,
    displayCurrency,
    usdToCny:
      Number.isFinite(usdToCny) && usdToCny > 0
        ? usdToCny
        : DEFAULT_BILLING_CURRENCY_VIEW.usdToCny,
  };
}

export function usdToDisplayAmount(
  amountUSD: number,
  view: BillingCurrencyView,
): number {
  const amount = Number.isFinite(amountUSD) ? amountUSD : 0;
  return view.displayCurrency === "CNY" ? amount * view.usdToCny : amount;
}

export function creditsToDisplayAmount(
  credits: number,
  view: BillingCurrencyView,
): number {
  const normalizedCredits = Number.isFinite(credits) ? credits : 0;
  return usdToDisplayAmount(normalizedCredits / view.creditsPerUSD, view);
}

export function displayToUSD(amount: number, view: BillingCurrencyView): number {
  const value = Number.isFinite(amount) ? amount : 0;
  return view.displayCurrency === "CNY" ? value / view.usdToCny : value;
}

function getDisplayLocale(): string | undefined {
  if (typeof document !== "undefined" && document.documentElement.lang) {
    return document.documentElement.lang;
  }
  if (typeof navigator !== "undefined" && navigator.language) {
    return navigator.language;
  }
  return undefined;
}

export function formatBillingAmount(
  amountUSD: number,
  view: BillingCurrencyView,
  fractionDigits = 2,
): string {
  const amount = usdToDisplayAmount(amountUSD, view);
  const digits =
    view.displayCurrency === "CNY"
      ? Math.max(2, fractionDigits)
      : fractionDigits;
  try {
    return new Intl.NumberFormat(getDisplayLocale(), {
      style: "currency",
      currency: view.displayCurrency,
      currencyDisplay: "narrowSymbol",
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    }).format(amount);
  } catch {
    return `${view.displayCurrency === "CNY" ? "¥" : "$"}${amount.toFixed(digits)}`;
  }
}

export function formatBillingCredits(
  credits: number,
  view: BillingCurrencyView,
  fractionDigits = 2,
): string {
  return formatBillingAmount(
    credits / view.creditsPerUSD,
    view,
    fractionDigits,
  );
}

export function billingCurrencySymbol(
  currency: BillingDisplayCurrency,
): string {
  return currency === "CNY" ? "¥" : "$";
}

/** Resolve the current public-settings snapshot outside a Vue component. */
export function getCurrentBillingCurrencyView(): BillingCurrencyView {
  if (typeof window === "undefined") return DEFAULT_BILLING_CURRENCY_VIEW;
  return resolveBillingCurrencyView(window.__APP_CONFIG__);
}

export function formatCurrentBillingAmount(
  amountUSD: number,
  fractionDigits = 2,
): string {
  return formatBillingAmount(
    amountUSD,
    getCurrentBillingCurrencyView(),
    fractionDigits,
  );
}
