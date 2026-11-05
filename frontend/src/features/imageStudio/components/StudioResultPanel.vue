<template>
  <section
    class="flex min-h-[420px] min-w-0 bg-white dark:bg-dark-950 lg:h-full lg:min-h-0"
    :aria-label="t('imageStudio.resultPanel.aria')"
  >
    <!-- ══════════ 中间：预览区（不滚动） ══════════
         顶部工具栏 + 固定方形相框 + 相框下操作键，全部塞进一屏，
         高度按视口算，永不产生纵向滚动。 -->
    <div class="flex min-w-0 flex-1 flex-col lg:min-h-0">
      <header
        class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b border-gray-100 px-4 py-2.5 dark:border-white/[0.06] sm:px-6"
      >
        <h2 class="shrink-0 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.resultPanel.generatedResults') }}
        </h2>
        <span
          v-if="state.tasks.length"
          class="shrink-0 rounded-md bg-gray-100 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-gray-500 dark:bg-white/[0.06] dark:text-dark-400"
        >
          {{ state.tasks.length }}
        </span>

        <div class="order-last w-full min-w-0 sm:order-none sm:ml-auto sm:max-w-64 sm:flex-1">
          <label for="studio-history-search" class="sr-only">{{ t('imageStudio.search.ariaHistory') }}</label>
          <Input
            id="studio-history-search"
            :model-value="state.searchQuery"
            :placeholder="t('imageStudio.search.placeholder')"
            @update:model-value="state.setSearchQuery"
          >
            <template #prefix><StudioIcon name="SearchIcon" class="h-4 w-4" /></template>
          </Input>
        </div>

        <div
          class="task-filter-segments flex shrink-0 items-center gap-0.5 rounded-lg bg-gray-100 p-0.5 dark:bg-white/[0.06]"
          role="group"
          :aria-label="t('imageStudio.search.filterLabel')"
        >
          <button
            v-for="status in statuses"
            :key="status"
            type="button"
            class="btn btn-ghost btn-sm text-[11px]"
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
      </header>

      <!-- 主体：上=相框容器（声明尺寸查询上下文），下=操作区 -->
      <div class="flex min-h-0 flex-1 flex-col items-stretch gap-2 overflow-hidden px-3 py-3 sm:px-5">
        <template v-if="activeTask">
          <!-- 舞台：相框与操作区共用一个宽度变量，保证两者左右对齐 -->
          <div class="studio-stage flex min-h-0 w-full flex-1 flex-col items-center justify-center gap-2">
            <StudioSquarePreview
              :task="activeTask"
              :image-id="currentImageId ?? undefined"
              @select-image="onSelectImage"
            />

            <!-- 操作区：宽度 = 相框宽度，因此天然与图片居中对齐 -->
            <div class="studio-ops flex flex-col items-center gap-2">
            <p
              v-if="activeTask.status === 'error'"
              data-testid="result-error-message"
              class="w-full whitespace-pre-wrap break-words rounded-lg bg-red-50 px-3 py-2 text-center text-[11px] leading-5 text-red-600 dark:bg-red-500/10 dark:text-red-400"
            >
              {{ errorMessage }}
            </p>
            <p
              v-else
              class="flex w-full min-w-0 items-center justify-center gap-2 text-[11px] text-gray-500 dark:text-dark-400"
            >
              <span class="line-clamp-1 min-w-0 break-words text-gray-700 dark:text-dark-200" :title="displayPrompt">
                {{ displayPrompt }}
              </span>
              <span class="shrink-0 tabular-nums">{{ sizeLabel }}</span>
              <span v-if="ratioLabel" class="shrink-0 tabular-nums">{{ ratioLabel }}</span>
              <span class="shrink-0 tabular-nums">{{ elapsedLabel }}</span>
            </p>

            <div class="flex flex-wrap items-center justify-center gap-1.5">
              <button
                v-if="activeTask.status !== 'error'"
                type="button"
                class="btn btn-secondary btn-sm gap-1.5"
                @click="reuseConfig(activeTask)"
              >
                <StudioIcon name="CopyIcon" class="h-3.5 w-3.5" />
                {{ t('imageStudio.task.reuseParams') }}
              </button>

              <button
                v-if="currentImageId"
                type="button"
                class="btn btn-secondary btn-sm gap-1.5"
                :disabled="copying"
                @click="copyImage"
              >
                <StudioIcon name="CopyIcon" class="h-3.5 w-3.5" />
                {{ t('imageStudio.result.copyImage') }}
              </button>

              <button
                v-if="currentImageId"
                type="button"
                data-testid="download-result-image"
                class="btn btn-secondary btn-sm gap-1.5"
                :disabled="downloading"
                :aria-busy="downloading"
                :title="t('imageStudio.lightbox.download')"
                @click="downloadImage"
              >
                <StudioIcon name="DownloadIcon" class="h-3.5 w-3.5" />
                {{ t('imageStudio.lightbox.download') }}
              </button>

              <button
                v-if="currentImageId"
                type="button"
                class="btn btn-secondary btn-sm gap-1.5"
                @click="addAsReference"
              >
                <StudioIcon name="EditIcon" class="h-3.5 w-3.5" />
                {{ t('imageStudio.task.asReference') }}
              </button>

              <button
                type="button"
                class="btn btn-secondary btn-sm gap-1.5"
                @click="state.setDetailTaskId(activeTask.id)"
              >
                {{ t('imageStudio.result.viewDetail') }}
              </button>

              <button
                v-if="canRegenerate"
                type="button"
                class="btn btn-primary btn-sm gap-1.5"
                :disabled="hasRunningTask || retrying"
                @click="onRetry"
              >
                <StudioIcon name="RefreshIcon" class="h-3.5 w-3.5" />
                {{ activeTask.status === 'error' ? t('imageStudio.task.retry') : t('imageStudio.result.regenerate') }}
              </button>
            </div>

            <p class="flex items-center justify-center gap-2 text-[11px] text-gray-400">
              <span class="max-w-[14rem] truncate">{{ modelName }}</span>
              <span class="tabular-nums">{{ t('imageStudio.result.consumed', { value: consumedQuota }) }}</span>
            </p>
            </div>
          </div>
        </template>

        <!-- 空状态：父级是 items-stretch + flex-col，
             必须自己 mx-auto（水平）与 my-auto（垂直）才会真正居中，
             否则会贴到左上角。 -->
        <EmptyState v-else class="mx-auto my-auto max-w-lg" :title="t('imageStudio.resultPanel.emptyTitle')" :description="t('imageStudio.resultPanel.emptyHint')">
          <template #icon><StudioIcon name="ImageUploadIcon" class="h-10 w-10" /></template>
          <template #action>
          <div class="mt-5 grid gap-2 sm:grid-cols-2">
            <button
              v-for="prompt in examplePrompts"
              :key="prompt"
              type="button"
              class="btn btn-secondary btn-sm whitespace-normal text-left text-xs leading-5"
              @click="state.setPrompt(prompt)"
            >
              {{ prompt }}
            </button>
          </div>
          </template>
        </EmptyState>
      </div>
    </div>

    <!-- ══════════ 右侧：生成历史（独立纵向滚动） ══════════ -->
    <StudioHistoryRail
      :tasks="filteredTasks"
      :active-task-id="activeTaskId"
      :active-image-id="currentImageId"
      :collapsed="railCollapsed"
      @select="onSelectHistory"
      @toggle="toggleRail"
    />
  </section>
