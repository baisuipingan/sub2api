<template>
  <section
    class="min-h-[420px] min-w-0 bg-white dark:bg-dark-950 lg:flex lg:h-full lg:min-h-0 lg:flex-col lg:overflow-hidden"
    :aria-label="t('imageStudio.resultPanel.aria')"
  >
    <!-- ── 顶部工具栏：标题 + 搜索 + 状态筛选收成一行 ──────────────
         之前是「小标题 + 大标题 + 独立搜索框 + 分段按钮 + 重复的下拉框」，
         同一个筛选项渲染两套控件，层级也散。现在压成一条。 -->
    <header
      class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-gray-100 px-4 py-2.5 dark:border-white/[0.06] sm:px-6"
    >
      <h2 class="shrink-0 text-sm font-semibold text-gray-900 dark:text-white">
        {{ heading }}
      </h2>

      <span
        v-if="state.tasks.length"
        class="shrink-0 rounded-md bg-gray-100 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-gray-500 dark:bg-white/[0.06] dark:text-dark-400"
      >
        {{ state.tasks.length }}
      </span>

      <!-- 搜索：手机上换行占满，桌面上靠右并限制宽度 -->
      <div class="order-last w-full min-w-0 sm:order-none sm:ml-auto sm:max-w-72 sm:flex-1">
        <label class="relative block">
          <StudioIcon
            name="SearchIcon"
            class="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400"
          />
          <input
            :aria-label="t('imageStudio.search.ariaHistory')"
            :value="state.searchQuery"
            :placeholder="t('imageStudio.search.placeholder')"
            class="w-full rounded-lg border border-transparent bg-gray-100 py-1.5 pl-8 pr-3 text-xs text-gray-900 outline-none transition placeholder:text-gray-400 focus:border-primary-400 focus:bg-white focus:ring-2 focus:ring-primary-500/15 dark:bg-white/[0.06] dark:text-white dark:focus:bg-dark-900"
            @input="state.setSearchQuery(($event.target as HTMLInputElement).value)"
          />
        </label>
      </div>

      <!-- 状态筛选：只保留分段控件，原来的下拉框是同一功能的重复入口 -->
      <div
        class="task-filter-segments flex shrink-0 items-center gap-0.5 rounded-lg bg-gray-100 p-0.5 dark:bg-white/[0.06]"
        role="group"
        :aria-label="t('imageStudio.search.filterLabel')"
      >
        <button
          v-for="status in statuses"
          :key="status"
          type="button"
          class="rounded-md px-2 py-1 text-[11px] font-medium transition"
          :class="
            state.filterStatus === status
              ? 'bg-white text-gray-900 shadow-sm dark:bg-white/[0.12] dark:text-white'
              : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-dark-200'
          "
          :aria-pressed="state.filterStatus === status"
          @click="state.setFilterStatus(status)"
        >
          {{ t(`imageStudio.search.status.${status}`) }}
        </button>
      </div>

      <button
        v-if="failedCount > 0"
        type="button"
        class="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-gray-400 transition hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-500/10 dark:hover:text-red-400"
        :title="t('imageStudio.search.clearFailed')"
        @click="clearFailed"
      >
        <StudioIcon name="TrashIcon" class="h-3.5 w-3.5" />
        {{ t('imageStudio.search.clearFailed') }}
      </button>
    </header>

    <!-- 新任务完成提示 -->
    <div
      v-if="noticeTask"
      role="status"
      class="flex items-center justify-between gap-2 border-b border-primary-100 bg-primary-50/70 px-4 py-1.5 text-xs text-primary-700 dark:border-primary-500/20 dark:bg-primary-500/10 dark:text-primary-300 sm:px-6"
    >
      <span class="truncate">{{ noticeText }}</span>
      <button type="button" class="shrink-0 font-medium hover:underline" @click="revealTask(noticeTask.id)">
        {{ t('imageStudio.resultPanel.viewTask') }}
      </button>
    </div>

    <!-- ── 有任务：主预览 + 底部缩略图轨道 ───────────────────────── -->
    <template v-if="state.tasks.length">
      <div
        data-history-task-scroll
        role="region"
        :aria-label="t('imageStudio.resultPanel.historyListAria')"
        tabindex="0"
        class="min-h-0 flex-1 overflow-y-auto overscroll-contain lg:overflow-y-auto"
      >
        <StudioActiveResult v-if="activeTask" :key="activeTask.id" :task="activeTask" />

        <!-- 没有选中任务时用网格铺满，避免整片空白 -->
        <div v-else class="p-4 sm:p-6">
          <StudioTaskGrid
            variant="grid"
            :selected-task-id="activeTaskId"
            :on-select-task="setActiveTaskId"
          />
        </div>
      </div>

      <p
        v-if="hiddenSelection"
        role="status"
        class="shrink-0 border-t border-gray-100 px-4 py-1.5 text-[11px] text-gray-500 dark:border-white/[0.06] sm:px-6"
      >
        {{ t('imageStudio.resultPanel.hiddenSelection') }}
      </p>

      <!-- 缩略图轨道：历史任务横向铺开，点一下换预览 -->
      <footer
        class="shrink-0 border-t border-gray-100 bg-gray-50/60 px-3 py-2.5 dark:border-white/[0.06] dark:bg-white/[0.02] sm:px-4"
      >
        <StudioTaskGrid
          variant="strip"
          :selected-task-id="activeTaskId"
          :on-select-task="setActiveTaskId"
        />
      </footer>
    </template>

    <!-- ── 空状态 ───────────────────────────────────────────────── -->
    <div v-else class="flex flex-1 items-center justify-center px-6 py-12">
      <div class="max-w-lg text-center">
        <div
          class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-50 text-2xl text-primary-500 dark:bg-primary-500/10 dark:text-primary-400"
        >
          ✦
        </div>
        <h3 class="mt-4 text-base font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.resultPanel.emptyTitle') }}
        </h3>
        <p class="mt-1.5 text-sm leading-6 text-gray-500 dark:text-dark-400">
          {{ t('imageStudio.resultPanel.emptyHint') }}
        </p>
        <div class="mt-5 grid gap-2 sm:grid-cols-2">
          <button
            v-for="prompt in examplePrompts"
            :key="prompt"
            type="button"
            class="rounded-xl border border-gray-200 bg-white px-3 py-2.5 text-left text-xs leading-5 text-gray-600 transition hover:border-primary-300 hover:text-primary-700 dark:border-white/[0.08] dark:bg-white/[0.03] dark:text-dark-300 dark:hover:border-primary-500/40"
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
/**
 * 生图结果面板。
 *
 * 布局取向：**图片优先**。
 * 顶部一条工具栏承载所有筛选，中间整块留给预览，底部缩略图轨道负责切换。
 *
 * 之前的问题（也是「右边很丑」的主因）：
 *   1. 搜索框、分段按钮、下拉框三个控件表达同一件事，视觉噪音大
 *   2. 任务卡片是「横条 + 64px 缩略图」，只有一条记录时右侧近 1000px 全是空白
 *   3. task-grid 这个 class 没有任何样式定义，卡片直接堆成一列
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import StudioActiveResult from './StudioActiveResult.vue'
import StudioTaskGrid from './StudioTaskGrid.vue'
import {
  removeMultipleTasks,
  storeState as state,
  taskMatchesFilterStatus,
  taskMatchesSearchQuery,
} from '../stores/store'

const { t } = useI18n()

const activeTaskId = ref<string | null>(null)
const noticeTaskId = ref<string | null>(null)

const statuses = ['all', 'done', 'running', 'error'] as const

/** 上一次渲染的任务快照，用于检测「任务刚完成」并提示 */
const previousTasks = ref(state.tasks)

