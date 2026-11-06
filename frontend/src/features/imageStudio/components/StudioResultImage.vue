<template>
  <div
    class="result-image-stage mx-auto flex max-w-full items-center justify-center overflow-hidden rounded-xl bg-gray-50 dark:bg-black/20"
    :style="{ maxHeight: 'min(42vh, 28rem)', maxWidth: 'min(42rem, 100%)' }"
  >
    <div v-if="failed" class="p-6 text-center text-sm text-gray-500" role="status">
      <p>{{ t('imageStudio.result.imageUnavailable') }}</p>
      <button
        type="button"
        class="btn btn-secondary btn-sm mt-3 gap-2"
        @click="reload"
      >
        <StudioIcon name="RefreshIcon" class="h-4 w-4" />
        {{ t('imageStudio.result.reloadImage') }}
      </button>
    </div>

    <button
      v-else-if="source"
      type="button"
      class="flex h-full w-full items-center justify-center"
      :aria-label="t('imageStudio.result.zoomAria')"
      @click="state.setLightboxImageId(imageId, images)"
    >
      <img
        :src="source"
        :data-image-id="imageId"
        :alt="t('imageStudio.result.imageAlt')"
        class="h-full w-full object-contain"
        @error="failed = true"
        @load="onLoad"
      />
    </button>

    <LoadingSpinner
      v-else
      :aria-label="t('imageStudio.result.loadingImage')"
      size="md"
    />
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { storeState as state } from '../stores/store'
import { deleteCachedImage, ensureImagePreviewUrl } from '../lib/imageCache'

/**
 * 结果大图。负责三件事：读缓存、加载失败可重试、点击放大到灯箱。
 *
 * 迁移说明：原实现用 attempt 计数触发重载，这里保留同样机制
 * （watch 依赖 [imageId, attempt]）。
 */
const props = defineProps<{
  imageId: string
  images: string[]
}>()

const emit = defineEmits<{ size: [size: { width: number; height: number }] }>()

const { t } = useI18n()
const source = ref<string | null>(null)
const failed = ref(false)
const attempt = ref(0)

let imageRequest = 0

async function load() {
  const request = ++imageRequest
  source.value = null
  failed.value = false
  try {
    const value = await ensureImagePreviewUrl(props.imageId)
    if (request !== imageRequest) return
    source.value = value ?? null
    failed.value = !value
  } catch {
    if (request === imageRequest) failed.value = true
  }
}

watch(() => [props.imageId, attempt.value], load, { immediate: true })
onUnmounted(() => {
  imageRequest += 1
})

function reload() {
  deleteCachedImage(props.imageId)
  attempt.value += 1
}

function onLoad(event: Event) {
  const img = event.currentTarget as HTMLImageElement
  emit('size', { width: img.naturalWidth, height: img.naturalHeight })
}
</script>
