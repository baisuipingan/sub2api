<template>
  <AppLayout>
    <div class="space-y-4">
      <div v-if="loading" class="flex items-center justify-center py-16">
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <!-- ══ 第一排：今日核心指标 ══ -->
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="kpi" data-tone="violet">
            <div class="kpi-icon">
              <Icon name="chartBar" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.todayRequests') }}</div>
              <div class="kpi-value mt-1">{{ formatNumber(stats.today_requests) }}</div>
              <div class="kpi-sub">
                {{ t('admin.dashboard.totalRequests') }} {{ formatNumber(stats.total_requests) }}
              </div>
            </div>
          </div>

          <div class="kpi" data-tone="blue">
            <div class="kpi-icon">
              <Icon name="cube" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.todayTokens') }}</div>
              <div class="kpi-value mt-1">{{ formatTokens(stats.today_tokens) }}</div>
              <div class="kpi-sub flex flex-wrap gap-x-2">
                <span>{{ formatTokens(stats.today_input_tokens) }} in</span>
                <span>{{ formatTokens(stats.today_output_tokens) }} out</span>
              </div>
            </div>
          </div>

          <div class="kpi" data-tone="amber">
            <div class="kpi-icon">
              <Icon name="clock" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.avgResponse') }}</div>
              <div class="kpi-value mt-1">{{ formatDuration(stats.average_duration_ms) }}</div>
              <div class="kpi-sub">
                {{ stats.hourly_active_users }} {{ t('admin.dashboard.todayRequests') }}
              </div>
            </div>
          </div>

          <div class="kpi" data-tone="green">
            <div class="kpi-icon">
              <Icon name="database" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.totalTokens') }}</div>
              <div class="kpi-value mt-1">{{ formatTokens(stats.total_tokens) }}</div>
              <div class="kpi-sub flex flex-wrap gap-x-2">
                <span>{{ billingSymbol }}{{ formatCost(stats.total_actual_cost) }}</span>
                <span class="opacity-60">/ {{ billingSymbol }}{{ formatCost(stats.total_cost) }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- ══ 第二排：资源状况 ══ -->
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="kpi" data-tone="cyan">
            <div class="kpi-icon">
              <Icon name="key" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.apiKeys') }}</div>
              <div class="kpi-value mt-1">{{ formatNumber(stats.total_api_keys) }}</div>
              <div class="kpi-sub">
                {{ stats.active_api_keys }} {{ t('common.active') }}
              </div>
            </div>
          </div>

          <div class="kpi" data-tone="indigo">
            <div class="kpi-icon">
              <Icon name="server" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.accounts') }}</div>
              <div class="kpi-value mt-1">{{ formatNumber(stats.total_accounts) }}</div>
              <div class="kpi-sub flex flex-wrap gap-x-2">
                <span class="text-[var(--success)]">{{ stats.normal_accounts }} {{ t('common.active') }}</span>
                <span v-if="stats.error_accounts > 0" class="text-[var(--danger)]">
                  {{ stats.error_accounts }} {{ t('common.error') }}
                </span>
                <span v-if="stats.ratelimit_accounts > 0" class="text-[var(--warning)]">
                  {{ stats.ratelimit_accounts }} {{ t('admin.dashboard.rateLimited') }}
                </span>
              </div>
            </div>
          </div>

          <div class="kpi" data-tone="rose">
            <div class="kpi-icon">
              <Icon name="users" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.users') }}</div>
              <div class="kpi-value mt-1">{{ formatNumber(stats.total_users) }}</div>
              <div class="kpi-sub">
                <span v-if="stats.today_new_users > 0" class="text-[var(--success)]">
                  +{{ stats.today_new_users }}
                </span>
                <span class="ml-1">{{ t('admin.dashboard.totalTokens') }}: {{ stats.total_users }}</span>
              </div>
            </div>
          </div>

          <div class="kpi" data-tone="violet">
            <div class="kpi-icon">
              <Icon name="bolt" size="sm" :stroke-width="2" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="kpi-label">{{ t('admin.dashboard.performance') }}</div>
              <div class="kpi-value mt-1 text-[20px]">
                {{ formatNumber(stats.total_requests) }}
              </div>
              <div class="kpi-sub">
                {{ t('admin.dashboard.totalRequests') }}
              </div>
            </div>
          </div>
        </div>

        <!-- ══ 图表区：左侧趋势 + 右侧分布 ══ -->
        <div class="grid grid-cols-1 gap-4 xl:grid-cols-5">
          <div class="panel xl:col-span-3">
            <div class="panel-head">
              <div class="flex items-center gap-2">
                <h3 class="panel-title">{{ t('admin.dashboard.tokenUsageTrend') }}</h3>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <DateRangePicker
                  v-model:start-date="startDate"
                  v-model:end-date="endDate"
                  @change="onDateRangeChange"
                />
                <button
                  class="btn btn-secondary btn-sm"
                  :disabled="chartsLoading"
                  @click="loadDashboardStats"
                >
                  {{ t('common.refresh') }}
                </button>
                <div class="segmented">
                  <button
                    v-for="opt in granularityOptions"
                    :key="String(opt.value)"
                    type="button"
                    class="segmented-item"
                    :class="{ 'segmented-item-active': granularity === opt.value }"
                    @click="setGranularity((opt.value as 'hour' | 'day'))"
                  >
                    {{ opt.label }}
                  </button>
                </div>
              </div>
            </div>
            <div class="panel-body">
              <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" bare />
            </div>
          </div>

          <div class="xl:col-span-2">
            <ModelDistributionChart
                :model-stats="modelStats"
                :enable-ranking-view="true"
                :ranking-items="rankingItems"
                :ranking-total-actual-cost="rankingTotalActualCost"
                :ranking-total-requests="rankingTotalRequests"
                :ranking-total-tokens="rankingTotalTokens"
                :loading="chartsLoading"
                :ranking-loading="rankingLoading"
                :ranking-error="rankingError"
                :start-date="startDate"
                :end-date="endDate"
                @ranking-click="goToUserUsage"
            />
          </div>
        </div>

        <!-- ══ 用户用量趋势（全宽）══ -->
        <div class="panel">
          <div class="panel-head">
            <h3 class="panel-title">{{ t('admin.dashboard.recentUsage') }} (Top 12)</h3>
            <div class="segmented" role="group" :aria-label="t('admin.dashboard.recentUsage')">
              <button
                v-for="metric in (['tokens', 'actual_cost'] as const)"
                :key="metric"
                type="button"
                class="segmented-item"
                :class="{ 'segmented-item-active': userTrendMetric === metric }"
                :aria-pressed="userTrendMetric === metric"
                @click="setUserTrendMetric(metric)"
              >
                {{ t(metric === 'tokens' ? 'admin.dashboard.tokens' : 'admin.dashboard.actualSpending', { currency: billingSymbol }) }}
              </button>
            </div>
          </div>
          <div class="h-64 p-4">
            <div v-if="userTrendLoading" class="flex h-full items-center justify-center">
              <LoadingSpinner size="md" />
            </div>
            <Line v-else-if="userTrendChartData" :data="userTrendChartData" :options="lineOptions" />
            <div
              v-else
              class="flex h-full items-center justify-center text-sm text-[var(--text-3)]"
            >
              {{ t('admin.dashboard.noDataAvailable') }}
            </div>
          </div>
        </div>

        <!-- ══ 快速入口 ══ -->
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <button
            v-if="canUseBatchImage"
            type="button"
            class="panel group flex items-center gap-3 p-3 text-left transition-colors hover:border-[var(--border-strong)]"
            @click="router.push('/batch-image')"
          >
            <span class="kpi-icon !h-10 !w-10">
              <Icon name="sparkles" size="md" :stroke-width="2" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block text-[13px] font-semibold text-[var(--text-1)]">
                {{ t('admin.dashboard.batchImage') }}
              </span>
              <span class="block text-[11px] text-[var(--text-3)]">
                {{ t('admin.dashboard.batchImageDesc') }}
              </span>
            </span>
            <Icon name="chevronRight" size="sm" class="text-[var(--text-4)]" />
          </button>
          <button
            type="button"
            class="panel group flex items-center gap-3 p-3 text-left transition-colors hover:border-[var(--border-strong)]"
            @click="router.push('/admin/groups')"
          >
            <span class="kpi-icon !h-10 !w-10">
              <Icon name="grid" size="md" :stroke-width="2" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block text-[13px] font-semibold text-[var(--text-1)]">
                {{ t('admin.dashboard.groupPricing') }}
              </span>
              <span class="block text-[11px] text-[var(--text-3)]">
                {{ t('admin.dashboard.groupPricingDesc') }}
              </span>
            </span>
            <Icon name="chevronRight" size="sm" class="text-[var(--text-4)]" />
          </button>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
import { adminAPI } from '@/api/admin'
import type {
  DashboardStats,
  TrendDataPoint,
  ModelStat,
  UserUsageTrendPoint,
  UserSpendingRankingItem
} from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useBillingCurrency } from '@/composables/useBillingCurrency'

