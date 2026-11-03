<template>
  <section
    :data-active-task-id="task.id"
    :data-status-task-id="task.id"
    :data-multiple-images="task.outputImages.length > 1"
    class="result-preview-panel min-w-0 bg-white dark:bg-dark-950"
    :aria-label="t('imageStudio.result.previewAria')"
  >
    <div class="p-4">
      <!-- 生成中 -->
      <div
        v-if="task.status === 'running'"
        role="status"
        class="result-image-stage flex min-h-[320px] w-full flex-col items-center justify-center gap-3 overflow-hidden rounded-xl bg-gray-50 text-sm text-primary-600 dark:bg-black/20 dark:text-primary-400"
        :style="{ maxHeight: 'min(46vh, 32rem)' }"
      >
        <template v-if="streamPreview">
          <img
            :src="streamPreview"
            :alt="t('imageStudio.task.streamPreview')"
            class="h-full w-full object-contain"
          />
          <span class="sr-only">{{ t('imageStudio.result.generatingFinal') }}</span>
        </template>
        <template v-else>
          <span
            class="h-9 w-9 animate-spin rounded-full border-2 border-primary-200 border-t-primary-600"
          />
          {{ t('imageStudio.result.generating') }}
        </template>
      </div>

      <!-- 已完成：结果图 -->
      <StudioResultImage
        v-else-if="imageId"
        :key="imageId"
        :image-id="imageId"
        :images="task.outputImages"
        @size="onNaturalSize"
      />

      <!-- 无图 -->
      <div
        v-else
        class="result-image-stage flex min-h-[320px] w-full items-center justify-center rounded-xl bg-gray-50 text-sm text-gray-400 dark:bg-black/20"
      >
        {{
          task.status === 'error'
            ? t('imageStudio.result.noOutput')
            : t('imageStudio.result.unavailable')
        }}
      </div>

      <!-- 失败原因：主面板必须能看到，不能只藏在详情弹窗里 -->
      <div
        v-if="task.status === 'error'"
        role="status"
        class="mt-3 rounded-lg border border-red-100 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-600 dark:border-red-500/20 dark:bg-red-500/10 dark:text-red-400"
      >
        <p class="break-words">{{ errorMessage }}</p>
        <p v-if="!available" class="mt-1 text-gray-500 dark:text-dark-400">
          {{ t('imageStudio.result.modelUnavailableHint') }}
        </p>
      </div>

      <!-- 部分图片失败 -->
      <p
        v-else-if="partialFailure"
        role="status"
        class="mt-3 rounded-lg border border-amber-100 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-700 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-300"
      >
        {{ partialFailure }}
      </p>

      <!-- 多图切换 -->
      <div v-if="task.outputImages.length > 1" class="mt-3">
        <div class="flex items-center justify-between gap-3">
          <span aria-live="polite" class="text-xs text-gray-500">
            {{ t('imageStudio.result.imageCounter', { index: imageIndex + 1, total: task.outputImages.length }) }}
          </span>
          <div class="flex gap-1">
            <button
              type="button"
              class="flex h-8 w-8 items-center justify-center rounded-md border border-gray-200 disabled:opacity-30 dark:border-white/[0.1]"
              :aria-label="t('imageStudio.result.prevImage')"
              :title="t('imageStudio.result.prevImage')"
              :disabled="imageIndex === 0"
              @click="selectImage(imageIndex - 1)"
            >
              <StudioIcon name="ChevronLeftIcon" class="h-4 w-4" />
            </button>
            <button
              type="button"
              class="flex h-8 w-8 items-center justify-center rounded-md border border-gray-200 disabled:opacity-30 dark:border-white/[0.1]"
              :aria-label="t('imageStudio.result.nextImage')"
              :title="t('imageStudio.result.nextImage')"
              :disabled="imageIndex === task.outputImages.length - 1"
              @click="selectImage(imageIndex + 1)"
            >
              <StudioIcon name="ChevronRightIcon" class="h-4 w-4" />
            </button>
          </div>
        </div>

        <div class="mt-2 flex gap-2 overflow-x-auto pb-1" :aria-label="t('imageStudio.result.taskImages')">
          <button
            v-for="(id, index) in task.outputImages"
            :key="id"
            type="button"
            class="h-16 w-16 shrink-0 overflow-hidden rounded-lg border-2 bg-gray-100 dark:bg-white/[0.06]"
            :class="id === imageId ? 'border-primary-500' : 'border-transparent'"
            :aria-label="t('imageStudio.result.viewImageN', { index: index + 1 })"
            :aria-pressed="id === imageId"
            @click="selectImage(index)"
          >
            <StudioImageThumbnail :image-id="id" />
          </button>
        </div>
      </div>

      <!-- 描述与规格 -->
      <div class="mt-4">
        <p
          data-selectable-text
          class="line-clamp-2 break-words text-sm font-medium leading-6 text-gray-800 dark:text-dark-200"
        >
          {{ displayPrompt }}
        </p>
        <div
          data-result-specs
          class="mt-2 flex flex-wrap gap-2 text-[11px] text-gray-600 dark:text-dark-300"
          :aria-label="t('imageStudio.result.specAria')"
        >
          <span
            class="max-w-full truncate rounded-md bg-gray-100 px-2 py-1 dark:bg-white/[0.07]"
          >{{ modelName }}</span>
          <span class="rounded-md bg-gray-100 px-2 py-1 tabular-nums dark:bg-white/[0.07]">{{ sizeLabel }}</span>
          <span
            v-if="ratioLabel"
            class="rounded-md bg-gray-100 px-2 py-1 tabular-nums dark:bg-white/[0.07]"
          >{{ ratioLabel }}</span>
        </div>

        <div
          data-result-record
          class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-dark-500"
          :aria-label="t('imageStudio.result.recordAria')"
        >
          <time :dateTime="createdIso">
            {{ t('imageStudio.result.generatedAt', { time: createdFull }) }}
          </time>
          <span>{{ elapsedLabel }}</span>
          <span class="tabular-nums text-gray-500 dark:text-dark-400">
            {{ t('imageStudio.result.consumed', { value: consumedQuota }) }}
          </span>
        </div>
      </div>

      <!-- 操作区 -->
      <div
        class="mt-4 flex flex-wrap items-center gap-2 border-t border-gray-100 pt-4 text-xs dark:border-white/[0.06]"
      >
        <button
          v-if="task.status !== 'error'"
          type="button"
          class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-3 py-2 dark:border-white/[0.1]"
          @click="reuseConfig(task)"
        >
          <StudioIcon name="CopyIcon" class="h-4 w-4" />{{ t('imageStudio.task.reuseParams') }}
        </button>

        <template v-if="imageId">
          <button
            type="button"
            class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-3 py-2 disabled:opacity-40 dark:border-white/[0.1]"
            :disabled="!canAddReference || editing"
            :title="referenceTitle"
            @click="onEditOutputs"
          >
            <StudioIcon name="EditIcon" class="h-4 w-4" />{{ t('imageStudio.task.asReference') }}
          </button>
          <button
            v-if="showEditImage"
            type="button"
            class="rounded-lg border border-gray-200 px-3 py-2 disabled:opacity-40 dark:border-white/[0.1]"
            :disabled="!canEdit || editing"
            @click="onEditMask"
          >
            {{ t('imageStudio.result.editImage') }}
          </button>
        </template>

        <button
          type="button"
          class="rounded-lg px-2 py-2 text-gray-500 hover:text-primary-600"
          @click="state.setDetailTaskId(task.id)"
        >
          {{ t('imageStudio.result.viewDetail') }}
        </button>
      </div>

      <!-- 再次生成 / 失败重试 -->
      <div v-if="task.status === 'done' || task.status === 'error'" class="mt-3 flex">
        <button
          v-if="available"
          type="button"
          class="inline-flex items-center justify-center gap-2 rounded-lg bg-primary-600 px-3.5 py-2 text-sm font-medium text-white transition hover:bg-primary-700 disabled:opacity-40"
          :disabled="hasRunningTask || retrying"
          @click="onRetry"
        >
          <StudioIcon name="RefreshIcon" class="h-4 w-4" />{{
            task.status === 'error'
              ? t('imageStudio.task.retry')
              : t('imageStudio.result.regenerate')
          }}
        </button>
        <button
          v-else
          type="button"
          class="text-sm text-primary-600 hover:underline"
          @click="reuseConfig(task)"
        >
          {{ t('imageStudio.result.reuseAndPick') }}
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'
import { getUserFacingApiErrorMessage } from '../lib/apiErrors'
import {
  storeState as state,
  editOutputMask,
  editOutputs,
  reuseConfig,
  retryTask,
} from '../stores/store'
import { getEstimatedCredits } from '../lib/generationEstimate'
import { resolveUnitPrice } from '../lib/studioPricing'
import { getImageModel, getModelResolution } from '../lib/modelRegistry'
import { formatImageRatio, parseRatio } from '../lib/size'
import { stripImageMentionMarkers } from '../lib/promptImageMentions'
import StudioIcon from './StudioIcon.vue'
import StudioImageThumbnail from './StudioImageThumbnail.vue'
import StudioResultImage from './StudioResultImage.vue'

