<template>
  <AppLayout>
    <div class="space-y-6" :aria-busy="loading" data-testid="workbench-overview">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div class="max-w-3xl">
          <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.overviewTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.workbench.scope') }}</p>
        </div>
        <div class="flex w-full flex-wrap items-center gap-2 sm:w-auto">
          <div class="min-w-40 flex-1 sm:flex-none">
            <Select :model-value="days" :options="periodOptions" :aria-label="t('admin.workbench.period')" @update:model-value="days = Number($event) as WorkbenchDays" />
          </div>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
            <Icon name="refresh" class="mr-1.5" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}
          </button>
        </div>
      </div>

      <div v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
        {{ t('admin.workbench.loadFailed') }}
      </div>
      <LoadingSpinner v-if="loading && !overview" />
      <template v-else-if="overview">
        <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <StatCard :title="t('admin.workbench.totalTasks')" :value="overview.tasks.total" :icon="taskIcon" />
          <StatCard :title="t('admin.workbench.successRate')" :value="successRate" :icon="successIcon" />
          <StatCard :title="t('admin.workbench.outputImages')" :value="overview.output_images" :icon="imageIcon" />
          <StatCard :title="t('admin.workbench.modelCalls')" :value="overview.calls.total" :icon="callIcon" />
        </div>
        <div class="space-y-1 text-xs leading-5 text-gray-500 dark:text-dark-400">
          <p>{{ t('admin.workbench.scopeHint') }}</p>
          <p>{{ t('admin.workbench.successHint') }}</p>
          <p>{{ t('admin.workbench.callsHint') }}</p>
        </div>

        <section class="card p-4 sm:p-6" :aria-label="t('admin.workbench.queueTitle')">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.queueTitle') }}</h3>
            <RouterLink to="/admin/workbench/tasks" class="text-sm font-medium text-primary-600 hover:underline dark:text-primary-400">{{ t('admin.workbench.viewTasks') }}</RouterLink>
          </div>
          <dl class="mt-5 grid grid-cols-2 gap-5 sm:grid-cols-4">
            <div v-for="status in queueStatuses" :key="status">
              <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.workbench.statuses.' + status) }}</dt>
              <dd class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ number(overview.queue[status]) }}</dd>
            </div>
          </dl>
          <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbench.queueHint') }}</p>
        </section>

        <section class="card p-4 sm:p-6" :aria-label="t('admin.workbench.breakdownTitle')">
          <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.breakdownTitle') }}</h3>
          <dl class="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-5">
            <div v-for="status in resultStatuses" :key="status" class="flex items-center justify-between gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-900/50">
              <dt><StatusBadge :status="workbenchStatusVariant(status)" :label="t('admin.workbench.statuses.' + status)" /></dt>
              <dd class="font-medium tabular-nums">{{ number(overview.tasks[status]) }}</dd>
            </div>
          </dl>
        </section>

        <section class="card p-4 sm:p-6" :aria-label="t('admin.workbench.galleryTitle')">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.galleryTitle') }}</h3>
            <RouterLink to="/admin/workbench/gallery" class="text-sm font-medium text-primary-600 hover:underline dark:text-primary-400">{{ t('admin.workbench.galleryBrowse') }}</RouterLink>
          </div>
          <dl class="mt-5 grid grid-cols-2 gap-5 sm:grid-cols-4">
            <div v-for="item in galleryStats" :key="item.label">
              <dt class="text-sm text-gray-500 dark:text-dark-400">{{ item.label }}</dt>
              <dd class="mt-2 break-words text-xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ item.value }}</dd>
            </div>
          </dl>
          <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbench.galleryHint') }}</p>
        </section>
        <p class="text-xs text-gray-400 dark:text-dark-500">{{ t('admin.workbench.refreshedAt', { time: formatTime(overview.until) }) }}</p>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, h, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import StatCard from '@/components/common/StatCard.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getWorkbenchOverview, type WorkbenchDays, type WorkbenchOverview } from '@/api/admin/workbench'
import { formatBytes } from '@/utils/format'
import { workbenchStatusVariant } from './workbenchDisplay'

const { t, locale } = useI18n()
const days = ref<WorkbenchDays>(7)
const overview = ref<WorkbenchOverview | null>(null)
const loading = ref(false)
const error = ref(false)
let controller: AbortController | null = null
let sequence = 0
const queueStatuses = ['queued', 'generating', 'saving', 'unknown'] as const
const resultStatuses = ['completed', 'partial', 'failed', 'unknown', 'cancelled'] as const
const periodOptions = computed(() => ([7, 30, 90] as const).map(value => ({ value, label: t('admin.workbench.days', { days: value }) })))
const number = (value: number) => value.toLocaleString(locale.value)
const formatTime = (value: string) => new Date(value).toLocaleString(locale.value)
const taskIcon = { render: () => h(Icon, { name: 'chart' }) }
const successIcon = { render: () => h(Icon, { name: 'checkCircle' }) }
const imageIcon = { render: () => h(Icon, { name: 'sparkles' }) }
const callIcon = { render: () => h(Icon, { name: 'arrowsUpDown' }) }
const successRate = computed(() => overview.value?.success_rate == null ? '—' : new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(overview.value.success_rate))
const galleryStats = computed(() => overview.value ? [
  { label: t('admin.workbench.galleryTasks'), value: number(overview.value.gallery.tasks) },
  { label: t('admin.workbench.galleryImages'), value: number(overview.value.gallery.output_images) },
  { label: t('admin.workbench.galleryAssets'), value: number(overview.value.gallery.assets) },
  { label: t('admin.workbench.galleryBytes'), value: formatBytes(overview.value.gallery.used_bytes, 1) },
] : [])

async function load() {
  controller?.abort()
  controller = new AbortController()
  const current = ++sequence
  loading.value = true
  error.value = false
  overview.value = null
  try {
    const result = await getWorkbenchOverview(days.value, controller.signal)
    if (current === sequence) overview.value = result
  } catch {
    if (current === sequence) error.value = true
  } finally {
    if (current === sequence) loading.value = false
  }
}

watch(days, () => { void load() }, { immediate: true })
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