</template>

<script setup lang="ts">
/**
 * 生图结果区：三段式中的「中 + 右」。
 *
 * 设计约束：
 *   1. 中间预览框固定正方形，不随图片比例变化
 *   2. 中间区域不上下滚动 —— 相框边长按可视高度动态计算
 *   3. 不同尺寸图片以最短边贴合相框、居中留白（object-contain）
 *   4. 相框下放操作键
 *   5. 右侧历史栏可收起，内部纵向滚动
 *
 * 相框边长算法：容器高度减去工具栏、提示词、操作键、元信息的固定占位，
 * 再夹在 [240, 560] 之间。这样任何视口下都不会把中间撑出滚动条。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'
import Input from '@/components/common/Input.vue'
import StudioIcon from './StudioIcon.vue'
import StudioSquarePreview from './StudioSquarePreview.vue'
import StudioHistoryRail from './StudioHistoryRail.vue'
import {
  editOutputs,
  retryTask,
  reuseConfig,
  storeState as state,
  taskMatchesFilterStatus,
  taskMatchesSearchQuery,
} from '../stores/store'
import { getImageModel, getModelResolution } from '../lib/modelRegistry'
import { formatImageRatio, parseRatio } from '../lib/size'
import { getEstimatedCredits } from '../lib/generationEstimate'
import { resolveUnitPrice } from '../lib/studioPricing'
import { stripImageMentionMarkers } from '../lib/promptImageMentions'
import { getUserFacingApiErrorMessage } from '../lib/apiErrors'
import { copyImageSourceToClipboard, getClipboardFailureMessage } from '../lib/clipboard'
import { ensureImageCached } from '../lib/imageCache'
import { downloadImageSource } from '../lib/downloadImage'

const { t } = useI18n()

const RAIL_STORAGE_KEY = 'image_studio_history_rail_collapsed'

const activeTaskId = ref<string | null>(null)
const activeImageId = ref<string | null>(null)
const railCollapsed = ref(readRailCollapsed())
const retrying = ref(false)

const statuses = ['all', 'done', 'running', 'error'] as const

const filteredTasks = computed(() => {
  const q = state.searchQuery.trim().toLowerCase()
  return [...state.tasks]
    .sort((a, b) => b.createdAt - a.createdAt)
    .filter((task) => taskMatchesFilterStatus(task, state.filterStatus) && taskMatchesSearchQuery(task, q))
})

const activeTask = computed(
  () => state.tasks.find((task) => task.id === activeTaskId.value) ?? null,
)

/** 当前要显示的图：优先用用户选的，没有则用第一张 */
const currentImageId = computed(() => {
  const task = activeTask.value
  if (!task) return null
  if (activeImageId.value && task.outputImages.includes(activeImageId.value)) {
    return activeImageId.value
  }
  return task.outputImages[0] ?? null
})

