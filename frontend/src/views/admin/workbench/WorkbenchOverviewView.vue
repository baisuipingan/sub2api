<template>
  <AppLayout>
    <div class="space-y-5" :aria-busy="loading" data-testid="workbench-overview">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.overviewTitle') }}</h2>
        <div class="flex flex-wrap items-center gap-2">
          <div class="w-36 sm:w-40">
            <Select :model-value="days" :options="periodOptions" :aria-label="t('admin.workbench.period')" @update:model-value="days = Number($event) as WorkbenchDays" />
          </div>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
            <Icon name="refresh" class="mr-1.5" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}
          </button>
        </div>
      </div>

      <LoadingSpinner v-if="loading" />
      <template v-else>
        <WorkbenchChatOverview v-if="chat" :overview="chat" />
        <p v-else class="card p-4 text-sm text-red-600 dark:text-red-400" role="alert">{{ t('admin.workbench.overview.chatLoadFailed') }}</p>

        <section v-if="images" class="card p-4 sm:p-5" aria-labelledby="workbench-image-overview">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 id="workbench-image-overview" class="font-semibold text-gray-900 dark:text-white">{{ t('nav.imageStudio') }}</h3>
            <div class="flex items-center gap-4 text-sm text-primary-600 dark:text-primary-400">
              <RouterLink to="/admin/workbench/tasks" class="hover:underline">{{ t('admin.workbench.viewTasks') }}</RouterLink>
              <RouterLink to="/admin/workbench/gallery" class="hover:underline">{{ t('admin.workbench.galleryBrowse') }}</RouterLink>
            </div>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
            <div v-for="item in imageStats" :key="item.label">
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ item.label }}</dt>
              <dd class="mt-1 break-words text-lg font-semibold tabular-nums">{{ item.value }}</dd>
            </div>
          </dl>
          <p class="mt-3 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.overview.imageScope') }}</p>
        </section>
        <p v-else class="card p-4 text-sm text-red-600 dark:text-red-400" role="alert">{{ t('admin.workbench.overview.imageLoadFailed') }}</p>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getWorkbenchChatOverview, getWorkbenchOverview, type WorkbenchChatOverview as ChatOverview, type WorkbenchDays, type WorkbenchOverview } from '@/api/admin/workbench'
import { formatBytes } from '@/utils/format'
import WorkbenchChatOverview from './WorkbenchChatOverview.vue'

const { t, locale } = useI18n()
const days = ref<WorkbenchDays>(7)
const chat = ref<ChatOverview>()
const images = ref<WorkbenchOverview>()
const loading = ref(true)
let controller: AbortController | undefined

const periodOptions = computed(() => ([7, 30, 90] as const).map(value => ({
  value, label: t('admin.workbench.days', { days: value }),
})))
const imageStats = computed(() => {
  const stats = images.value
  if (!stats) return []
  const percent = new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 })
  return [
    { label: t('admin.workbench.totalTasks'), value: stats.tasks.total.toLocaleString(locale.value) },
    { label: t('admin.workbench.successRate'), value: stats.success_rate == null ? '—' : percent.format(stats.success_rate) },
    { label: t('admin.workbench.overview.galleryCount'), value: stats.gallery.output_images.toLocaleString(locale.value) },
    { label: t('admin.workbench.overview.galleryStorage'), value: formatBytes(stats.gallery.used_bytes, 1) },
  ]
})

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  const [chatResult, imageResult] = await Promise.allSettled([
    getWorkbenchChatOverview(days.value, request.signal),
    getWorkbenchOverview(days.value, request.signal),
  ])
  if (request.signal.aborted) return
  chat.value = chatResult.status === 'fulfilled' ? chatResult.value : undefined
  images.value = imageResult.status === 'fulfilled' ? imageResult.value : undefined
  loading.value = false
}

watch(days, load, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
