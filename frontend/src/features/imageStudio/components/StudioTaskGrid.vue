<template>
  <div
    v-if="!filteredTasks.length"
    class="py-20 text-center text-gray-400 dark:text-dark-500"
  >
    <template v-if="hasFilter">
      <p class="text-sm">{{ emptyFilterText }}</p>
      <button
        type="button"
        class="mt-3 text-sm font-medium text-primary-600 hover:underline"
        @click="clearFilters"
      >
        {{ t('imageStudio.grid.clearFilter') }}
      </button>
    </template>

    <template v-else>
      <StudioIcon name="ImageUploadIcon" class="mx-auto mb-4 h-16 w-16 text-gray-200 dark:text-dark-700" />
      <p class="text-sm">{{ t('imageStudio.grid.emptyTasks') }}</p>
    </template>
  </div>

  <div v-else data-task-grid-root class="task-grid grid items-start gap-2 pb-2">
    <StudioTaskCard
      v-for="task in filteredTasks"
      :key="task.id"
      :task="task"
      :is-selected="task.id === selectedTaskId"
      @select="onSelect(task)"
      @reuse="reuseConfig(task)"
      @edit-outputs="editOutputs(task)"
      @delete="confirmDelete(task)"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import StudioTaskCard from './StudioTaskCard.vue'
import type { TaskRecord } from '../types'
import {
  storeState as state,
  reuseConfig,
  editOutputs,
  removeTask,
  taskMatchesFilterStatus,
  taskMatchesSearchQuery,
} from '../stores/store'

const props = defineProps<{
  selectedTaskId?: string | null
  onSelectTask?: (taskId: string) => void
}>()

const { t } = useI18n()

const filteredTasks = computed(() => {
  const q = state.searchQuery.trim().toLowerCase()
  return [...state.tasks]
    .sort((a, b) => b.createdAt - a.createdAt)
    .filter(
      (task) =>
        taskMatchesFilterStatus(task, state.filterStatus) && taskMatchesSearchQuery(task, q),
    )
})

const hasFilter = computed(
  () => Boolean(state.searchQuery.trim()) || state.filterStatus !== 'all',
)

const emptyFilterText = computed(() => {
  if (state.searchQuery.trim()) {
    return t('imageStudio.grid.noMatchTask')
  }
  return t('imageStudio.grid.noStatusTask', {
    status: t(`imageStudio.search.status.${state.filterStatus}`),
  })
})

function clearFilters() {
  state.setSearchQuery('')
  state.setFilterStatus('all')
}

function onSelect(task: TaskRecord) {
  if (props.onSelectTask) props.onSelectTask(task.id)
  else state.setDetailTaskId(task.id)
}

function confirmDelete(task: TaskRecord) {
  state.setConfirmDialog({
    title: t('imageStudio.grid.deleteTitle'),
    message: t('imageStudio.grid.deleteMessage'),
    cancelText: t('imageStudio.common.cancel'),
    confirmText: t('imageStudio.task.delete'),
    tone: 'danger',
    action: () => removeTask(task),
  })
}
</script>