import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
)

const appStore = useAppStore()
const router = useRouter()
const { formatCompact, symbol: billingSymbol } = useBillingCurrency()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
const stats = ref<DashboardStats | null>(null)
const loading = ref(false)
const chartsLoading = ref(false)
const userTrendLoading = ref(false)
const rankingLoading = ref(false)
const rankingError = ref(false)

// Chart data
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const userTrend = ref<UserUsageTrendPoint[]>([])
const userTrendMetric = ref<'tokens' | 'actual_cost'>('tokens')
const rankingItems = ref<UserSpendingRankingItem[]>([])
const rankingTotalActualCost = ref(0)
const rankingTotalRequests = ref(0)
const rankingTotalTokens = ref(0)
let chartLoadSeq = 0
let usersTrendLoadSeq = 0
let rankingLoadSeq = 0
const rankingLimit = 12

// Helper function to format date in local timezone
const formatLocalDate = (date: Date): string => {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const getLast24HoursRangeDates = (): { start: string; end: string } => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return {
    start: formatLocalDate(start),
    end: formatLocalDate(end)
  }
}

// Date range
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)

// Granularity options for Select component
const granularityOptions = computed(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') }
])

/** 分段控件切换粒度；避免在模板里写赋值表达式（类型收窄会失败） */
function setGranularity(next: 'hour' | 'day') {
  if (granularity.value === next) return
  granularity.value = next
  loadChartData()
}