const props = defineProps<{ task: TaskRecord }>()
const { t } = useI18n()

const retrying = ref(false)
const editing = ref(false)
const selection = ref({ taskId: props.task.id, imageId: props.task.outputImages[0] })
const naturalSize = ref<{ imageId: string; width: number; height: number } | null>(null)

watch(
  () => props.task.id,
  (id) => {
    selection.value = { taskId: id, imageId: props.task.outputImages[0] }
  },
)

const imageId = computed(() => {
  const sel = selection.value
  if (sel.taskId === props.task.id && props.task.outputImages.includes(sel.imageId)) {
    return sel.imageId
  }
  return props.task.outputImages[0]
})

const imageIndex = computed(() => props.task.outputImages.indexOf(imageId.value))
const streamPreview = computed(() => state.streamPreviews[props.task.id]?.image)

const displayPrompt = computed(
  () => stripImageMentionMarkers(props.task.prompt) || t('imageStudio.task.noPrompt'),
)

const modelId = computed(
  () => props.task.modelId ?? getImageModel(props.task.apiModel ?? '')?.id,
)

const taskModel = computed(
  () =>
    getImageModel(modelId.value ?? '', state.modelCatalog) ??
    getImageModel(props.task.apiModel ?? ''),
)

const available = computed(
  () =>
    state.settings.miloRouter.connectionStatus === 'connected' &&
    state.catalogStatus === 'ready' &&
    state.modelCatalog.some((model) => model.id === modelId.value),
)

