<template>
  <!-- 小屏在预览下方横向滚动；桌面在右侧纵向滚动并可收起。 -->
  <aside
    data-testid="studio-history-rail"
    class="flex w-full min-w-0 shrink-0 flex-col border-t border-gray-100 bg-white lg:min-h-0 lg:border-l lg:border-t-0 lg:transition-[width] lg:duration-200 dark:border-white/[0.06] dark:bg-dark-950"
    :class="collapsed ? 'lg:w-10' : 'lg:w-[92px]'"
    :aria-label="t('imageStudio.history.aria')"
  >
    <!-- 栏头 -->
    <div
      class="flex shrink-0 items-center justify-between gap-1 border-b border-gray-100 px-3 py-2 dark:border-white/[0.06] lg:py-2.5"
      :class="collapsed ? 'lg:justify-center lg:px-1' : 'lg:px-2'"
    >
      <div class="flex min-w-0 items-center gap-1.5" :class="{ 'lg:hidden': collapsed }">
        <span class="whitespace-nowrap text-xs font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.history.shortTitle') }}
        </span>
        <span
          class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-gray-500 dark:bg-white/[0.08] dark:text-dark-400"
        >
          {{ items.length }}
        </span>
      </div>

      <button
        type="button"
        class="hidden h-7 w-7 shrink-0 items-center justify-center rounded-md text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 lg:flex dark:hover:bg-white/[0.08] dark:hover:text-dark-200"
        :title="collapsed ? t('imageStudio.history.expand') : t('imageStudio.history.collapse')"
        :aria-label="collapsed ? t('imageStudio.history.expand') : t('imageStudio.history.collapse')"
        :aria-expanded="!collapsed"
        @click="emit('toggle')"
      >
        <StudioIcon :name="collapsed ? 'ChevronLeftIcon' : 'ChevronRightIcon'" class="h-4 w-4" />
      </button>
    </div>

    <!-- 收起态：只显示数量，竖排提示 -->
    <div v-if="collapsed" class="hidden flex-1 flex-col items-center gap-2 pt-3 lg:flex">
      <span class="text-[10px] tabular-nums text-gray-400">{{ items.length }}</span>
      <span
        class="text-[10px] leading-tight tracking-widest text-gray-400"
        style="writing-mode: vertical-rl; text-orientation: mixed"
      >
        {{ t('imageStudio.history.title') }}
      </span>
    </div>

    <!-- 小屏始终展开；桌面仅展开时显示。 -->
    <div
      data-testid="studio-history-items"
      class="flex min-w-0 gap-2 overflow-x-auto overscroll-contain px-3 py-2 [scrollbar-width:thin] lg:min-h-0 lg:flex-1 lg:flex-col lg:gap-1.5 lg:overflow-x-hidden lg:overflow-y-auto lg:px-1.5"
      :class="{ 'lg:hidden': collapsed }"
    >
      <p v-if="!items.length" class="px-1 py-6 text-center text-[11px] leading-5 text-gray-400">
        {{ t('imageStudio.history.empty') }}
      </p>

      <button
        v-for="item in items"
        :key="item.task.id"
        :data-studio-history-task-id="item.task.id"
        type="button"
        class="group relative block w-16 shrink-0 overflow-hidden rounded-lg ring-1 ring-inset transition lg:w-full"
        :class="
          item.task.id === activeTaskId
            ? 'ring-2 ring-primary-500'
            : 'ring-black/[0.06] hover:ring-primary-300 dark:ring-white/10 dark:hover:ring-primary-500/50'
        "
        :aria-label="item.label"
        :aria-current="item.task.id === activeTaskId ? 'true' : undefined"
        @click="emit('select', item.task.id, item.imageId)"
      >
        <span class="block aspect-square w-full bg-gray-100 dark:bg-white/[0.06]">
          <StudioImageThumbnail :image-id="item.imageId" :alt="item.label" />
        </span>

        <!-- 状态点：生成中转圈，失败标红 -->
        <LoadingSpinner
          v-if="item.task.status === 'running'"
          size="sm"
          class="absolute right-1.5 top-1.5"
        />
        <span
          v-else-if="item.task.status === 'error'"
          class="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-red-500 ring-2 ring-white/80 dark:ring-black/50"
        />

        <!-- 悬停显示时间，平时不打扰 -->
        <span
          class="pointer-events-none absolute inset-x-0 bottom-0 translate-y-full bg-gradient-to-t from-black/70 to-transparent px-1.5 pb-1 pt-4 text-[10px] text-white opacity-0 transition-all duration-150 group-hover:translate-y-0 group-hover:opacity-100"
        >
          {{ item.timeLabel }}
        </span>
      </button>
    </div>
  </aside>
</template>

<script setup lang="ts">
/**
 * 生成历史栏。
 *
 * 小屏在预览下方横向排列，桌面位于右侧并支持收起。
 * 收起状态由父组件持久化，仅影响桌面布局。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'
import StudioIcon from './StudioIcon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StudioImageThumbnail from './StudioImageThumbnail.vue'

const props = defineProps<{
  tasks: TaskRecord[]
  activeTaskId: string | null
  collapsed: boolean
}>()

const { t } = useI18n()

interface HistoryItem {
  task: TaskRecord
  imageId: string
  label: string
  timeLabel: string
}

/** 每个任务取第一张图作为代表；同一任务多图时也只占一格 */
const items = computed<HistoryItem[]>(() =>
  props.tasks
    .filter((task) => task.outputImages.length > 0 || task.status === 'running' || task.status === 'error')
    .map((task) => ({
      task,
      imageId: task.outputImages[0] ?? '',
      label: task.prompt?.slice(0, 40) || t('imageStudio.task.noPrompt'),
      timeLabel: new Date(task.createdAt).toLocaleTimeString(undefined, {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }),
    })),
)

const emit = defineEmits<{
  select: [taskId: string, imageId: string]
  toggle: []
}>()
</script>
