<template>
  <div class="grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(16rem,1fr)]">
    <section class="card min-w-0 p-4 sm:p-5" aria-labelledby="workbench-chat-overview">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 id="workbench-chat-overview" class="font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.overview.chatTitle') }}</h3>
        <RouterLink to="/admin/workbench/chat" class="text-sm text-primary-600 hover:underline dark:text-primary-400">
          {{ t('admin.workbench.overview.chatHistory') }}
        </RouterLink>
      </div>

      <dl class="mt-4 grid grid-cols-2 gap-x-4 gap-y-4 md:grid-cols-4">
        <div v-for="item in metrics" :key="item.label" class="min-w-0">
          <dt class="text-xs text-gray-500 dark:text-dark-400">{{ item.label }}</dt>
          <dd class="mt-1 break-words text-xl font-semibold tabular-nums text-gray-900 dark:text-white sm:text-2xl">{{ item.value }}</dd>
        </div>
      </dl>

      <div class="mt-4 grid gap-3 border-t border-gray-100 pt-3 text-sm dark:border-dark-700 sm:grid-cols-2">
        <dl class="grid gap-1.5 text-xs">
          <div v-for="item in results" :key="item.label" class="flex items-baseline justify-between gap-3">
            <dt :class="item.color">{{ item.label }}</dt>
            <dd class="font-medium tabular-nums">{{ item.value }}</dd>
          </div>
        </dl>
        <dl class="grid content-start gap-1.5 text-xs sm:border-l sm:border-gray-100 sm:pl-4 sm:dark:border-dark-700">
          <div v-for="item in usage" :key="item.label" class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-dark-400">{{ item.label }}</dt>
            <dd class="text-right font-medium tabular-nums">{{ item.value }}</dd>
          </div>
        </dl>
      </div>
    </section>

    <section class="card min-w-0 p-4 sm:p-5" aria-labelledby="workbench-office-service">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 id="workbench-office-service" class="font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.overview.officeService') }}</h3>
        <StatusBadge :status="toolsBadge[overview.office_tools]" :label="t('admin.workbench.overview.tools.' + overview.office_tools)" />
      </div>
      <div class="mt-2 flex flex-wrap items-center justify-between gap-2 text-sm">
        <p v-if="overview.office_nodes" class="text-gray-500 dark:text-dark-400">
          {{ t('admin.workbench.nodes.summary', { online: number(overview.office_nodes.online), total: number(overview.office_nodes.total), slots: number(overview.office_nodes.slots) }) }}
        </p>
        <RouterLink to="/admin/workbench/office-nodes" class="text-primary-600 hover:underline dark:text-primary-400">
          {{ t('nav.workbenchOfficeNodes') }}
        </RouterLink>
      </div>
      <div class="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
        <h4 class="text-sm font-medium">{{ t('admin.workbench.overview.officeFiles') }}</h4>
        <RouterLink to="/admin/workbench/files" class="text-sm text-primary-600 hover:underline dark:text-primary-400">
          {{ t('admin.workbench.overview.userFiles') }}
        </RouterLink>
      </div>
      <dl class="mt-3 grid grid-cols-2 gap-4">
        <div>
          <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.overview.availableFiles') }}</dt>
          <dd class="mt-1 text-lg font-semibold tabular-nums">{{ number(overview.resource_count) }}</dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.overview.fileStorage') }}</dt>
          <dd class="mt-1 break-words text-lg font-semibold tabular-nums">{{ formatBytes(overview.used_bytes, 1) }}</dd>
        </div>
      </dl>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusBadge from '@/components/common/StatusBadge.vue'
import type { WorkbenchChatOverview } from '@/api/admin/workbench'
import { formatBytes } from '@/utils/format'
import { officeBilling } from './workbenchChatDisplay'

const props = defineProps<{ overview: WorkbenchChatOverview }>()
const { t, locale } = useI18n()
const number = (value: number) => value.toLocaleString(locale.value)
const toolsBadge = { ready: 'success', unavailable: 'error', unconfigured: 'warning', disabled: 'neutral' }
const metrics = computed(() => [
  { label: t('admin.workbench.overview.turns'), value: number(props.overview.total_runs) },
  { label: t('admin.workbench.overview.settledModels'), value: number(props.overview.model_calls) },
  { label: t('admin.workbench.overview.settledSearches'), value: number(props.overview.search_calls) },
  { label: t('admin.workbench.overview.totalCost'), value: officeBilling(props.overview.charge_credits) },
])
const results = computed(() => [
  { label: t('admin.workbench.overview.completed'), value: number(props.overview.completed), color: 'text-emerald-700 dark:text-emerald-400' },
  { label: t('admin.workbench.overview.failed'), value: number(props.overview.failed), color: 'text-red-600 dark:text-red-400' },
  { label: t('admin.workbench.overview.otherResults'), value: t('admin.workbench.overview.otherCounts', {
    partial: number(props.overview.partially_completed), cancelled: number(props.overview.cancelled),
  }), color: 'text-gray-500 dark:text-dark-400' },
])
const usage = computed(() => [
  { label: t('admin.workbench.overview.modelCost'), value: officeBilling(props.overview.model_charge_credits) },
  { label: t('admin.workbench.overview.searchCost'), value: officeBilling(props.overview.search_charge_credits) },
  { label: t('admin.workbench.overview.tokens'), value: `${number(props.overview.input_tokens)} / ${number(props.overview.output_tokens)}` },
])
</script>