const capabilities = computed(
  () => getImageModel(state.settings.selectedModelId, state.modelCatalog)?.capabilities,
)

const referenceLimit = computed(() => Math.min(16, capabilities.value?.referenceImages ?? 0))

const alreadyAdded = computed(() => state.inputImages.some((image) => image.id === imageId.value))

const canAddReference = computed(
  () =>
    state.catalogStatus === 'ready' &&
    state.inputImages.length < referenceLimit.value &&
    !alreadyAdded.value,
)

const canEdit = computed(
  () =>
    state.catalogStatus === 'ready' &&
    Boolean(
      (capabilities.value?.mask || capabilities.value?.transparentOutput) &&
        referenceLimit.value,
    ) &&
    (alreadyAdded.value || state.inputImages.length < referenceLimit.value),
)

const showEditImage = computed(
  () =>
    Boolean(capabilities.value?.mask || capabilities.value?.transparentOutput) &&
    Boolean(referenceLimit.value),
)

const hasRunningTask = computed(() => state.tasks.some((item) => item.status === 'running'))

const modelName = computed(
  () =>
    props.task.modelName ||
    getImageModel(props.task.apiModel ?? '')?.name ||
    t('imageStudio.task.legacyModel'),
)

const actualSize = computed(() => {
  if (naturalSize.value?.imageId === imageId.value) return naturalSize.value
  return parseRatio(
    props.task.actualParamsByImage?.[imageId.value]?.size ??
      props.task.actualParams?.size ??
      '',
  )
})

