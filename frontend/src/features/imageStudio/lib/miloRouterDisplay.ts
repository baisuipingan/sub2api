export function formatMiloRouterBalance(balance?: number) {
  if (typeof balance !== 'number' || !Number.isFinite(balance)) return undefined
  return balance.toFixed(2)
}
