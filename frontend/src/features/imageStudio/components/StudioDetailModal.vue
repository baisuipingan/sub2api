<template>
  <BaseDialog
    :show="open"
    :title="t('imageStudio.detail.title')"
    width="extra-wide"
    :close-on-escape="false"
    close-on-click-outside
    @close="close"
  >
    <div v-if="task" class="flex min-h-0 flex-col gap-5 lg:flex-row">
      <!-- 左侧大图 -->
      <div
        class="relative flex min-h-[280px] min-w-0 flex-1 items-center justify-center rounded-xl bg-gray-100 p-4 dark:bg-black/20"
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
          <LoadingSpinner v-else size="lg" />
          <span :class="streamPreview ? 'sr-only' : ''">{{ t('imageStudio.status.running') }}</span>
        </div>

        <div v-else data-testid="detail-error-message" class="max-h-[58vh] max-w-sm overflow-y-auto whitespace-pre-wrap break-words text-center text-sm leading-6 text-red-500">
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
      <div class="flex min-h-0 w-full min-w-0 flex-col lg:w-[380px] lg:shrink-0">
        <h2 class="text-lg font-semibold text-gray-950 dark:text-white">
          {{ modelName || t('imageStudio.detail.generationTask') }}
        </h2>

        <!-- 提示词 -->
        <section class="mt-5">
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-xs font-medium uppercase tracking-[0.14em] text-gray-400">
              {{ t('imageStudio.detail.prompt') }}
            </h3>
            <button
              type="button"
              class="btn btn-ghost btn-icon"
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

        <div v-if="showGallerySyncStatus" class="mt-2 flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400">
          <span :class="detailSyncStatus === 'error' ? 'text-red-500' : ''">{{ t('imageStudio.gallery.' + gallerySyncLabelKey(detailSyncStatus)) }}</span>
          <button v-if="detailSyncStatus === 'error'" type="button" class="btn btn-ghost btn-sm text-primary-600" :disabled="retryingSync" @click="handleRetrySync">
            {{ t('imageStudio.gallery.retrySync') }}
          </button>
        </div>
        <p v-if="showGallerySyncStatus && detailSyncStatus === 'error'" class="mt-1 text-xs text-red-500">{{ detailSyncReason }}</p>

        <!-- 操作 -->
        <div
          class="mt-auto grid grid-cols-2 gap-2 border-t border-gray-100 pt-4 dark:border-white/[0.08] sm:grid-cols-3"
        >
          <button
            type="button"
            class="btn btn-secondary btn-sm gap-1.5"
            @click="handleReuse"
          >
            <StudioIcon name="RefreshIcon" class="h-4 w-4 rotate-180" />{{ t('imageStudio.detail.reuse') }}
          </button>
          <button
            type="button"
            class="btn btn-success btn-sm gap-1.5"
            :disabled="!task.outputImages.length"
            @click="handleEdit"
          >
            <StudioIcon name="EditIcon" class="h-4 w-4" />{{ t('imageStudio.detail.reedit') }}
          </button>
          <button
            v-if="task.status === 'error'"
            type="button"
            class="btn btn-secondary btn-sm gap-1.5"
            @click="handleRetry"
          >
            <StudioIcon name="RefreshIcon" class="h-4 w-4" />{{ t('imageStudio.task.retry') }}
          </button>
          <button
            type="button"
            class="btn btn-danger btn-sm gap-1.5"
            @click="handleDelete"
          >
            <StudioIcon name="TrashIcon" class="h-4 w-4" />{{ t('imageStudio.task.delete') }}
          </button>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import {
  storeState as state,
  editOutputs,
  removeTask,
  retryTask,
  retryGalleryTask,
  reuseConfig,
} from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { ensureImageCached } from '../lib/imageCache'
import { getImageModel } from '../lib/modelRegistry'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'
import { copyTextToClipboard } from '../lib/clipboard'
import { createMaskPreviewDataUrl } from '../lib/canvasImage'
import { getUserFacingApiErrorMessage } from '../lib/apiErrors'
import { stripImageMentionMarkers } from '../lib/promptImageMentions'
import { formatImageRatio, parseRatio } from '../lib/size'
import { galleryTaskSyncStatus, gallerySyncLabelKey, gallerySyncReasonKey } from '../lib/gallerySyncDisplay'

const { t } = useI18n()

const imageSources = ref<Record<string, string>>({})
const maskPreview = ref('')
const imageIndex = ref(0)
const retryingSync = ref(false)

const task = computed(() => state.tasks.find((item) => item.id === state.detailTaskId) ?? null)
const detailSyncStatus = computed(() => task.value
  ? galleryTaskSyncStatus(state.galleryUserId, state.gallerySyncStatus, task.value.id)
  : 'local')
const showGallerySyncStatus = computed(() => Boolean(task.value && state.galleryUserId && task.value.status !== 'running'
  && (task.value.execution !== 'cloud' || task.value.cloudJobStatus === 'completed' || task.value.cloudJobStatus === 'partial')))
const detailSyncReason = computed(() => task.value
  ? t('imageStudio.gallery.' + gallerySyncReasonKey(state.gallerySyncErrorCode?.[task.value.id]))
  : '')

async function handleRetrySync() {
  if (!task.value) return
  retryingSync.value = true
  try { await retryGalleryTask(task.value.id) } finally { retryingSync.value = false }
}
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

/** 预加载任务涉及的全部图片（输出 + 参考 + 遮罩） */
watch(
  [task, () => state.detailImageId],
  async ([current, selectedId], _previous, onCleanup) => {
    let cancelled = false
    onCleanup(() => { cancelled = true })
    imageIndex.value = Math.max(0, current?.outputImages.indexOf(selectedId ?? '') ?? 0)
    imageSources.value = {}
    if (!current) return
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
  async ([sources, current], _previous, onCleanup) => {
    let cancelled = false
    onCleanup(() => { cancelled = true })
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