// Dark mode detection
const isDarkMode = computed(() => {
  return document.documentElement.classList.contains('dark')
})

// Chart colors
const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

// Line chart options (for user trend chart)
const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      itemSort: (a: any, b: any) => {
        const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
        const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
        return bValue - aValue
      },
      callbacks: {
        label: (context: any) => {
          return `${context.dataset.label}: ${formatUserTrendValue(Number(context.raw))}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatUserTrendValue(Number(value))
      }
    }
  }
}))

// User trend chart data
const userTrendChartData = computed(() => {
  if (!userTrend.value?.length) return null

  const getDisplayName = (point: UserUsageTrendPoint): string => {
    const username = point.username?.trim()
    if (username) {
      return username
    }

    const email = point.email?.trim()
    if (email) {
      return email
    }

    return t('admin.redeem.userPrefix', { id: point.user_id })
  }

  // Group by user_id to avoid merging different users with the same display name
  const userGroups = new Map<number, { name: string; data: Map<string, number> }>()
  const allDates = new Set<string>()

  userTrend.value.forEach((point) => {
    allDates.add(point.date)
    const key = point.user_id
    if (!userGroups.has(key)) {
      userGroups.set(key, { name: getDisplayName(point), data: new Map() })
    }
    userGroups.get(key)!.data.set(point.date, userTrendMetric.value === 'tokens' ? point.tokens : point.actual_cost)
  })

  const sortedDates = Array.from(allDates).sort()
  const colors = [
    '#3b82f6',
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#14b8a6',
    '#f97316',
    '#6366f1',
    '#84cc16',
    '#06b6d4',
    '#a855f7'
  ]

  const datasets = Array.from(userGroups.values()).map((group, idx) => ({
    label: group.name,
    data: sortedDates.map((date) => group.data.get(date) || 0),
    borderColor: colors[idx % colors.length],
    backgroundColor: `${colors[idx % colors.length]}20`,
    fill: false,
    tension: 0.3
  }))

  return {
    labels: sortedDates,
    datasets
  }
})

// Format helpers
const formatUserTrendValue = (value: number): string =>
  userTrendMetric.value === 'tokens' ? formatTokens(value) : formatCost(value)

const formatTokens = (value: number | undefined): string => {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatNumber = (value: number | null | undefined): string => {
  return toFiniteNumber(value).toLocaleString()
}

const formatCost = (value: number | null | undefined): string => {
  return formatCompact(toFiniteNumber(value))
}

const formatDuration = (ms: number): string => {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

const goToUserUsage = (item: UserSpendingRankingItem) => {
  void router.push({
    path: '/admin/usage',
    query: {
      user_id: String(item.user_id),
      start_date: startDate.value,
      end_date: endDate.value
    }
  })
}

// Date range change handler
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  preset: string | null
}) => {
  // Auto-select granularity based on date range
  const start = new Date(range.startDate)
  const end = new Date(range.endDate)
  const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  // If range is 1 day, use hourly granularity
  if (daysDiff <= 1) {
    granularity.value = 'hour'
  } else {
    granularity.value = 'day'
  }

  loadChartData()
}

// Load data
const loadDashboardSnapshot = async (includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) {
    loading.value = true
  }
  chartsLoading.value = true
  try {
    const response = await adminAPI.dashboard.getSnapshotV2({
      start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      include_stats: includeStats,
      include_trend: true,
      include_model_stats: true,
      include_group_stats: false,
      include_users_trend: false
    })
    if (currentSeq !== chartLoadSeq) return
    if (includeStats && response.stats) {
      stats.value = response.stats
    }
    trendData.value = response.trend || []
    modelStats.value = response.models || []
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    appStore.showError(t('admin.dashboard.failedToLoad'))
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const setUserTrendMetric = (metric: 'tokens' | 'actual_cost') => {
  if (userTrendMetric.value === metric) return
  userTrendMetric.value = metric
  userTrend.value = []
  loadUsersTrend()
}

const loadUsersTrend = async () => {
  const currentSeq = ++usersTrendLoadSeq
  userTrendLoading.value = true
  try {
    const response = await adminAPI.dashboard.getUserUsageTrend({
      start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      limit: 12,
      metric: userTrendMetric.value
    })
    if (currentSeq !== usersTrendLoadSeq) return
    userTrend.value = response.trend || []
  } catch (error) {
    if (currentSeq !== usersTrendLoadSeq) return
    console.error('Error loading users trend:', error)
    userTrend.value = []
  } finally {
    if (currentSeq === usersTrendLoadSeq) {
      userTrendLoading.value = false
    }
  }
}

const loadUserSpendingRanking = async () => {
  const currentSeq = ++rankingLoadSeq
  rankingLoading.value = true
  rankingError.value = false
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({
      start_date: startDate.value,
      end_date: endDate.value,
      limit: rankingLimit
    })
    if (currentSeq !== rankingLoadSeq) return
    rankingItems.value = response.ranking || []
    rankingTotalActualCost.value = response.total_actual_cost || 0
    rankingTotalRequests.value = response.total_requests || 0
    rankingTotalTokens.value = response.total_tokens || 0
  } catch (error) {
    if (currentSeq !== rankingLoadSeq) return
    console.error('Error loading user spending ranking:', error)
    rankingItems.value = []
    rankingTotalActualCost.value = 0
    rankingTotalRequests.value = 0
    rankingTotalTokens.value = 0
    rankingError.value = true
  } finally {
    if (currentSeq === rankingLoadSeq) {
      rankingLoading.value = false
    }
  }
}

const loadDashboardStats = async () => {
  await Promise.all([
    loadDashboardSnapshot(true),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

const loadChartData = async () => {
  await Promise.all([
    loadDashboardSnapshot(false),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

onMounted(() => {
  void refreshBatchImageAccess()
  loadDashboardStats()
})
</script>

<style scoped>
</style>
