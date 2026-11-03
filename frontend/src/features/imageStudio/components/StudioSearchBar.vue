<template>
  <input
    :aria-label="ariaLabel"
    :value="state.searchQuery"
    :placeholder="t('imageStudio.search.placeholder')"
    class="w-full min-w-0 rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/15 dark:border-white/[0.1] dark:bg-white/[0.04] dark:text-white"
    @input="state.setSearchQuery(($event.target as HTMLInputElement).value)"
  />
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div
      class="task-filter-segments items-center gap-1"
      role="group"
      :aria-label="t('imageStudio.search.filterLabel')"
    >
      <button
        v-for="status in statuses"
        :key="status"
        type="button"
        class="rounded-md px-2.5 py-2 text-xs transition"
        :class="
          state.filterStatus === status
            ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
            : 'text-gray-500 hover:bg-gray-100 dark:hover:bg-white/[0.06]'
        "
        :aria-pressed="state.filterStatus === status"
        @click="state.setFilterStatus(status)"
      >
        {{ t(`imageStudio.search.status.${status}`) }}
      </button>
    </div>

    <select
      :aria-label="t('imageStudio.search.filterLabel')"
      :value="state.filterStatus"
      class="task-filter-select rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs dark:border-white/[0.1] dark:bg-white/[0.04] dark:text-white"
      @change="state.setFilterStatus(($event.target as HTMLSelectElement).value as TaskFilterStatus)"
    >
      <option v-for="status in statuses" :key="status" :value="status">
        {{ t(`imageStudio.search.status.${status}`) }}
      </option>
    </select>

    <button
      v-if="failedCount > 0"
      type="button"
      class="ml-auto flex h-8 items-center justify-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-gray-500 hover:bg-red-50 hover:text-red-600 dark:text-dark-400 dark:hover:bg-red-500/10 dark:hover:text-red-400"
      :aria-label="t('imageStudio.search.clearFailed')"
      :title="t('imageStudio.search.clearFailed')"
      @click="clearFailed"
    >
      <StudioIcon name="TrashIcon" class="h-4 w-4" />
      {{ t('imageStudio.search.clearFailed') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import { storeState as state, clearFailedTasks, taskMatchesFilterStatus, taskMatchesSearchQuery } from '../stores/store'

const { t } = useI18n()

type TaskFilterStatus = 'all' | 'done' | 'running' | 'error'
const statuses: TaskFilterStatus[] = ['all', 'done', 'running', 'error']

const ariaLabel = computed(() => t('imageStudio.search.ariaHistory'))

/** 当前筛选范围内的失败条数 */
const failedCount = computed(
  () =>
    state.tasks.filter(
      (task) =>
        taskMatchesFilterStatus(task, 'error') &&
        taskMatchesSearchQuery(task, state.searchQuery.trim().toLowerCase()),
    ).length,
)

function clearFailed() {
  if (!failedCount.value) return
  const query = state.searchQuery.trim().toLowerCase()
  const ids = state.tasks
    .filter((task) => taskMatchesFilterStatus(task, 'error') && taskMatchesSearchQuery(task, query))
    .map((task) => task.id)

  state.setConfirmDialog({
    title: t('imageStudio.search.clearFailedTitle'),
    message: t('imageStudio.search.clearFailedMessage', { count: ids.length }),
    confirmText: t('imageStudio.search.clearFailedConfirm'),
    cancelText: t('imageStudio.common.cancel'),
    tone: 'danger',
    action: () => clearFailedTasks(ids),
  })
}
</script>
