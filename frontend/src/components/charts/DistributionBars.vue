<template>
  <div class="dist-panel">
    <!-- 头部：Tab 切换 + 项数 -->
    <div class="dist-panel-head">
      <div class="dist-tabs" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          role="tab"
          class="dist-tab"
          :class="{ 'dist-tab-on': activeTab === tab.key }"
          :aria-selected="activeTab === tab.key"
          @click="switchTab(tab.key)"
        >
          {{ tab.label }}
        </button>
      </div>

      <div class="dist-panel-meta">
        <span v-if="loading" class="dist-count">加载中…</span>
        <span v-else class="dist-count">{{ currentItems.length }} 项</span>
      </div>
    </div>

    <!-- 指标切换 + 来源切换 -->
    <div v-if="showMetricToggle || showSourceToggle" class="dist-metric-row">
      <div v-if="showMetricToggle" class="seg">
        <button
          v-for="m in metricOptions"
          :key="m.value"
          type="button"
          class="seg-btn"
          :class="{ 'seg-btn-on': metric === m.value }"
          @click="emit('update:metric', m.value)"
        >
          {{ m.label }}
        </button>
      </div>

      <div v-if="showSourceToggle && sourceOptions.length > 1" class="seg ml-auto">
        <button
          v-for="src in sourceOptions"
          :key="src.value"
          type="button"
          class="seg-btn"
          :class="{ 'seg-btn-on': currentSource === src.value }"
          @click="selectSource(src.value)"
        >
          {{ src.label }}
        </button>
      </div>
    </div>

    <!-- 横条列表：固定高度可滚 -->
    <div ref="scrollRef" class="dist-scroll">
      <div v-if="loading" class="dist-empty">加载中…</div>
      <div v-else-if="bars.length === 0" class="dist-empty">所选范围内暂无数据</div>
      <template v-else>
        <div
          v-for="(bar, i) in bars"
          :key="bar.key"
        >
          <component
            :is="canExpand(bar) ? 'button' : 'div'"
            :type="canExpand(bar) ? 'button' : undefined"
            class="cbar"
            :class="{ 'cbar-top': i === 0, 'cbar-button': canExpand(bar) }"
            :title="`${bar.name} · ${bar.displayValue}`"
            :aria-expanded="canExpand(bar) ? expandedKey === bar.key : undefined"
            :aria-controls="canExpand(bar) && expandedKey === bar.key ? breakdownId : undefined"
            @click="canExpand(bar) && toggleBreakdown(bar)"
          >
            <span class="cbar-name flex items-center gap-1.5">
              <Icon
                v-if="canExpand(bar)"
                :name="expandedKey === bar.key ? 'chevronDown' : 'chevronRight'"
                size="sm"
                class="shrink-0 text-primary-500"
                aria-hidden="true"
              />
              <span class="min-w-0 truncate">{{ bar.name }}</span>
            </span>
            <span class="cbar-track">
              <span class="cbar-fill block" :style="{ width: bar.width + '%' }" />
            </span>
            <span class="cbar-val">{{ bar.displayValue }}</span>
          </component>
          <div
            v-if="expandedKey === bar.key"
            :id="breakdownId"
            role="region"
            :aria-label="t('usage.distribution.userBreakdown', { name: bar.name })"
            class="my-1 overflow-hidden rounded-md border border-gray-100 dark:border-dark-700"
          >
            <div v-if="breakdownError" role="alert" class="flex items-center justify-between gap-2 p-3 text-xs text-gray-500 dark:text-gray-400">
              <span>{{ t('usage.distribution.breakdownFailed') }}</span>
              <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="loadBreakdown(bar)">
                {{ t('usage.distribution.retry') }}
              </button>
            </div>
            <UserBreakdownSubTable
              v-else
              :items="breakdownItems"
              :loading="breakdownLoading"
              show-header
            />
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelStat, GroupStat, EndpointStat, UserBreakdownItem } from '@/types'
import { useBillingCurrency } from '@/composables/useBillingCurrency'
import { formatCompactNumber } from '@/utils/format'
import { getUserBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import UserBreakdownSubTable from './UserBreakdownSubTable.vue'
import Icon from '@/components/icons/Icon.vue'

/** 与三个分布组件内部保持一致的局部类型 */
type DistributionMetric = 'tokens' | 'actual_cost'
type DistTab = 'model' | 'group' | 'endpoint'

/** 三种分布的最小公共结构 */
interface DistItem {
  key: string
  name: string
  groupId?: number
  requests: number
  total_tokens: number
  actual_cost: number
}

const props = withDefaults(
  defineProps<{
    modelStats?: ModelStat[]
    groupStats?: GroupStat[]
    endpointStats?: EndpointStat[]
    metric?: DistributionMetric
    showMetricToggle?: boolean
    loading?: boolean
    defaultTab?: DistTab
    showGroup?: boolean
    showEndpoint?: boolean
    /** 上游/映射数据（管理端用） */
    upstreamModelStats?: ModelStat[]
    mappingModelStats?: ModelStat[]
    upstreamEndpointStats?: EndpointStat[]
    endpointPathStats?: EndpointStat[]
    /** 是否显示来源切换（管理端） */
    showSourceToggle?: boolean
    /** 来源选择 */
    modelSource?: 'requested' | 'upstream' | 'mapping'
    endpointSource?: 'inbound' | 'upstream' | 'path'
    /** 只在管理员使用记录页开启，用户页不请求管理员接口。 */
    enableBreakdown?: boolean
    startDate?: string
    endDate?: string
    filters?: UserBreakdownParams
    /** 端点总量来自使用统计接口，另包含页面的模型与计费模式筛选。 */
    endpointFilters?: UserBreakdownParams
    breakdownRefreshKey?: number
  }>(),
  {
    modelStats: () => [],
    groupStats: () => [],
    endpointStats: () => [],
    metric: 'tokens',
    showMetricToggle: true,
    loading: false,
    defaultTab: 'model',
    showGroup: true,
    showEndpoint: true,
    upstreamModelStats: () => [],
    mappingModelStats: () => [],
    upstreamEndpointStats: () => [],
    endpointPathStats: () => [],
    showSourceToggle: false,
    modelSource: 'requested',
    endpointSource: 'inbound',
    enableBreakdown: false,
    filters: () => ({}),
    endpointFilters: () => ({}),
    breakdownRefreshKey: 0,
  },
)

const emit = defineEmits<{
  'update:metric': [value: DistributionMetric]
  'update:modelSource': [value: 'requested' | 'upstream' | 'mapping']
  'update:endpointSource': [value: 'inbound' | 'upstream' | 'path']
}>()

const { t } = useI18n()
const { formatCompact } = useBillingCurrency()

const activeTab = ref<DistTab>(props.defaultTab)
const scrollRef = ref<HTMLElement | null>(null)
const breakdownId = useId()
const breakdownLimit = 50
const expandedKey = ref<string | null>(null)
const breakdownItems = ref<UserBreakdownItem[]>([])
const breakdownLoading = ref(false)
const breakdownError = ref(false)
let breakdownRequest = 0
let breakdownController: AbortController | undefined

function resetBreakdown() {
  breakdownRequest++
  breakdownController?.abort()
  breakdownController = undefined
  expandedKey.value = null
  breakdownItems.value = []
  breakdownLoading.value = false
  breakdownError.value = false
}

function canExpand(item: DistItem) {
  return props.enableBreakdown && (activeTab.value !== 'group' || (item.groupId ?? 0) > 0)
}

async function loadBreakdown(item: DistItem) {
  breakdownController?.abort()
  const controller = new AbortController()
  breakdownController = controller
  const request = ++breakdownRequest
  breakdownLoading.value = true
  breakdownError.value = false
  breakdownItems.value = []
  const params: UserBreakdownParams = {
    ...props.filters,
    ...(activeTab.value === 'endpoint' ? props.endpointFilters : {}),
    start_date: props.startDate,
    end_date: props.endDate,
    limit: breakdownLimit,
    sort_by: props.metric === 'tokens' ? 'total_tokens' : 'actual_cost',
  }
  if (activeTab.value === 'model') {
    params.model = item.name
    params.model_source = props.modelSource
  } else if (activeTab.value === 'group') {
    params.group_id = item.groupId
  } else {
    params.endpoint = item.name
    params.endpoint_type = props.endpointSource
  }
  try {
    const result = await getUserBreakdown(params, { signal: controller.signal })
    if (request === breakdownRequest) breakdownItems.value = result.users || []
  } catch {
    if (request === breakdownRequest) breakdownError.value = true
  } finally {
    if (request === breakdownRequest) breakdownLoading.value = false
  }
}

function toggleBreakdown(item: DistItem) {
  if (expandedKey.value === item.key) {
    resetBreakdown()
    return
  }
  expandedKey.value = item.key
  void loadBreakdown(item)
}

// 相同筛选的手动刷新也失效；旧请求不能覆盖新行、新来源或新日期的数据。
watch(
  () => JSON.stringify([
    activeTab.value, props.modelSource, props.endpointSource, props.metric,
    props.startDate, props.endDate, props.filters, props.endpointFilters,
    props.breakdownRefreshKey, props.enableBreakdown,
  ]),
  resetBreakdown,
  { flush: 'sync' },
)
watch(() => props.loading, (loading) => { if (loading) resetBreakdown() }, { flush: 'sync' })
onUnmounted(resetBreakdown)

const tabs = computed(() => {
  const list: { key: DistTab; label: string }[] = [
    { key: 'model', label: t('usage.distribution.modelTab') },
  ]
  if (props.showGroup) list.push({ key: 'group', label: t('usage.distribution.groupTab') })
  if (props.showEndpoint) list.push({ key: 'endpoint', label: t('usage.distribution.endpointTab') })
  return list
})

const metricOptions = computed<{ value: DistributionMetric; label: string }[]>(() => [
  { value: 'tokens', label: t('usage.distribution.byTokens') },
  { value: 'actual_cost', label: t('usage.distribution.byCost') },
])

/** 当前 Tab 对应的来源选项 */
const sourceOptions = computed<{ value: string; label: string }[]>(() => {
  if (activeTab.value === 'model') {
    return [
      { value: 'requested', label: t('usage.distribution.srcRequested') },
      { value: 'upstream', label: t('usage.distribution.srcUpstream') },
      { value: 'mapping', label: t('usage.distribution.srcMapping') },
    ]
  }
  if (activeTab.value === 'endpoint') {
    return [
      { value: 'inbound', label: t('usage.distribution.srcInbound') },
      { value: 'upstream', label: t('usage.distribution.srcUpstream') },
      { value: 'path', label: t('usage.distribution.srcPath') },
    ]
  }
  return []
})

const currentSource = computed(() =>
  activeTab.value === 'model' ? props.modelSource : props.endpointSource,
)

function selectSource(value: string) {
  if (activeTab.value === 'model') {
    emit('update:modelSource', value as 'requested' | 'upstream' | 'mapping')
  } else if (activeTab.value === 'endpoint') {
    emit('update:endpointSource', value as 'inbound' | 'upstream' | 'path')
  }
}

function switchTab(key: DistTab) {
  activeTab.value = key
  // 切 Tab 时回到顶部，避免沿用一个 Tab 的滚动位置
  if (scrollRef.value) scrollRef.value.scrollTop = 0
}

/** 把三种不同的 stat 结构归一成 DistItem */
function normalize(): DistItem[] {
  if (activeTab.value === 'model') {
    const src =
      props.modelSource === 'upstream'
        ? props.upstreamModelStats
        : props.modelSource === 'mapping'
          ? props.mappingModelStats
          : props.modelStats
    return src.map((s) => ({
      key: `model-${props.modelSource}-${s.model}`,
      name: s.model,
      requests: s.requests,
      total_tokens: s.total_tokens,
      actual_cost: s.actual_cost,
    }))
  }
  if (activeTab.value === 'group') {
    return props.groupStats.map((s) => ({
      key: `group-${s.group_id}`,
      name: s.group_name,
      groupId: s.group_id,
      requests: s.requests,
      total_tokens: s.total_tokens,
      actual_cost: s.actual_cost,
    }))
  }
  const epSrc =
    props.endpointSource === 'upstream'
      ? props.upstreamEndpointStats
      : props.endpointSource === 'path'
        ? props.endpointPathStats
        : props.endpointStats
  return epSrc.map((s) => ({
    key: `endpoint-${props.endpointSource}-${s.endpoint}`,
    name: s.endpoint,
    requests: s.requests,
    total_tokens: s.total_tokens,
    actual_cost: s.actual_cost,
  }))
}

/** 取当前指标的数值 */
function metricValue(d: DistItem): number {
  return props.metric === 'actual_cost' ? d.actual_cost : d.total_tokens
}

/** 展示值格式化：Token 走紧凑单位，费用走货币格式 */
function displayValue(d: DistItem): string {
  return props.metric === 'actual_cost'
    ? formatCompact(d.actual_cost)
    : formatCompactNumber(d.total_tokens, { allowBillions: true })
}

/** 当前 Tab 的数据，按指标降序 */
const currentItems = computed<DistItem[]>(() =>
  [...normalize()].sort((a, b) => metricValue(b) - metricValue(a)),
)

/**
 * 横条宽度。
 *
 * 用 √ 开方而不是线性比例：真实用量是长尾分布，
 * 第一名与末位常相差数百倍（实测 5.72M vs 9K = 636 倍），
 * 线性比例下末位宽度不足 1%，会塌成看不见的线。
 * 开方后既保留量级差异、又让每一项可辨。
 * 再设 2% 最小宽度兜底，避免极小项彻底消失。
 */
const bars = computed(() => {
  const items = currentItems.value
  if (items.length === 0) return []
  const values = items.map(metricValue)
  const max = Math.max(...values, 1)
  return items.map((d) => {
    const ratio = Math.sqrt(metricValue(d) / max)
    return {
      ...d,
      displayValue: displayValue(d),
      width: Math.max(2, ratio * 100),
    }
  })
})
</script>

<style scoped>
.cbar-button {
  @apply w-full rounded px-1 text-left transition-colors hover:bg-gray-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700/50;
  height: 28px;
}
</style>
