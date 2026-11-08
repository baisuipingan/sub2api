<template>
  <button ref="element" type="button" class="group relative flex aspect-square w-full items-center justify-center overflow-hidden rounded-xl border border-gray-200 bg-gray-50 focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" :aria-label="t('admin.workbench.galleryViewImage', { number: number })" @click="open">
    <img v-if="thumbnail" :src="thumbnail.url" :alt="t('admin.workbench.galleryImageAlt', { number: number })" loading="lazy" decoding="async" referrerpolicy="no-referrer" class="h-full w-full object-cover transition-transform group-hover:scale-105" @error="imageFailed" />
    <span v-else class="px-2 text-center text-xs text-gray-500">{{ t(failed ? 'admin.workbench.galleryImageRetry' : 'common.loading') }}</span>
  </button>
  <BaseDialog :show="show" :title="t('admin.workbench.galleryViewImage', { number: number })" width="wide" @close="close">
    <div class="flex min-h-40 items-center justify-center rounded-xl bg-gray-50 dark:bg-dark-800">
      <img v-if="preview" :src="preview.url" :alt="t('admin.workbench.galleryImageAlt', { number: number })" referrerpolicy="no-referrer" class="max-h-[70vh] max-w-full object-contain" @error="previewImageFailed" />
      <button v-else-if="previewFailed" type="button" class="btn btn-secondary" @click="retryPreview">{{ t('common.tryAgain') }}</button>
      <LoadingSpinner v-else />
    </div>
    <p class="mt-3 text-xs text-gray-500">{{ t('admin.workbench.galleryPreviewHint') }}</p>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getWorkbenchGalleryImage, type WorkbenchGalleryImage, type WorkbenchGalleryTask } from '@/api/admin/workbench'

const props = defineProps<{ task: WorkbenchGalleryTask; imageId: string; number: number }>()
const { t } = useI18n()
const thumbnail = ref<WorkbenchGalleryImage | null>(null)
const preview = ref<WorkbenchGalleryImage | null>(null)
const failed = ref(false)
const previewFailed = ref(false)
const show = ref(false)
const controller = new AbortController()
let previewController: AbortController | null = null
let previewSequence = 0
let previewRetried = false
const element = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null
let thumbnailRetried = false
async function loadThumbnail() {
  failed.value = false
  try {
    const result = await getWorkbenchGalleryImage(props.task, props.imageId, 'thumbnail', controller.signal)
    if (!controller.signal.aborted) thumbnail.value = result
  }
  catch { if (!controller.signal.aborted) failed.value = true }
}
function imageFailed() {
  thumbnail.value = null
  if (!thumbnailRetried) { thumbnailRetried = true; void loadThumbnail() }
  else failed.value = true
}
async function loadPreview() {
  previewController?.abort()
  previewController = new AbortController()
  const current = ++previewSequence
  preview.value = null
  previewFailed.value = false
  try {
    const result = await getWorkbenchGalleryImage(props.task, props.imageId, 'preview', previewController.signal)
    if (current === previewSequence && show.value) preview.value = result
  } catch { if (current === previewSequence && show.value && !previewController?.signal.aborted) previewFailed.value = true }
}
function previewImageFailed() {
  preview.value = null
  if (!previewRetried) { previewRetried = true; void loadPreview() }
  else previewFailed.value = true
}
function retryPreview() { previewRetried = false; void loadPreview() }
function open() {
  if (failed.value) { thumbnailRetried = false; void loadThumbnail() }
  show.value = true
  previewRetried = false
  void loadPreview()
}
function close() {
  show.value = false
  previewSequence++
  previewController?.abort()
  previewController = null
  preview.value = null
  previewFailed.value = false
}
onMounted(() => {
  if (typeof IntersectionObserver !== 'function' || !element.value) { void loadThumbnail(); return }
  observer = new IntersectionObserver((entries) => {
    if (entries.some(entry => entry.isIntersecting)) { observer?.disconnect(); void loadThumbnail() }
  }, { rootMargin: '200px' })
  observer.observe(element.value)
})
onBeforeUnmount(() => { observer?.disconnect(); controller.abort(); close(); thumbnail.value = null })
</script>