const requestedSize = computed(() => parseRatio(props.task.params.size))
const ratioSize = computed(() => actualSize.value ?? requestedSize.value)

const sizeLabel = computed(() => {
  if (actualSize.value) return `${actualSize.value.width} × ${actualSize.value.height}`
  if (props.task.params.size === 'auto') {
    return props.task.params.resolution === 'auto'
      ? t('imageStudio.result.sizeAuto')
      : props.task.params.resolution
  }
  return t('imageStudio.result.requestedSize', {
    size: props.task.params.size.replace('x', ' × '),
  })
})

const ratioLabel = computed(() =>
  ratioSize.value
    ? formatImageRatio(ratioSize.value.width, ratioSize.value.height)
    : t('imageStudio.result.ratioAuto'),
)

const consumedQuota = computed(() => {
  if (props.task.status === 'running') return t('imageStudio.result.calculating')

  const resolution = getModelResolution(
    taskModel.value ?? null,
    props.task.params.size,
    props.task.params.resolution,
  )
  const count = props.task.status === 'done' ? props.task.outputImages.length : 0

  // 用后端实价还原本次花费；取不到时退回内置估算
  const price = resolveUnitPrice(
    state.imagePricing,
    taskModel.value,
    props.task.params.size,
    props.task.params.resolution,
  )

  return getEstimatedCredits(
    resolution,
    count,
    typeof price === 'number' && Number.isFinite(price) ? price : undefined,
  ).toFixed(4)
})

const createdIso = computed(() => new Date(props.task.createdAt).toISOString())
const createdFull = computed(() =>
  new Date(props.task.createdAt).toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }),
)

/** 运行中每秒刷新，让用户看到真实等待时长（原版行为） */
const now = ref(Date.now())
let ticker: number | null = null

function stopTicker() {
  if (ticker != null) {
    window.clearInterval(ticker)
    ticker = null
  }
}

watch(
  () => [props.task.status, props.task.id] as const,
  ([status]) => {
    stopTicker()
    if (status !== 'running') return
    now.value = Date.now()
    ticker = window.setInterval(() => {
      now.value = Date.now()
    }, 1000)
  },
  { immediate: true },
)

onUnmounted(stopTicker)

const elapsedMs = computed(() => {
  if (props.task.status === 'running') {
    return Math.max(0, now.value - props.task.createdAt)
  }
  return props.task.elapsed ?? null
})

const elapsedLabel = computed(() =>
  elapsedMs.value != null
    ? t('imageStudio.task.elapsedSeconds', { seconds: Math.floor(elapsedMs.value / 1000) })
    : t('imageStudio.result.elapsedCalculating'),
)

/** 失败任务的可见原因（模型不可用等底层信息不外暴） */
const errorMessage = computed(() => getUserFacingApiErrorMessage(props.task.error, t))

/** 部分图片失败时给出明确提示，而不是静默少图 */
const partialFailure = computed(() => {
  if (props.task.status !== 'done' || !props.task.outputErrors?.length) return ''
  return t('imageStudio.result.partialFailed', {
    done: props.task.outputImages.length,
    total: props.task.params.n,
    error: getUserFacingApiErrorMessage(props.task.outputErrors[0]?.error, t),
  })
})


const referenceTitle = computed(() => {
  if (alreadyAdded.value) return t('imageStudio.result.alreadyReferenced')
  if (canAddReference.value) return t('imageStudio.result.addAsReference')
  return t('imageStudio.result.referenceUnavailable')
})

function selectImage(index: number) {
  selection.value = { taskId: props.task.id, imageId: props.task.outputImages[index] }
}

function onNaturalSize(size: { width: number; height: number }) {
  naturalSize.value = { imageId: imageId.value, ...size }
}

async function onEditOutputs() {
  editing.value = true
  try {
    await editOutputs(props.task, [imageId.value])
  } finally {
    editing.value = false
  }
}

async function onEditMask() {
  editing.value = true
  try {
    await editOutputMask(props.task, imageId.value)
  } finally {
    editing.value = false
  }
}

async function onRetry() {
  retrying.value = true
  try {
    await retryTask(props.task)
  } finally {
    retrying.value = false
  }
}
</script>
