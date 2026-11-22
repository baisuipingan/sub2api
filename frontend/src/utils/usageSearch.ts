import type { UsageLog } from '@/types'

const providerNames: Record<string, string> = { deepseek: 'DeepSeek', brave: 'Brave', tavily: 'Tavily' }

export function usageModelLabel(model: string, t: (key: string) => string): string {
  return model === 'workbench-web-search' ? t('usage.webSearch') : model
}

export function usageSearchProvider(row: Pick<UsageLog, 'search_provider'>): string {
  const provider = row.search_provider
  return provider ? providerNames[provider] || provider : ''
}
