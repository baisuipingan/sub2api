<template>
  <article
    :data-task-id="task.id"
    :aria-label="t('imageStudio.task.viewAria', { prompt: displayPrompt })"
    :aria-current="isSelected ? 'true' : undefined"
    tabindex="0"
    class="min-w-0 cursor-pointer transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
    :class="
      compact
        ? [
            'w-[72px] shrink-0 overflow-hidden rounded-lg border',
            isSelected
              ? 'border-primary-500 ring-2 ring-primary-500/40'
              : 'border-gray-200 hover:border-primary-300 dark:border-white/[0.1]',
          ]
        : [
            'rounded-lg border p-3',
            isSelected
              ? 'border-primary-500 bg-primary-50/60 ring-1 ring-inset ring-primary-500 dark:bg-primary-500/10'
              : 'border-gray-200 bg-white hover:border-primary-300 dark:border-white/[0.1] dark:bg-dark-950',
          ]
    "
    @blur="onBlur"
    @keydown="onKeydown"
    @click="emit('select', $event)"
  >
    <!-- 紧凑模式：缩略图条只需要「图 + 状态」，细节留给主预览区 -->
    <div v-if="compact" class="relative aspect-square w-full bg-gray-100 dark:bg-white/[0.06]">
      <img
        v-if="task.status === 'running' && streamPreview"
        :src="streamPreview"
        :alt="t('imageStudio.task.streamPreview')"
        class="h-full w-full object-cover"
      />
      <StudioImageThumbnail
        v-else-if="outputImageId"
        :key="outputImageId"
        :image-id="outputImageId"
        :alt="t('imageStudio.task.preview')"
        @size="updateImageSize"
      />
      <span v-else class="flex h-full items-center justify-center text-[10px] text-gray-400">
        {{ task.status === 'running' ? t('imageStudio.status.running') : '—' }}
      </span>
      <span
        class="absolute left-1 top-1 h-1.5 w-1.5 rounded-full ring-2 ring-white/70 dark:ring-black/40"
        :class="dotClass"
        aria-hidden="true"
      />
    </div>

    <div v-else class="flex gap-3">
      <div
        class="mt-1 h-16 w-16 shrink-0 overflow-hidden rounded-md bg-gray-100 dark:bg-white/[0.06]"
      >
        <img
          v-if="task.status === 'running' && streamPreview"
          :src="streamPreview"
          :alt="t('imageStudio.task.streamPreview')"
          class="h-full w-full object-cover"
        />
        <StudioImageThumbnail
          v-else-if="outputImageId"
          :key="outputImageId"
          :image-id="outputImageId"
          :alt="t('imageStudio.task.preview')"
          @size="updateImageSize"
        />
        <span v-else class="flex h-full items-center justify-center text-xs text-gray-400">
          {{ task.status === 'running' ? t('imageStudio.status.running') : t('imageStudio.task.noImage') }}
        </span>
      </div>

      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center justify-between gap-1.5">
          <StudioTaskStatusBadge :task="task" />
          <time class="text-[11px] text-gray-500" :dateTime="createdIso">
            {{ createdLabel }}
          </time>
        </div>
        <p
          class="mt-1.5 line-clamp-2 min-h-10 break-words text-sm leading-5 text-gray-800 dark:text-dark-200"
        >
          {{ displayPrompt }}
        </p>
        <p class="mt-1 truncate text-xs text-gray-500" :title="metaTitle">
          {{ metaLabel }}
        </p>
      </div>
    </div>

    <div v-if="!compact" class="mt-2 flex items-center justify-between gap-2" @click.stop>
      <span class="text-[11px] text-gray-400">{{ timeLabel }}</span>
      <div class="flex items-center gap-1">
        <button
          ref="moreButtonRef"
          type="button"
          class="flex h-8 w-8 items-center justify-center rounded-md text-lg text-gray-500 hover:bg-gray-100 dark:hover:bg-white/[0.06]"
          :aria-label="t('imageStudio.task.moreActions')"
          :title="t('imageStudio.task.moreActions')"
          :aria-expanded="moreOpen"
          :aria-controls="`task-actions-${task.id}`"
          @click="moreOpen = !moreOpen"
        >
          ···
        </button>
      </div>
    </div>

    <div
      v-if="moreOpen && !compact"
      :id="`task-actions-${task.id}`"
      class="mt-1 flex flex-wrap gap-1 border-t border-gray-200 pt-2 text-xs dark:border-white/[0.1]"
      @click.stop
    >
      <button
        v-if="task.status === 'error' && available"
        type="button"
        class="inline-flex items-center gap-1.5 rounded px-2 py-2 text-primary-600 disabled:opacity-40"
        :disabled="hasRunningTask || retrying"
        @click="onRetry"
      >
        <StudioIcon name="RefreshIcon" class="h-4 w-4" />{{ t('imageStudio.task.retry') }}
      </button>
      <button
        type="button"
        class="inline-flex items-center gap-1.5 rounded px-2 py-2 hover:bg-gray-100 dark:hover:bg-white/[0.06]"
        @click="closeAnd('reuse')"
      >
        <StudioIcon name="CopyIcon" class="h-4 w-4" />{{ t('imageStudio.task.reuseParams') }}
      </button>
      <button
        v-if="task.outputImages.length > 0"
        type="button"
        class="inline-flex items-center gap-1.5 rounded px-2 py-2 disabled:opacity-40"
        :disabled="!canAddReference"
        @click="closeAnd('editOutputs')"
      >
        <StudioIcon name="EditIcon" class="h-4 w-4" />{{ t('imageStudio.task.asReference') }}
      </button>
      <button
        type="button"
        class="inline-flex items-center gap-1.5 rounded px-2 py-2 text-red-600"
        @click="closeAnd('delete')"
      >
        <StudioIcon name="TrashIcon" class="h-4 w-4" />{{ t('imageStudio.task.delete') }}
      </button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'
