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
          :key="bar.name"
          class="cbar"
          :class="{ 'cbar-top': i === 0 }"
          :title="`${bar.name} · ${bar.displayValue}`"
        >
          <span class="cbar-name">{{ bar.name }}</span>
          <div class="cbar-track">
            <div class="cbar-fill" :style="{ width: bar.width + '%' }" />
          </div>
          <span class="cbar-val">{{ bar.displayValue }}</span>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelStat, GroupStat, EndpointStat } from '@/types'
import { useBillingCurrency } from '@/composables/useBillingCurrency'

/** 与三个分布组件内部保持一致的局部类型 */
type DistributionMetric = 'tokens' | 'actual_cost'
type DistTab = 'model' | 'group' | 'endpoint'

/** 三种分布的最小公共结构 */
interface DistItem {
  name: string
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
      name: s.model,
      requests: s.requests,
      total_tokens: s.total_tokens,
      actual_cost: s.actual_cost,
    }))
  }
  if (activeTab.value === 'group') {
    return props.groupStats.map((s) => ({
      name: s.group_name,
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
    : formatCompact(d.total_tokens)
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
      name: d.name,
      displayValue: displayValue(d),
      width: Math.max(2, ratio * 100),
    }
  })
})
</script>
