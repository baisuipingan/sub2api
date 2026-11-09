<template>
  <div class="studio-preview-shell" :class="{ 'studio-preview-single': !multiple }" role="region" :aria-label="t('imageStudio.result.previewAria')">
    <button
      v-if="multiple"
      type="button"
      class="studio-preview-arrow btn btn-secondary btn-icon"
      :aria-label="t('imageStudio.result.prevImage')"
      :title="t('imageStudio.result.prevImage')"
      :disabled="imageIndex <= 0"
      @click="selectImage(imageIndex - 1)"
    >
      <StudioIcon name="ChevronLeftIcon" size="lg" />
    </button>
    <div class="studio-frame-cell">
      <div class="studio-frame relative flex items-center justify-center overflow-hidden rounded-2xl bg-gray-100/80 ring-1 ring-inset ring-black/[0.04] dark:bg-white/[0.04] dark:ring-white/[0.06]">
        <template v-if="task.status === 'running'">
          <img v-if="streamPreview" :src="streamPreview" :alt="t('imageStudio.task.streamPreview')" class="h-full w-full object-contain" />
          <div v-else class="flex flex-col items-center gap-3 text-sm text-primary-600 dark:text-primary-400">
            <LoadingSpinner size="md" />
            {{ runningLabel }}
          </div>
          <span class="absolute bottom-3 left-3 rounded-full bg-black/60 px-2.5 py-1 text-[11px] text-white">{{ submissionStage ? runningLabel : t('imageStudio.result.rendering') }}</span>
        </template>
        <button
          v-else-if="source"
          type="button"
          class="group h-full w-full outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500"
          :aria-label="t('imageStudio.result.zoomAria')"
          @click="openLightbox"
        >
          <img :src="source" :data-image-id="imageId" :alt="t('imageStudio.result.imageAlt')" class="h-full w-full object-contain" @error="failed = true; source = null" />
        </button>
        <div v-else class="flex flex-col items-center gap-2 px-4 text-center text-sm text-gray-400">
          <StudioIcon v-if="task.status === 'error'" name="ImageUploadIcon" class="h-10 w-10 text-gray-300 dark:text-dark-600" />
          <LoadingSpinner v-else-if="loading" size="md" />
          <span>{{ task.status === 'error' ? t('imageStudio.result.noOutput') : failed ? t('imageStudio.result.imageUnavailable') : t('imageStudio.result.unavailable') }}</span>
        </div>
      </div>
    </div>
    <button
      v-if="multiple"
      type="button"
      class="studio-preview-arrow btn btn-secondary btn-icon"
      :aria-label="t('imageStudio.result.nextImage')"
      :title="t('imageStudio.result.nextImage')"
      :disabled="imageIndex >= task.outputImages.length - 1"
      @click="selectImage(imageIndex + 1)"
    >
      <StudioIcon name="ChevronRightIcon" size="lg" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'
import { storeState as state } from '../stores/store'
import { ensureImagePreviewUrl, getCachedPreviewUrl } from '../lib/imageCache'
import StudioIcon from './StudioIcon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = defineProps<{ task: TaskRecord; imageId?: string }>()
const emit = defineEmits<{ selectImage: [index: number] }>()
const { t } = useI18n()
const source = ref<string | null>(null)
const failed = ref(false)
const loading = ref(false)
const imageIndex = computed(() => Math.max(0, props.task.outputImages.indexOf(props.imageId ?? '')))
const multiple = computed(() => props.task.outputImages.length > 1)
const submissionStage = computed(() => state.studioSubmissionStage?.[props.task.id])
const runningLabel = computed(() => t(submissionStage.value === 'preparing' ? 'imageStudio.store.referencePreparing' : submissionStage.value === 'uploading' ? 'imageStudio.store.referenceUploading' : submissionStage.value === 'submitting' ? 'imageStudio.store.cloudSubmitting' : 'imageStudio.result.generating'))
const streamPreview = computed(() => state.streamPreviews[props.task.id]?.image)

watch(() => props.imageId, async (id, _previous, onCleanup) => {
  let active = true
  onCleanup(() => { active = false })
  source.value = id ? getCachedPreviewUrl(id) ?? null : null
  failed.value = false
  loading.value = Boolean(id && !source.value)
  if (!id || source.value) return
  try {
    const value = await ensureImagePreviewUrl(id)
    if (!active) return
    source.value = value ?? null
    failed.value = !value
  } catch {
    if (active) failed.value = true
  } finally {
    if (active) loading.value = false
  }
}, { immediate: true })

function selectImage(index: number) {
  if (index >= 0 && index < props.task.outputImages.length) emit('selectImage', index)
}
function openLightbox() {
  if (props.imageId) state.setLightboxImageId(props.imageId, props.task.outputImages)
}
</script>

<style scoped>
.studio-preview-shell {
  display: grid;
  grid-template-columns: 2.75rem minmax(0, 1fr) 2.75rem;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
}
.studio-preview-single { grid-template-columns: minmax(0, 1fr); }
.studio-frame-cell {
  display: grid;
  place-items: center;
  min-width: 0;
  min-height: 0;
  height: 100%;
}
.studio-frame {
  width: min(100%, 100cqh, 720px);
  aspect-ratio: 1;
}
.studio-preview-arrow {
  width: 2.75rem;
  height: 2.75rem;
  border-radius: 9999px;
  border-color: rgb(209 213 219);
  box-shadow: 0 2px 8px rgb(0 0 0 / 0.08);
}
.studio-preview-arrow:disabled { opacity: 0.3; }
@media (max-width: 479px) {
  .studio-preview-shell { grid-template-columns: 2rem minmax(0, 1fr) 2rem; gap: 0.25rem; }
  .studio-preview-single { grid-template-columns: minmax(0, 1fr); }
  .studio-preview-arrow { width: 2rem; height: 2.5rem; padding: 0; }
}
@supports not (width: 1cqh) {
  .studio-frame { width: min(100%, 62vh, 720px); }
}
</style>