import { storeState as state, retryTask } from '../stores/store'
import { getImageModel } from '../lib/modelRegistry'
import { stripImageMentionMarkers } from '../lib/promptImageMentions'
import StudioIcon from './StudioIcon.vue'
import StudioImageThumbnail from './StudioImageThumbnail.vue'
import StudioTaskStatusBadge from './StudioTaskStatusBadge.vue'

const props = withDefaults(
  defineProps<{
    task: TaskRecord
    isSelected?: boolean
    /** 紧凑模式：只显示缩略图与状态点，用于底部缩略图轨道 */
    compact?: boolean
  }>(),
  { isSelected: false, compact: false },
)

const emit = defineEmits<{
  select: [event: MouseEvent]
  reuse: []
  editOutputs: []
  delete: []
}>()

const { t } = useI18n()

const moreOpen = ref(false)

/** compact 模式下左上角的状态点颜色 */
const dotClass = computed(() => {
  switch (props.task.status) {
    case 'done':
      return 'bg-emerald-500'
    case 'running':
      return 'bg-primary-500 animate-pulse'
    case 'error':
      return 'bg-red-500'
    default:
      return 'bg-gray-400'
  }
})
const retrying = ref(false)
const imageSize = ref<{ imageId: string; width: number; height: number } | null>(null)
const moreButtonRef = ref<HTMLButtonElement | null>(null)

const displayPrompt = computed(
  () => stripImageMentionMarkers(props.task.prompt) || t('imageStudio.task.noPrompt'),
)

const modelId = computed(
  () => props.task.modelId ?? getImageModel(props.task.apiModel ?? '')?.id,
)

const available = computed(
  () =>
    state.settings.miloRouter.connectionStatus === 'connected' &&
    state.catalogStatus === 'ready' &&
    state.modelCatalog.some((model) => model.id === modelId.value),
)

const canAddReference = computed(() => {
  if (state.catalogStatus !== 'ready') return false
  const model = getImageModel(state.settings.selectedModelId, state.modelCatalog)
  return state.inputImages.length < (model?.capabilities.referenceImages ?? 0)
})

const hasRunningTask = computed(() => state.tasks.some((item) => item.status === 'running'))
const streamPreview = computed(() => state.streamPreviews[props.task.id]?.image)

const modelName = computed(
  () => props.task.modelName || getImageModel(props.task.apiModel ?? '')?.name || t('imageStudio.task.legacyModel'),
)

const outputImageId = computed(() => props.task.outputImages[0] ?? '')

const resolutionLabel = computed(() => {
  if (imageSize.value?.imageId === outputImageId.value) {
    return `${imageSize.value.width} × ${imageSize.value.height}`
  }
  return outputImageId.value ? t('imageStudio.task.readingSize') : t('imageStudio.task.noResult')
})

const metaLabel = computed(() => {
  const count = props.task.status === 'done' ? props.task.outputImages.length : props.task.params.n
  return t('imageStudio.task.metaLine', {
    model: modelName.value,
    resolution: resolutionLabel.value,
    count,
  })
})

const metaTitle = computed(() => `${modelName.value} · ${resolutionLabel.value}`)

const createdIso = computed(() => new Date(props.task.createdAt).toISOString())
const createdLabel = computed(() =>
  new Date(props.task.createdAt).toLocaleString(undefined, {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }),
)

const timeLabel = computed(() => {
  if (props.task.elapsed != null) {
    return t('imageStudio.task.elapsedSeconds', { seconds: Math.floor(props.task.elapsed / 1000) })
  }
  return props.task.status === 'running' ? t('imageStudio.task.generating') : ''
})


function updateImageSize(size: { width: number; height: number }) {
  if (outputImageId.value) {
    imageSize.value = { imageId: outputImageId.value, ...size }
  }
}

function onBlur(event: FocusEvent) {
  const next = event.relatedTarget as Node | null
  if (!(event.currentTarget as HTMLElement).contains(next)) moreOpen.value = false
}

function onKeydown(event: KeyboardEvent) {
  if (event.target !== event.currentTarget) return
  if (!['Enter', ' '].includes(event.key)) return
  event.preventDefault()
  ;(event.currentTarget as HTMLElement).click()
}

function closeAnd(action: 'reuse' | 'editOutputs' | 'delete') {
  moreOpen.value = false
  if (action === 'reuse') emit('reuse')
  else if (action === 'editOutputs') emit('editOutputs')
  else emit('delete')
}

async function onRetry() {
  moreOpen.value = false
  retrying.value = true
  try {
    await retryTask(props.task)
  } finally {
    retrying.value = false
  }
}

/** 菜单打开时：点外部或按 ESC 收起 */
function onPointerDown(event: PointerEvent) {
  const target = event.target as Node
  const menu = document.getElementById(`task-actions-${props.task.id}`)
  if (menu?.contains(target)) return
  if (moreButtonRef.value?.contains(target)) return
  moreOpen.value = false
}

function onKeyDownGlobal(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  moreOpen.value = false
  moreButtonRef.value?.focus()
}

watch(moreOpen, (open) => {
  if (open) {
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDownGlobal)
  } else {
    document.removeEventListener('pointerdown', onPointerDown)
    document.removeEventListener('keydown', onKeyDownGlobal)
  }
})

onUnmounted(() => {
  document.removeEventListener('pointerdown', onPointerDown)
  document.removeEventListener('keydown', onKeyDownGlobal)
})
</script>
