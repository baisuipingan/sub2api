<template>
  <img
    v-if="source"
    :src="source"
    :data-image-id="imageId"
    :alt="alt"
    class="h-full w-full object-cover"
    loading="lazy"
    @error="source = null"
  />
  <span v-else class="flex h-full w-full items-center justify-center text-[10px] text-gray-400">
    {{ t('imageStudio.thumbnail.placeholder') }}
  </span>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ensureImageThumbnailCached, subscribeImageThumbnail } from '../lib/imageCache'

/**
 * 缩略图。首屏只读缓存/订阅，不阻塞主流程。
 *
 * 迁移说明：原实现用 cancelled 标记防竞态，这里改成每次 watch
 * 重建订阅，语义等价且更直观。
 */
const props = withDefaults(
  defineProps<{
    imageId: string
    alt?: string
  }>(),
  { alt: '' },
)

const emit = defineEmits<{
  size: [size: { width: number; height: number }]
}>()

const { t } = useI18n()
const source = ref<string | null>(null)

let cancelled = false
let unsubscribe: (() => void) | null = null

function teardown() {
  cancelled = true
  unsubscribe?.()
  unsubscribe = null
}

function load(id: string) {
  teardown()
  cancelled = false
  source.value = null

  unsubscribe = subscribeImageThumbnail(id, (value) => {
    if (cancelled) return
    source.value = value.dataUrl
    if (value.width && value.height) {
      emit('size', { width: value.width, height: value.height })
    }
  })

  void ensureImageThumbnailCached(id)
    .then((value) => {
      if (cancelled || !value) return
      source.value = value.dataUrl
      if (value.width && value.height) {
        emit('size', { width: value.width, height: value.height })
      }
    })
    .catch(() => {
      /* 后台补建后仍可通过订阅恢复 */
    })
}

watch(() => props.imageId, load, { immediate: true })
onUnmounted(teardown)
</script>
