<template>
  <img
    v-if="thumbnail"
    :src="thumbnail"
    alt=""
    class="h-full w-full object-cover"
    loading="lazy"
  />
  <div
    v-else
    class="flex h-full w-full items-center justify-center bg-amber-50 text-amber-400 dark:bg-amber-500/[0.08]"
  >
    <StudioIcon
      v-if="isVirtualAll"
      name="FavoriteIcon"
      class="h-10 w-10 fill-current opacity-80"
    />
    <StudioIcon v-else name="ImageUploadIcon" class="h-10 w-10 opacity-70" />
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import StudioIcon from '../StudioIcon.vue'
import type { TaskRecord } from '../../types'
import { ensureImageThumbnailCached, subscribeImageThumbnail } from '../../lib/imageCache'

/** 收藏夹封面：取最新一张有结果的缩略图，没有则显示占位图标 */
const props = defineProps<{
  task?: TaskRecord
  isVirtualAll: boolean
}>()

const thumbnail = ref<string | null>(null)

let cancelled = false
let unsubscribe: (() => void) | null = null

function teardown() {
  cancelled = true
  unsubscribe?.()
  unsubscribe = null
}

function load() {
  teardown()
  cancelled = false
  thumbnail.value = null

  const imageId = props.task?.outputImages?.[0]
  if (!imageId) return

  unsubscribe = subscribeImageThumbnail(imageId, (value) => {
    if (!cancelled) thumbnail.value = value.dataUrl
  })
  void ensureImageThumbnailCached(imageId).then((value) => {
    if (!cancelled && value) thumbnail.value = value.dataUrl
  })
}

watch(() => props.task?.outputImages?.[0], load, { immediate: true })
onUnmounted(teardown)
</script>