const displayPrompt = computed(() =>
  activeTask.value ? stripImageMentionMarkers(activeTask.value.prompt) || t('imageStudio.task.noPrompt') : '',
)

const errorMessage = computed(() =>
  activeTask.value ? getUserFacingApiErrorMessage(activeTask.value.error, t) : '',
)

const modelName = computed(() => {
  const task = activeTask.value
  if (!task) return ''
  const id = task.modelId ?? getImageModel(task.apiModel ?? '')?.id
  return (
    task.modelName ||
    getImageModel(id ?? '', state.modelCatalog)?.name ||
    getImageModel(task.apiModel ?? '')?.name ||
    t('imageStudio.task.legacyModel')
  )
})

const sizeLabel = computed(() => {
  const task = activeTask.value
  if (!task) return ''
  const actual = parseRatio(
    task.actualParamsByImage?.[currentImageId.value ?? '']?.size ?? task.actualParams?.size ?? '',
  )
  if (actual) return `${actual.width} × ${actual.height}`
  if (task.params.size === 'auto') {
    return task.params.resolution === 'auto' ? t('imageStudio.result.sizeAuto') : task.params.resolution
  }
  return task.params.size.replace('x', ' × ')
})

const ratioLabel = computed(() => {
  const task = activeTask.value
  if (!task) return ''
  const size =
    parseRatio(
      task.actualParamsByImage?.[currentImageId.value ?? '']?.size ?? task.actualParams?.size ?? '',
    ) ?? parseRatio(task.params.size)
  return size ? formatImageRatio(size.width, size.height) : t('imageStudio.result.ratioAuto')
})

const elapsedLabel = computed(() => {
  const task = activeTask.value
  if (!task) return ''
  const ms = task.status === 'running' ? Date.now() - task.createdAt : task.elapsed
  return ms != null
    ? t('imageStudio.task.elapsedSeconds', { seconds: Math.floor(ms / 1000) })
    : t('imageStudio.result.elapsedCalculating')
})

const consumedQuota = computed(() => {
  const task = activeTask.value
  if (!task) return '0.0000'
  if (task.status === 'running') return t('imageStudio.result.calculating')
  const model = getImageModel(task.modelId ?? task.apiModel ?? '', state.modelCatalog)
  const resolution = getModelResolution(model ?? null, task.params.size, task.params.resolution)
  const count = task.status === 'done' ? task.outputImages.length : 0
  const price = resolveUnitPrice(state.imagePricing, model, task.params.size, task.params.resolution)
  return getEstimatedCredits(
    resolution,
    count,
    typeof price === 'number' && Number.isFinite(price) ? price : undefined,
  ).toFixed(4)
})

const hasRunningTask = computed(() => state.tasks.some((task) => task.status === 'running'))

const canRegenerate = computed(() => {
  const task = activeTask.value
  if (!task) return false
  if (task.status !== 'done' && task.status !== 'error') return false
  return (
    state.settings.miloRouter.connectionStatus === 'connected' &&
    state.catalogStatus === 'ready' &&
    state.modelCatalog.some((model) => model.id === (task.modelId ?? getImageModel(task.apiModel ?? '')?.id))
  )
})

