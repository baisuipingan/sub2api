<template>
  <div
    v-if="imageId && src"
    role="dialog"
    aria-modal="true"
    :aria-label="t('imageStudio.lightbox.aria')"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-black/75 p-4"
    @click="state.setLightboxImageId(null)"
  >
    <div
      class="relative flex h-full w-full items-center justify-center"
      @click.stop
    >
      <button
        type="button"
        class="absolute right-2 top-2 z-10 rounded-full bg-black/50 p-2 text-white transition hover:bg-black/75"
        :aria-label="t('imageStudio.lightbox.close')"
        :title="t('imageStudio.common.close')"
        @click="state.setLightboxImageId(null)"
      >
        <StudioIcon name="CloseIcon" class="h-5 w-5" />
      </button>

      <div class="relative max-h-full max-w-full">
        <img
          :src="src"
          :data-image-id="imageId"
          alt=""
          class="max-h-[86vh] max-w-[92vw] object-contain"
          @click.stop
        />
        <img
          v-if="maskPreview"
          :src="maskPreview"
          alt=""
          class="pointer-events-none absolute inset-0 h-full w-full object-contain"
        />
      </div>

      <template v-if="canNavigate">
        <button
          type="button"
          class="absolute left-2 top-1/2 rounded-full bg-black/50 p-2 text-white transition hover:bg-black/75"
          :aria-label="t('imageStudio.result.prevImage')"
          :title="t('imageStudio.result.prevImage')"
          @click="goTo(currentIndex - 1)"
        >
          <StudioIcon name="ChevronLeftIcon" class="h-6 w-6" />
        </button>
        <button
          type="button"
          class="absolute right-2 top-1/2 rounded-full bg-black/50 p-2 text-white transition hover:bg-black/75"
          :aria-label="t('imageStudio.result.nextImage')"
          :title="t('imageStudio.result.nextImage')"
          @click="goTo(currentIndex + 1)"
        >
          <StudioIcon name="ChevronRightIcon" class="h-6 w-6" />
        </button>
        <span
          class="absolute bottom-4 left-1/2 -translate-x-1/2 rounded bg-black/60 px-2 py-1 text-xs text-white"
        >
          {{ currentIndex + 1 }} / {{ imageList.length }}
        </span>
      </template>

      <div
        v-if="isInputImage"
        class="absolute bottom-4 left-1/2 flex -translate-x-1/2 gap-2 rounded-xl bg-black/65 p-2"
      >
        <input
          ref="fileRef"
          type="file"
          accept="image/*"
          class="hidden"
          @change="handleReplace"
        />
        <button
          type="button"
          class="flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm text-white transition hover:bg-white/15"
          @click="fileRef?.click()"
        >
          <StudioIcon name="RefreshIcon" class="h-4 w-4" />{{ t('imageStudio.lightbox.replace') }}
        </button>
        <button
          v-if="supportsMask"
          type="button"
          class="flex items-center gap-1.5 rounded-lg bg-primary-600 px-3 py-2 text-sm text-white transition hover:bg-primary-700"
          @click="openMaskEditor"
        >
          <StudioIcon name="EditIcon" class="h-4 w-4" />{{ t('imageStudio.lightbox.editMask') }}
        </button>
      </div>

      <div
        v-if="!isInputImage"
        class="absolute bottom-4 right-4 rounded-xl bg-black/65 p-2"
      >
        <button
          type="button"
          class="flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm text-white transition hover:bg-white/15"
          :aria-label="t('imageStudio.lightbox.download')"
          :title="t('imageStudio.lightbox.download')"
          @click="handleDownload"
        >
          <StudioIcon name="DownloadIcon" class="h-4 w-4" />{{ t('imageStudio.lightbox.download') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import {
  storeState as state,
  createInputImageFromFile,
  deleteImageIfUnreferenced,
} from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'
import { createMaskPreviewDataUrl } from '../lib/canvasImage'
import { downloadImageSource } from '../lib/downloadImage'
import { ensureImageCached, getCachedImage } from '../lib/imageCache'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'

const { t } = useI18n()

const fileRef = ref<HTMLInputElement | null>(null)
const src = ref('')
const maskPreview = ref('')

const imageId = computed(() => state.lightboxImageId)
const imageList = computed(() => state.lightboxImageList)

const isInputImage = computed(
  () => Boolean(imageId.value && state.inputImages.some((image) => image.id === imageId.value)),
)

const supportsMask = computed(
  () => getImageModelCapabilities(state.settings.selectedModelId, state.modelCatalog).mask,
)

const currentIndex = computed(() =>
  imageId.value ? imageList.value.indexOf(imageId.value) : -1,
)
const canNavigate = computed(() => imageList.value.length > 1)

const open = computed(() => Boolean(imageId.value))

useCloseOnEscape(() => open.value, () => state.setLightboxImageId(null))
usePreventBackgroundScroll(() => open.value)

let cancelled = false

/** 读图片本体 */
watch(
  imageId,
  (id) => {
    cancelled = false
    src.value = ''
    maskPreview.value = ''
    if (!id) return

    const cached = getCachedImage(id)
    if (cached) {
      src.value = cached
      return
    }
    void ensureImageCached(id).then((value) => {
      if (!cancelled && value) src.value = value
    })
  },
  { immediate: true },
)

/** 叠加遮罩预览（有遮罩草稿或历史遮罩时） */
watch(
  [imageId, src],
  async ([id, currentSrc]) => {
    if (!id || !currentSrc) return
    cancelled = false
    try {
      const taskMaskId = [...state.tasks]
        .reverse()
        .find((task) => task.maskTargetImageId === id && task.maskImageId)?.maskImageId

      const currentMask =
        state.maskDraft?.targetImageId === id
          ? state.maskDraft.maskDataUrl
          : taskMaskId
            ? (getCachedImage(taskMaskId) ?? (await ensureImageCached(taskMaskId)) ?? '')
            : ''

      if (!currentMask) return
      const preview = await createMaskPreviewDataUrl(currentSrc, currentMask)
      if (!cancelled) maskPreview.value = preview
    } catch {
      if (!cancelled) maskPreview.value = ''
    }
  },
  { immediate: true },
)

onUnmounted(() => {
  cancelled = true
})

function goTo(index: number) {
  if (!canNavigate.value) return
  const next = (index + imageList.value.length) % imageList.value.length
  state.setLightboxImageId(imageList.value[next], imageList.value)
}

function openMaskEditor() {
  if (!imageId.value) return
  const id = imageId.value
  state.setLightboxImageId(null)
  state.setMaskEditorImageId(id)
}

async function handleReplace(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  try {
    const image = await createInputImageFromFile(file)
    if (!image) {
      state.showToast(t('imageStudio.lightbox.invalidImage'), 'error')
      return
    }
    const targetIndex = state.inputImages.findIndex((item) => item.id === imageId.value)
    if (targetIndex < 0) return

    state.replaceInputImage(targetIndex, image)
    const nextList = imageList.value.map((id) => (id === imageId.value ? image.id : id))
    state.setLightboxImageId(image.id, nextList)
    const oldId = imageId.value
    if (oldId) void deleteImageIfUnreferenced(oldId)
    state.showToast(t('imageStudio.lightbox.replaced'), 'success')
  } catch (error) {
    state.showToast(
      error instanceof Error ? error.message : t('imageStudio.lightbox.replaceFailed'),
      'error',
    )
  }
}

async function handleDownload() {
  try {
    await downloadImageSource(src.value)
    state.showToast(t('imageStudio.lightbox.downloaded'), 'success')
  } catch (error) {
    console.error(error)
    state.showToast(t('imageStudio.lightbox.downloadFailed'), 'error')
  }
}
</script>
