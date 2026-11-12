<template>
  <div class="bg-gray-50/50 dark:bg-dark-700/30">
    <div v-if="loading" class="flex items-center justify-center py-3">
      <LoadingSpinner />
    </div>
    <div v-else-if="items.length === 0" class="py-2 text-center text-xs text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-xs" :class="{ 'min-w-[480px]': showHeader }">
        <thead v-if="showHeader">
          <tr class="border-b border-gray-100 text-[10px] text-gray-500 dark:border-dark-700 dark:text-gray-400">
            <th scope="col" class="px-2 py-2 text-left font-medium">{{ t('usage.distribution.user') }}</th>
            <th scope="col" class="py-2 text-right font-medium">{{ t('admin.dashboard.requests') }}</th>
            <th scope="col" class="py-2 text-right font-medium">{{ t('admin.dashboard.tokens') }}</th>
            <th scope="col" class="py-2 text-right font-medium">{{ t('admin.dashboard.actual') }}</th>
            <th v-if="showAccountCost" scope="col" class="py-2 text-right font-medium">{{ t('usage.accountCost') }}</th>
            <th scope="col" class="px-2 py-2 text-right font-medium">{{ t('admin.dashboard.standard') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="user in items"
            :key="user.user_id"
            class="border-t border-gray-100/50 dark:border-dark-700/50"
          >
            <td class="max-w-[120px] truncate py-1 text-gray-600 dark:text-gray-300" :class="showHeader ? 'pl-2' : 'pl-6'" :title="user.email">
              {{ user.email || `User #${user.user_id}` }}
            </td>
            <td class="py-1 text-right text-gray-500 dark:text-gray-400">
              {{ user.requests.toLocaleString() }}
            </td>
            <td class="py-1 text-right text-gray-500 dark:text-gray-400">
              {{ formatTokens(user.total_tokens) }}
            </td>
            <td class="py-1 text-right text-green-600 dark:text-green-400">
              {{ formatCost(user.actual_cost) }}
            </td>
            <td v-if="showAccountCost" class="py-1 text-right text-orange-500 dark:text-orange-400">
              {{ formatCost(user.account_cost) }}
            </td>
            <td class="py-1 pr-1 text-right text-gray-400 dark:text-gray-500">
              {{ formatCost(user.cost) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { useBillingCurrency } from '@/composables/useBillingCurrency'
import type { UserBreakdownItem } from '@/types'

const { t } = useI18n()
const { formatCompact } = useBillingCurrency()

const props = withDefaults(defineProps<{
  items: UserBreakdownItem[]
  loading?: boolean
  showAccountCost?: boolean
  showHeader?: boolean
}>(), {
  loading: false,
  showAccountCost: true,
  showHeader: false,
})

const showAccountCost = computed(() => props.showAccountCost)

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number | null | undefined): string => {
  return formatCompact(toFiniteNumber(value))
}

</script>
