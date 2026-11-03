<template>
  <section
    class="min-h-[420px] min-w-0 bg-gray-50/80 px-4 py-5 dark:bg-dark-900/40 sm:px-7 lg:h-full lg:min-h-0 lg:overflow-y-auto"
    :aria-label="t('imageStudio.resultPanel.aria')"
  >
    <div class="mb-5 flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <p class="text-xs font-medium uppercase tracking-[0.18em] text-gray-400">
          {{ t('imageStudio.resultPanel.workspace') }}
        </p>
        <h2 class="mt-1 truncate text-xl font-semibold text-gray-950 dark:text-white">
          {{ heading }}
        </h2>
      </div>
    </div>

    <!-- 历史任务视图：有任务 -->
    <template v-if="state.tasks.length">
      <StudioActiveResult v-if="activeTask" :key="activeTask.id" :task="activeTask" />

      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-sm font-semibold text-gray-800 dark:text-dark-100">
          {{ t('imageStudio.resultPanel.history') }}
        </h3>
        <span class="text-xs text-gray-400">
          {{ t('imageStudio.resultPanel.taskCount', { count: state.tasks.length }) }}
        </span>
      </div>

      <div
        v-if="noticeTask"
        role="status"
        class="mb-3 flex items-center justify-between gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs text-gray-600 dark:border-white/[0.1] dark:bg-white/[0.04] dark:text-dark-300"
      >
        <span>{{ noticeText }}</span>
        <button
          type="button"
          class="shrink-0 font-medium text-primary-600 hover:underline"
          @click="revealTask(noticeTask.id)"
        >
          {{ t('imageStudio.resultPanel.viewTask') }}
        </button>
      </div>

      <StudioSearchBar />

      <p v-if="hiddenSelection" role="status" class="mt-3 text-xs leading-5 text-gray-500">
        {{ t('imageStudio.resultPanel.hiddenSelection') }}
      </p>

      <div
        data-history-task-scroll
        role="region"
        :aria-label="t('imageStudio.resultPanel.historyListAria')"
        tabindex="0"
        class="mt-3 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 lg:max-h-[min(42dvh,30rem)] lg:overflow-y-auto lg:overscroll-contain lg:pr-1 lg:[scrollbar-gutter:stable]"
      >
        <StudioTaskGrid :selected-task-id="activeTaskId" :on-select-task="setActiveTaskId" />
      </div>
    </template>

    <!-- 历史任务视图：空状态 -->
    <div v-else class="flex min-h-[560px] items-center justify-center py-12 text-center">
      <div class="max-w-xl">
        <div
          class="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl bg-primary-50 text-3xl dark:bg-primary-500/10"
        >
          ✦
        </div>
        <h3 class="mt-5 text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.resultPanel.emptyTitle') }}
        </h3>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">
          {{ t('imageStudio.resultPanel.emptyHint') }}
        </p>
        <div class="mt-5 grid gap-2 sm:grid-cols-2">
          <button
            v-for="prompt in examplePrompts"
            :key="prompt"
            type="button"
            class="rounded-xl border border-gray-200 bg-white px-3 py-3 text-left text-sm text-gray-600 transition hover:border-primary-300 hover:text-primary-700 dark:border-white/[0.08] dark:bg-white/[0.03] dark:text-dark-300"
            @click="state.setPrompt(prompt)"
          >
            {{ prompt }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioActiveResult from './StudioActiveResult.vue'
import StudioSearchBar from './StudioSearchBar.vue'
import StudioTaskGrid from './StudioTaskGrid.vue'
import {
  storeState as state,
  taskMatchesFilterStatus,
  taskMatchesSearchQuery,
} from '../stores/store'

const { t } = useI18n()

const activeTaskId = ref<string | null>(null)
const noticeTaskId = ref<string | null>(null)

/** 上一次渲染的任务快照，用于检测「任务刚完成」并提示 */
const previousTasks = ref(state.tasks)

const sortedTasks = computed(() =>
  [...state.tasks].sort((a, b) => b.createdAt - a.createdAt),
)

const activeTask = computed(
  () => state.tasks.find((task) => task.id === activeTaskId.value) ?? null,
)

const heading = computed(() => t('imageStudio.resultPanel.generatedResults'))

const noticeTask = computed(() =>
  state.tasks.find((task) => task.id === noticeTaskId.value && task.id !== activeTaskId.value),
)

const noticeText = computed(() => {
  const task = noticeTask.value
  if (!task) return ''
  if (task.status === 'done') {
    return task.outputErrors?.length
      ? t('imageStudio.resultPanel.partialDone')
      : t('imageStudio.resultPanel.done')
  }
  if (task.status === 'error') return t('imageStudio.resultPanel.failed')
  return ''
})

/** 当前选中任务是否被筛选条件隐藏 */
const hiddenSelection = computed(() => {
  const task = activeTask.value
  if (!task) return false
  if (!taskMatchesFilterStatus(task, state.filterStatus)) return true
  if (!taskMatchesSearchQuery(task, state.searchQuery)) return true
  return false
})

const examplePrompts = computed(() => [
  t('imageStudio.resultPanel.example1'),
  t('imageStudio.resultPanel.example2'),
  t('imageStudio.resultPanel.example3'),
  t('imageStudio.resultPanel.example4'),
])

/**
 * 维护「当前选中任务」：
 * - 选中任务被删除 → 自动跳到相邻任务
 * - 有任务刚跑完 → 弹提示
 */
watch(
  () => state.tasks,
  (tasks) => {
    const previous = [...previousTasks.value].sort((a, b) => b.createdAt - a.createdAt)

    if (!activeTaskId.value || !tasks.some((task) => task.id === activeTaskId.value)) {
      const index = previous.findIndex((task) => task.id === activeTaskId.value)
      const neighbors =
        index < 0
          ? []
          : [...previous.slice(index + 1), ...previous.slice(0, index).reverse()]
      activeTaskId.value =
        neighbors.find((task) => tasks.some((item) => item.id === task.id))?.id ??
        sortedTasks.value[0]?.id ??
        null
    }

    const completed = tasks.find(
      (task) =>
        task.status !== 'running' &&
        task.id !== activeTaskId.value &&
        previous.some((item) => item.id === task.id && item.status === 'running'),
    )
    if (completed) noticeTaskId.value = completed.id

    previousTasks.value = tasks
  },
  { deep: false },
)

/** 新提交任务时跳到该任务 */
watch(
  () => state.latestSubmittedTaskId,
  (id) => {
    if (!id) return
    activeTaskId.value = id
    noticeTaskId.value = null
    state.setSearchQuery('')
    state.setFilterStatus('all')
  },
)

function revealTask(taskId: string) {
  activeTaskId.value = taskId
  noticeTaskId.value = null
}

function setActiveTaskId(taskId: string) {
  activeTaskId.value = taskId
}
</script>
