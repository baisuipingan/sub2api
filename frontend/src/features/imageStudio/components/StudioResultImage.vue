<template>
  <div
    class="result-image-stage flex w-full items-center justify-center overflow-hidden bg-gray-50 dark:bg-black/20"
  >
    <div v-if="failed" class="p-6 text-center text-sm text-gray-500" role="status">
      <p>{{ t('imageStudio.result.imageUnavailable') }}</p>
      <button
        type="button"
        class="mt-3 inline-flex items-center gap-2 font-medium text-primary-600 hover:underline"
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

    <span
      v-else
      role="status"
      :aria-label="t('imageStudio.result.loadingImage')"
      class="h-8 w-8 animate-spin rounded-full border-2 border-gray-200 border-t-primary-500"
    />
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import { storeState as state } from '../stores/store'
import { deleteCachedImage, ensureImageCached } from '../lib/imageCache'

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

let cancelled = false

async function load() {
  cancelled = false
  source.value = null
  failed.value = false
  try {
    const value = await ensureImageCached(props.imageId)
    if (cancelled) return
    source.value = value ?? null
    failed.value = !value
  } catch {
    if (!cancelled) failed.value = true
  }
}

watch(() => [props.imageId, attempt.value], load, { immediate: true })
onUnmounted(() => {
  cancelled = true
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
