<template>
  <span ref="element" class="flex h-16 w-24 max-w-full items-center justify-start overflow-hidden" aria-hidden="true">
    <img
      v-if="url" :src="url" alt="" decoding="async" referrerpolicy="no-referrer"
      class="h-full max-w-full rounded-md object-contain object-left" @error="url = ''"
    />
    <Icon v-else name="document" size="lg" class="text-primary-500" />
  </span>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { workbenchChatApi } from '@/api/workbenchChat'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{ versionId: string }>()
const auth = useAuthStore()
const element = ref<HTMLElement | null>(null)
const visible = ref(false)
const url = ref('')
let observer: IntersectionObserver | undefined

watch(() => [visible.value, props.versionId, auth.user?.id] as const, async ([inView, versionId, userId], _, onCleanup) => {
  url.value = ''
  if (!inView || !userId) return
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  try {
    const access = await workbenchChatApi.access(versionId, controller.signal)
    if (!controller.signal.aborted && access.preview_kind === 'image' && access.preview_url) {
      url.value = access.preview_url
    }
  } catch {
    // Keep the file card usable when its optional thumbnail cannot be loaded.
  }
}, { flush: 'sync' })

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined') {
    visible.value = true
    return
  }
  observer = new IntersectionObserver(entries => {
    if (!entries.some(entry => entry.isIntersecting)) return
    observer?.disconnect()
    visible.value = true
  }, { rootMargin: '160px' })
  if (element.value) observer.observe(element.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>