const heading = computed(() => t('imageStudio.resultPanel.generatedResults'))

const activeTask = computed(
  () => state.tasks.find((task) => task.id === activeTaskId.value) ?? null,
)

/** 失败任务数，决定是否显示「清除失败」 */
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
    confirmText: t('imageStudio.search.clearFailed'),
    tone: 'danger',
    action: () => removeMultipleTasks(ids),
  })
}

const noticeTask = computed(
  () => state.tasks.find((task) => task.id === noticeTaskId.value && task.id !== activeTaskId.value),
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

/** 选中任务被删除时跳到相邻任务；有新任务完成时提示 */
watch(
  () => state.tasks,
  (tasks) => {
    const prev = previousTasks.value
    previousTasks.value = tasks

    if (activeTaskId.value && !tasks.some((task) => task.id === activeTaskId.value)) {
      activeTaskId.value = tasks[0]?.id ?? null
    }

    const finished = tasks.find((task) => {
      const before = prev.find((item) => item.id === task.id)
      return before && before.status === 'running' && task.status !== 'running'
    })
    if (finished) noticeTaskId.value = finished.id
  },
  { deep: false },
)

/** 首次进来默认选中最新任务，避免主区域空白 */
watch(
  () => state.tasks.length,
  (length) => {
    if (length > 0 && !activeTaskId.value) activeTaskId.value = state.tasks[0].id
  },
  { immediate: true },
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
