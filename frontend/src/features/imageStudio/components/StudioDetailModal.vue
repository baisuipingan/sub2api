<template>
  <div
    v-if="task"
    class="fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="close"
  >
    <div class="absolute inset-0 bg-black/40 backdrop-blur-sm" />
    <div
      ref="modalRef"
      role="dialog"
      aria-modal="true"
      aria-labelledby="task-detail-title"
      class="relative z-10 flex max-h-[90vh] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-2xl dark:border-white/[0.1] dark:bg-dark-900 lg:flex-row"
      @click.stop
    >
      <!-- 左侧大图 -->
      <div
        class="relative flex min-h-[280px] flex-1 items-center justify-center bg-gray-100 p-4 dark:bg-black/20"
      >
        <button
          v-if="task.status === 'done' && outputSrc"
          type="button"
          class="h-full w-full"
          :aria-label="t('imageStudio.result.zoomAria')"
          @click="state.setLightboxImageId(outputId, task.outputImages)"
        >
          <img
            :src="outputSrc"
            :data-image-id="outputId"
            alt=""
            class="mx-auto max-h-[58vh] max-w-full object-contain"
          />
        </button>

        <div
          v-else-if="task.status === 'running'"
          class="flex flex-col items-center gap-3 text-sm text-primary-600 dark:text-primary-400"
        >
          <img
            v-if="streamPreview"
            :src="streamPreview"
            :alt="t('imageStudio.task.streamPreview')"
            class="max-h-[58vh] max-w-full object-contain"
          />
          <span
            v-else
            class="h-10 w-10 animate-spin rounded-full border-2 border-primary-200 border-t-primary-600"
          />
          <span :class="streamPreview ? 'sr-only' : ''">{{ t('imageStudio.status.running') }}</span>
        </div>

        <div v-else class="max-w-sm text-center text-sm leading-6 text-red-500">
          {{ errorMessage }}
        </div>

        <template v-if="task.outputImages.length > 1">
          <button
            type="button"
            class="absolute left-3 top-1/2 rounded-full bg-black/50 p-2 text-white transition hover:bg-black/70"
            :aria-label="t('imageStudio.result.prevImage')"
            :title="t('imageStudio.result.prevImage')"
            @click="prevImage"
          >
            ‹
          </button>
          <button
            type="button"
            class="absolute right-3 top-1/2 rounded-full bg-black/50 p-2 text-white transition hover:bg-black/70"
            :aria-label="t('imageStudio.result.nextImage')"
            :title="t('imageStudio.result.nextImage')"
            @click="nextImage"
          >
            ›
          </button>
          <span
            class="absolute bottom-3 rounded bg-black/55 px-2 py-1 text-xs text-white"
          >{{ imageIndex + 1 }} / {{ task.outputImages.length }}</span>
        </template>
      </div>

      <!-- 右侧详情 -->
      <div class="flex min-h-0 w-full flex-col overflow-y-auto p-5 lg:w-[420px]">
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-xs font-medium uppercase tracking-[0.16em] text-gray-400">
              {{ t('imageStudio.detail.title') }}
            </p>
            <h2 id="task-detail-title" class="mt-1 text-lg font-semibold text-gray-950 dark:text-white">
              {{ modelName || t('imageStudio.detail.generationTask') }}
            </h2>
          </div>
          <button
            type="button"
            class="rounded-lg p-2 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-white/[0.06] dark:hover:text-dark-200"
            :aria-label="t('imageStudio.common.close')"
            :title="t('imageStudio.common.close')"
            @click="close"
          >
            <StudioIcon name="CloseIcon" class="h-5 w-5" />
          </button>
        </div>

        <!-- 提示词 -->
        <section class="mt-5">
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-xs font-medium uppercase tracking-[0.14em] text-gray-400">
              {{ t('imageStudio.detail.prompt') }}
            </h3>
            <button
              type="button"
              class="rounded-md p-1.5 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-white/[0.06]"
              :aria-label="t('imageStudio.detail.copyPrompt')"
              :title="t('imageStudio.detail.copyPrompt')"
              @click="handleCopyPrompt"
            >
              <StudioIcon name="CopyIcon" class="h-4 w-4" />
            </button>
          </div>
          <p class="whitespace-pre-wrap text-sm leading-6 text-gray-700 dark:text-dark-200">
            {{ displayPrompt || t('imageStudio.detail.noPrompt') }}
          </p>
        </section>

        <!-- 参考图 -->
        <section v-if="task.inputImageIds.length > 0" class="mt-5">
          <h3 class="mb-2 text-xs font-medium uppercase tracking-[0.14em] text-gray-400">
            {{ t('imageStudio.detail.referenceImages') }}
          </h3>
          <div class="flex gap-2 overflow-x-auto">
            <button
              v-for="id in task.inputImageIds"
              :key="id"
              type="button"
              class="h-16 w-16 shrink-0 overflow-hidden rounded-lg border border-gray-200 dark:border-white/[0.1]"
              :aria-label="t('imageStudio.detail.viewReference')"
              @click="state.setLightboxImageId(id, task.inputImageIds)"
            >
              <img
                v-if="imageSources[id]"
                :src="imageSources[id]"
                alt=""
                class="h-full w-full object-cover"
              />
            </button>
          </div>
        </section>

        <!-- 遮罩范围 -->
        <section v-if="maskPreview && task.maskTargetImageId" class="mt-5">
          <h3 class="mb-2 text-xs font-medium uppercase tracking-[0.14em] text-gray-400">
            {{ t('imageStudio.detail.editRange') }}
          </h3>
          <button
            type="button"
            class="overflow-hidden rounded-lg border border-primary-200 bg-primary-50 dark:border-primary-500/30 dark:bg-primary-500/10"
            :aria-label="t('imageStudio.detail.viewMask')"
            @click="state.setLightboxImageId(task.maskTargetImageId!, [task.maskTargetImageId!])"
          >
            <img :src="maskPreview" :alt="t('imageStudio.detail.maskAlt')" class="h-28 w-28 object-cover" />
          </button>
          <p class="mt-1.5 text-xs text-gray-400">{{ t('imageStudio.detail.maskHint') }}</p>
        </section>

        <!-- 参数 -->
        <section class="mt-5">
          <h3 class="mb-2 text-xs font-medium uppercase tracking-[0.14em] text-gray-400">
            {{ t('imageStudio.detail.params') }}
          </h3>
          <dl class="grid grid-cols-2 gap-2 text-sm">
            <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-white/[0.04]">
              <dt class="text-xs text-gray-400">{{ t('imageStudio.detail.resolution') }}</dt>
              <dd class="mt-1 text-gray-700 dark:text-dark-200">
                {{ task.params.resolution === 'auto' ? t('imageStudio.detail.defaultValue') : task.params.resolution }}
              </dd>
            </div>
            <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-white/[0.04]">
              <dt class="text-xs text-gray-400">{{ t('imageStudio.detail.ratio') }}</dt>
              <dd class="mt-1 text-gray-700 dark:text-dark-200">{{ ratio }}</dd>
            </div>
            <div v-if="capabilities.quality" class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-white/[0.04]">
              <dt class="text-xs text-gray-400">{{ t('imageStudio.detail.quality') }}</dt>
              <dd class="mt-1 text-gray-700 dark:text-dark-200">{{ task.params.quality }}</dd>
            </div>
            <div v-if="capabilities.outputFormat" class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-white/[0.04]">
              <dt class="text-xs text-gray-400">{{ t('imageStudio.detail.format') }}</dt>
              <dd class="mt-1 text-gray-700 dark:text-dark-200">
                {{ task.params.output_format.toUpperCase() }}
              </dd>
            </div>
            <div v-if="capabilities.imageCount" class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-white/[0.04]">
              <dt class="text-xs text-gray-400">{{ t('imageStudio.detail.count') }}</dt>
              <dd class="mt-1 text-gray-700 dark:text-dark-200">{{ task.params.n }}</dd>
            </div>
            <div v-if="capabilities.moderation" class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-white/[0.04]">
              <dt class="text-xs text-gray-400">{{ t('imageStudio.detail.moderation') }}</dt>
              <dd class="mt-1 text-gray-700 dark:text-dark-200">{{ task.params.moderation }}</dd>
            </div>
            <div v-if="task.editMode" class="rounded-lg bg-primary-50 px-3 py-2 dark:bg-primary-500/10">
              <dt class="text-xs text-primary-500">{{ t('imageStudio.detail.editMode') }}</dt>
              <dd class="mt-1 text-primary-700 dark:text-primary-300">
                {{ task.editMode === 'erase' ? t('imageStudio.detail.eraseFill') : t('imageStudio.detail.localEdit') }}
              </dd>
            </div>
            <div
              v-if="task.transparentOutput"
              class="rounded-lg bg-emerald-50 px-3 py-2 dark:bg-emerald-500/10"
            >
              <dt class="text-xs text-emerald-600 dark:text-emerald-400">
                {{ t('imageStudio.detail.transparent') }}
              </dt>
              <dd class="mt-1 text-emerald-700 dark:text-emerald-300">
                {{ t('imageStudio.detail.enabled') }}
              </dd>
            </div>
          </dl>
        </section>

        <p class="mt-5 text-xs text-gray-400">
          {{ createdText }}
        </p>

        <!-- 操作 -->
        <div
          class="mt-auto grid grid-cols-2 gap-2 border-t border-gray-100 pt-4 dark:border-white/[0.08] sm:grid-cols-3"
        >
          <button
            type="button"
            class="flex items-center justify-center gap-1.5 rounded-lg bg-primary-50 px-3 py-2 text-sm font-medium text-primary-600 transition hover:bg-primary-100 dark:bg-primary-500/10 dark:text-primary-400"
            @click="handleReuse"
          >
            <StudioIcon name="RefreshIcon" class="h-4 w-4 rotate-180" />{{ t('imageStudio.detail.reuse') }}
          </button>
          <button
            type="button"
            class="flex items-center justify-center gap-1.5 rounded-lg bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-600 transition hover:bg-emerald-100 disabled:opacity-40 dark:bg-emerald-500/10 dark:text-emerald-400"
            :disabled="!task.outputImages.length"
            @click="handleEdit"
          >
            <StudioIcon name="EditIcon" class="h-4 w-4" />{{ t('imageStudio.detail.reedit') }}
          </button>
          <button
            v-if="task.status === 'error'"
            type="button"
            class="flex items-center justify-center gap-1.5 rounded-lg bg-primary-50 px-3 py-2 text-sm font-medium text-primary-600 transition hover:bg-primary-100 dark:bg-primary-500/10 dark:text-primary-400"
            @click="handleRetry"
          >
            <StudioIcon name="RefreshIcon" class="h-4 w-4" />{{ t('imageStudio.task.retry') }}
          </button>
          <button
            type="button"
            class="flex items-center justify-center gap-1.5 rounded-lg bg-red-50 px-3 py-2 text-sm font-medium text-red-600 transition hover:bg-red-100 dark:bg-red-500/10 dark:text-red-400"
            @click="handleDelete"
          >
            <StudioIcon name="TrashIcon" class="h-4 w-4" />{{ t('imageStudio.task.delete') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import {
  storeState as state,
  editOutputs,
  removeTask,
  retryTask,
  reuseConfig,
} from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'
import { ensureImageCached } from '../lib/imageCache'
import { getImageModel } from '../lib/modelRegistry'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'
import { copyTextToClipboard } from '../lib/clipboard'
import { createMaskPreviewDataUrl } from '../lib/canvasImage'
import { getUserFacingApiErrorMessage } from '../lib/apiErrors'
import { stripImageMentionMarkers } from '../lib/promptImageMentions'
import { formatImageRatio, parseRatio } from '../lib/size'

const { t } = useI18n()

const modalRef = ref<HTMLDivElement | null>(null)
const imageSources = ref<Record<string, string>>({})
const maskPreview = ref('')
const imageIndex = ref(0)

const task = computed(() => state.tasks.find((item) => item.id === state.detailTaskId) ?? null)
const streamPreview = computed(() =>
  state.detailTaskId ? state.streamPreviews[state.detailTaskId]?.image : undefined,
)

const taskModelId = computed(() => task.value?.modelId ?? task.value?.apiModel ?? '')

const capabilities = computed(() =>
  getImageModelCapabilities(
    taskModelId.value,
    getImageModel(taskModelId.value, state.modelCatalog) ? state.modelCatalog : undefined,
  ),
)

const modelName = computed(() => {
  const current = task.value
  if (!current) return ''
  return (
    current.modelName ??
    (current.apiModel ? getImageModel(current.apiModel)?.name : '') ??
    t('imageStudio.task.legacyModel')
  )
})

const open = computed(() => Boolean(task.value))

useCloseOnEscape(() => open.value, () => state.setDetailTaskId(null))
usePreventBackgroundScroll(() => open.value, modalRef)

const outputId = computed(() => task.value?.outputImages[imageIndex.value] ?? '')
const outputSrc = computed(() => (outputId.value ? imageSources.value[outputId.value] : ''))

const displayPrompt = computed(() =>
  task.value ? stripImageMentionMarkers(task.value.prompt) : '',
)

const errorMessage = computed(() =>
  task.value ? getUserFacingApiErrorMessage(task.value.error, t) : '',
)

const ratio = computed(() => {
  if (!task.value) return ''
  const requested = parseRatio(task.value.params.size)
  return requested ? formatImageRatio(requested.width, requested.height) : t('imageStudio.ratio.autoShort')
})

const createdText = computed(() => {
  if (!task.value) return ''
  const created = new Date(task.value.createdAt).toLocaleString()
  const duration =
    task.value.elapsed == null
      ? ''
      : t('imageStudio.task.elapsedSeconds', {
          seconds: Math.floor(task.value.elapsed / 1000),
        })
  return duration
    ? t('imageStudio.detail.createdWithDuration', { created, duration })
    : t('imageStudio.detail.createdOnly', { created })
})

let cancelled = false

/** 预加载任务涉及的全部图片（输出 + 参考 + 遮罩） */
watch(
  task,
  async (current) => {
    imageIndex.value = 0
    imageSources.value = {}
    if (!current) return

    cancelled = false
    const ids = [
      ...current.outputImages,
      ...current.inputImageIds,
      ...(current.maskImageId ? [current.maskImageId] : []),
    ]
    const entries = await Promise.all(
      ids.map(async (id) => [id, await ensureImageCached(id)] as const),
    )
    if (cancelled) return
    imageSources.value = Object.fromEntries(
      entries.filter((entry): entry is [string, string] => Boolean(entry[1])),
    )
  },
  { immediate: true },
)

/** 合成遮罩预览 */
watch(
  [imageSources, task],
  async ([sources, current]) => {
    maskPreview.value = ''
    if (!current?.maskTargetImageId || !current.maskImageId) return
    const source = sources[current.maskTargetImageId]
    const mask = sources[current.maskImageId]
    if (!source || !mask) return

    try {
      const preview = await createMaskPreviewDataUrl(source, mask)
      if (!cancelled) maskPreview.value = preview
    } catch {
      if (!cancelled) maskPreview.value = ''
    }
  },
)

function prevImage() {
  if (!task.value) return
  imageIndex.value =
    (imageIndex.value - 1 + task.value.outputImages.length) % task.value.outputImages.length
}

function nextImage() {
  if (!task.value) return
  imageIndex.value = (imageIndex.value + 1) % task.value.outputImages.length
}

function close() {
  state.setDetailTaskId(null)
}

async function handleCopyPrompt() {
  if (!task.value) return
  try {
    await copyTextToClipboard(stripImageMentionMarkers(task.value.prompt))
    state.showToast(t('imageStudio.detail.promptCopied'), 'success')
  } catch {
    state.showToast(t('imageStudio.detail.promptCopyFailed'), 'error')
  }
}

function handleDelete() {
  const current = task.value
  if (!current) return
  close()
  state.setConfirmDialog({
    title: t('imageStudio.grid.deleteTitle'),
    message: t('imageStudio.grid.deleteMessage'),
    tone: 'danger',
    confirmText: t('imageStudio.task.delete'),
    cancelText: t('imageStudio.common.cancel'),
    action: () => removeTask(current),
  })
}

function handleReuse() {
  if (!task.value) return
  void reuseConfig(task.value)
  close()
}

function handleEdit() {
  if (!task.value) return
  void editOutputs(task.value)
  close()
}

function handleRetry() {
  if (!task.value) return
  void retryTask(task.value)
  close()
}
</script>
