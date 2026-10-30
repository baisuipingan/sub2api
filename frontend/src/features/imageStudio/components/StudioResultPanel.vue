<template>
  <section
    class="min-h-[420px] min-w-0 bg-gray-50/80 px-4 py-5 dark:bg-dark-900/40 sm:px-7 lg:h-full lg:min-h-0 lg:overflow-y-auto"
    :aria-label="t('imageStudio.resultPanel.aria')"
  >
    <div class="mb-5 flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <button
          v-if="showFavorites && activeCollectionId"
          type="button"
          class="mb-2 inline-flex items-center gap-1 text-sm text-gray-500 transition hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-400"
          @click="backToCollections"
        >
          <StudioIcon name="ChevronLeftIcon" class="h-4 w-4" />{{ t('imageStudio.resultPanel.collections') }}
        </button>
        <p class="text-xs font-medium uppercase tracking-[0.18em] text-gray-400">
          {{ t('imageStudio.resultPanel.workspace') }}
        </p>
        <h2 class="mt-1 truncate text-xl font-semibold text-gray-950 dark:text-white">
          {{ heading }}
        </h2>
        <p
          v-if="showFavorites && activeCollectionId && activeCollection"
          class="mt-1 text-sm text-gray-500 dark:text-dark-400"
        >
          {{ t('imageStudio.resultPanel.taskCount', { count: activeCollection.taskCount }) }}
        </p>
      </div>

      <div
        role="tablist"
        :aria-label="t('imageStudio.resultPanel.viewAria')"
        class="flex shrink-0 items-center rounded-lg border border-gray-200 bg-white p-1 dark:border-white/[0.1] dark:bg-white/[0.04]"
      >
        <button
          type="button"
          role="tab"
          class="rounded-md px-3 py-1.5 text-sm transition"
          :class="
            !showFavorites
              ? 'bg-gray-100 font-medium text-gray-900 shadow-sm dark:bg-white/[0.1] dark:text-white'
              : 'text-gray-500 hover:text-gray-800 dark:text-dark-400'
          "
          :aria-selected="!showFavorites"
          @click="switchView(false)"
        >
          {{ t('imageStudio.resultPanel.history') }}
        </button>
        <button
          type="button"
          role="tab"
          class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition"
          :class="
            showFavorites
              ? 'bg-gray-100 font-medium text-gray-900 shadow-sm dark:bg-white/[0.1] dark:text-white'
              : 'text-gray-500 hover:text-gray-800 dark:text-dark-400'
          "
          :aria-label="t('imageStudio.resultPanel.favorites')"
          :aria-selected="showFavorites"
          @click="switchView(true)"
        >
          <StudioIcon
            name="FavoriteIcon"
            class="h-4 w-4 text-amber-500"
            :class="favoriteTaskCount > 0 ? 'fill-current' : ''"
          />
          <span>{{ t('imageStudio.resultPanel.favorites') }}</span>
        </button>
      </div>
    </div>

    <!-- 收藏视图 -->
    <template v-if="showFavorites">
      <template v-if="activeCollectionId">
        <StudioActiveResult v-if="activeTask" :key="activeTask.id" :task="activeTask" />
        <StudioSearchBar :favorite-collection-id="activeCollectionId" />
        <StudioTaskGrid
          :favorite-collection-id="activeCollectionId"
          :selected-task-id="activeTaskId"
          :on-select-task="setActiveTaskId"
        />
      </template>
      <StudioFavoriteCollectionsView v-else @open-collection="openCollection" />
    </template>

    <!-- 历史任务视图：有任务 -->
    <template v-else-if="state.tasks.length">
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
import StudioIcon from './StudioIcon.vue'
import StudioActiveResult from './StudioActiveResult.vue'
import StudioSearchBar from './StudioSearchBar.vue'
import StudioTaskGrid from './StudioTaskGrid.vue'
import StudioFavoriteCollectionsView from './favorites/StudioFavoriteCollectionsView.vue'
import {
  storeState as state,
  taskMatchesFilterStatus,
  taskMatchesSearchQuery,
} from '../stores/store'
import { ALL_FAVORITES_COLLECTION_ID } from '../lib/favoriteState'
import { getCollectionTasks, taskMatchesFavoriteCollection } from './favorites/favoriteUtils'

const { t } = useI18n()

const showFavorites = ref(false)
const activeCollectionId = ref<string | null>(null)
const activeTaskId = ref<string | null>(null)
const noticeTaskId = ref<string | null>(null)

/** 上一次渲染的任务快照，用于检测「任务刚完成」并提示 */
const previousTasks = ref(state.tasks)

const sortedTasks = computed(() =>
  [...state.tasks].sort((a, b) => b.createdAt - a.createdAt),
)

const favoriteTaskCount = computed(() => state.tasks.filter((task) => task.isFavorite).length)

const activeTask = computed(
  () => state.tasks.find((task) => task.id === activeTaskId.value) ?? null,
)

const activeCollection = computed(() => {
  if (activeCollectionId.value === ALL_FAVORITES_COLLECTION_ID) {
    return { name: t('imageStudio.resultPanel.allFavorites'), taskCount: favoriteTaskCount.value }
  }
  if (
    activeCollectionId.value &&
    state.favoriteCollections.some((c) => c.id === activeCollectionId.value)
  ) {
    const found = state.favoriteCollections.find((c) => c.id === activeCollectionId.value)!
    return {
      name: found.name,
      taskCount: getCollectionTasks(
        activeCollectionId.value,
        state.tasks,
        state.defaultFavoriteCollectionId,
      ).length,
    }
  }
  return null
})

const heading = computed(() => {
  if (showFavorites.value) {
    return activeCollection.value?.name ?? t('imageStudio.resultPanel.favorites')
  }
  return t('imageStudio.resultPanel.generatedResults')
})

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
  if (
    showFavorites.value &&
    activeCollectionId.value &&
    !taskMatchesFavoriteCollection(task, activeCollectionId.value, state.defaultFavoriteCollectionId)
  ) {
    return true
  }
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
    showFavorites.value = false
    activeCollectionId.value = null
    activeTaskId.value = id
    noticeTaskId.value = null
    state.setSearchQuery('')
    state.setFilterStatus('all')
  },
)

/** 当前收藏夹被删除时退回列表 */
watch(
  () => state.favoriteCollections,
  (collections) => {
    const id = activeCollectionId.value
    if (!id || id === ALL_FAVORITES_COLLECTION_ID) return
    if (collections.some((c) => c.id === id)) return
    activeCollectionId.value = null
    state.setSearchQuery('')
  },
)

function switchView(favorites: boolean) {
  showFavorites.value = favorites
  activeCollectionId.value = null
  state.setSearchQuery('')
  state.setFilterStatus('all')
}

function backToCollections() {
  activeCollectionId.value = null
  state.setSearchQuery('')
  state.setFilterStatus('all')
}

function openCollection(collectionId: string) {
  state.setSearchQuery('')
  state.setFilterStatus('all')
  activeCollectionId.value = collectionId
}

function revealTask(taskId: string) {
  switchView(false)
  activeTaskId.value = taskId
  noticeTaskId.value = null
}

function setActiveTaskId(taskId: string) {
  activeTaskId.value = taskId
}
</script>
