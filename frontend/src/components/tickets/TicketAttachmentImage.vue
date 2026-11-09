<template>
  <div ref="host" class="flex min-h-20 items-center justify-center" :class="variant === 'thumbnail' ? 'h-24 w-32' : 'min-h-40'">
    <img v-if="url" :src="url" :alt="alt" :class="variant === 'thumbnail' ? 'h-24 w-32 object-contain' : 'mx-auto max-h-[72vh] max-w-full object-contain'" referrerpolicy="no-referrer" @error="imageFailed" />
    <div v-else-if="failed" class="flex flex-col items-center gap-2 p-3 text-xs text-gray-500 dark:text-dark-400">
      <span>{{ t('tickets.attachments.loadFailed') }}</span>
      <button type="button" class="text-primary-600 hover:underline" @click.stop="load">{{ t('tickets.attachments.retry') }}</button>
    </div>
    <LoadingSpinner v-else size="sm" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getTicketAttachmentURL } from '@/api/tickets'
import { loadPrivateImageVariant } from '@/lib/privateImageVariantCache'

const props = withDefaults(defineProps<{ ticketId: number; ownerId: number; attachmentId: string; variant: 'thumbnail' | 'preview'; alt: string; admin?: boolean }>(), { admin: false })
const { t } = useI18n()
const auth = useAuthStore()
const host = ref<HTMLElement>()
const visible = ref(props.variant === 'preview')
let observer: IntersectionObserver | undefined
const url = ref('')
const failed = ref(false)
let revision = 0
let abort: AbortController | undefined
async function load() {
  abort?.abort()
  const controller = new AbortController()
  abort = controller
  const version = ++revision
  url.value = ''; failed.value = false
  const viewerId = auth.user?.id
  if (!viewerId) { failed.value = true; return }
  try {
    const asset = await loadPrivateImageVariant({
      viewerId, ownerId: props.ownerId, imageId: `ticket:${props.ticketId}:${props.attachmentId}`, variant: props.variant,
      authorize: () => getTicketAttachmentURL(props.ticketId, props.attachmentId, props.variant, props.admin),
      isCurrent: () => version === revision && auth.user?.id === viewerId,
      signal: controller.signal,
      previewMaxDimension: 2560
    })
    if (version === revision) url.value = asset.url
  } catch (error) {
    if (version === revision && !(error instanceof DOMException && error.name === 'AbortError')) failed.value = true
  }
}
function imageFailed() { url.value = ''; failed.value = true }
watch(() => [props.ticketId, props.attachmentId, props.variant, props.admin, auth.user?.id, visible.value], () => {
  revision++; abort?.abort(); url.value = ''; failed.value = false
  if (visible.value) void load()
}, { immediate: true })
onMounted(() => {
  if (visible.value) return
  if (typeof IntersectionObserver === 'undefined') { visible.value = true; return }
  observer = new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) { visible.value = true; observer?.disconnect() }
  }, { rootMargin: '160px' })
  if (host.value) observer.observe(host.value)
})
onUnmounted(() => { revision++; abort?.abort(); observer?.disconnect() })
</script>