const examplePrompts = computed(() => [
  t('imageStudio.resultPanel.example1'),
  t('imageStudio.resultPanel.example2'),
  t('imageStudio.resultPanel.example3'),
  t('imageStudio.resultPanel.example4'),
])

function readRailCollapsed(): boolean {
  if (typeof localStorage === 'undefined') return false
  return localStorage.getItem(RAIL_STORAGE_KEY) === '1'
}

function toggleRail() {
  railCollapsed.value = !railCollapsed.value
  try {
    localStorage.setItem(RAIL_STORAGE_KEY, railCollapsed.value ? '1' : '0')
  } catch {
    /* 隐私模式写不进去也不影响使用 */
  }
}

function select(taskId: string, imageId?: string | null) {
  activeTaskId.value = taskId
  const task = state.tasks.find((item) => item.id === taskId)
  activeImageId.value = imageId ?? task?.outputImages[0] ?? null
}

function onSelectHistory(taskId: string, imageId: string) {
  select(taskId, imageId)
}

function onSelectImage(index: number) {
  const task = activeTask.value
  if (!task) return
  const id = task.outputImages[index]
  if (id) activeImageId.value = id
}

const copying = ref(false)
const downloading = ref(false)

async function downloadImage() {
  const imageId = currentImageId.value
  if (!imageId || downloading.value) return
  downloading.value = true
  try {
    await downloadImageSource(ensureImageCached(imageId))
    state.showToast(t('imageStudio.lightbox.downloaded'), 'success')
  } catch {
    state.showToast(t('imageStudio.lightbox.downloadFailed'), 'error')
  } finally {
    downloading.value = false
  }
}

/** 复制当前图片到剪贴板。走 clipboard 库，内部会处理 http 环境下的降级。 */
async function copyImage() {
  const imageId = currentImageId.value
  if (!imageId || copying.value) return
  copying.value = true
  try {
    await copyImageSourceToClipboard(ensureImageCached(imageId))
    state.showToast(t('imageStudio.result.copySuccess'), 'success')
  } catch (err) {
    state.showToast(getClipboardFailureMessage(t('imageStudio.result.copyFailed'), err), 'error')
  } finally {
    copying.value = false
  }
}

async function addAsReference() {
  const task = activeTask.value
  const imageId = currentImageId.value
  if (!task || !imageId) return
  await editOutputs(task, [imageId])
}

async function onRetry() {
  const task = activeTask.value
  if (!task) return
  retrying.value = true
  try {
    await retryTask(task)
  } finally {
    retrying.value = false
  }
}

/** 任务被删除或筛选变化后，保证仍有可显示的目标 */
watch(
  filteredTasks,
  (list) => {
    if (!list.length) {
      activeTaskId.value = null
      activeImageId.value = null
      return
    }
    if (!activeTaskId.value || !list.some((task) => task.id === activeTaskId.value)) {
      select(list[0].id)
    }
  },
  { immediate: true },
)

/** 新提交任务时自动跳到它 */
watch(
  () => state.latestSubmittedTaskId,
  (id) => {
    if (!id) return
    state.setSearchQuery('')
    state.setFilterStatus('all')
    select(id)
  },
)

</script>

<style scoped>
/**
 * 舞台：相框与操作区共用同一个宽度。
 *
 * 关键点：--studio-frame-w 在这里算一次，相框和操作区都读它。
 * 之前操作区写死 max-w-xl(576px)，而相框是 371px，
 * 两者宽度不一致，操作区就偏左了 —— 看着像「跑到左边」。
 *
 * 88px 是操作区实际占位（提示词行 + 按钮行 + 元信息行 + 间距）。
 */
.studio-stage {
  container-type: size;
  min-height: 0;
  --studio-frame-w: min(100cqw, calc(100cqh - 88px), 720px);
}

.studio-ops {
  width: var(--studio-frame-w);
  max-width: 100%;
}

@media (max-width: 1023px) {
  .studio-stage {
    container-type: inline-size;
    --studio-frame-w: min(100cqw, 720px);
    flex: 0 0 auto;
  }
}

/**
 * 相框的尺寸查询上下文。
 *
 * StudioSquarePreview 内部用 cqw/cqh 算自己的边长，需要这个容器先声明
 * container-type: size —— 否则 cqw/cqh 会回退到视口单位，又变成不准的估算。
 *
 * min-height: 0 是必须的：flex 子项默认 min-height:auto 会把容器撑高，
 * 导致 100cqh 算出来比实际可用空间大，相框就会溢出（就是 100% 缩放那个 bug）。
 */
.studio-frame-host {
  container-type: size;
  min-height: 0;
}
</style>
