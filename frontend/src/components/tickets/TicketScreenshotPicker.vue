<template>
  <div class="space-y-2" @paste.stop="paste" @dragover.prevent @drop.prevent="drop">
    <div class="flex flex-wrap items-center gap-3">
      <input ref="input" type="file" class="sr-only" accept="image/png,image/jpeg,image/webp" multiple :disabled="disabled || busy || !uploadsEnabled" @change="select" />
      <button type="button" class="btn btn-secondary !py-1.5 text-xs" :disabled="disabled || busy || !uploadsEnabled || modelValue.length >= TICKET_MAX_FILES" @click="input?.click()">
        <LoadingSpinner v-if="busy" size="sm" />
        <Icon v-else name="plus" size="sm" />
        {{ busy ? t('tickets.attachments.preparing') : t('tickets.attachments.add') }}
        <span class="tabular-nums">{{ modelValue.length }}/{{ TICKET_MAX_FILES }}</span>
      </button>
      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('tickets.attachments.hint') }}</p>
    </div>
    <p v-if="!uploadsEnabled" class="text-xs text-amber-600 dark:text-amber-400">{{ t('tickets.attachments.disabledError') }}</p>
    <p class="text-[11px] text-gray-400 dark:text-dark-500">{{ t('tickets.attachments.localHint') }}</p>
    <div v-if="previews.length" class="grid max-w-sm grid-cols-3 gap-2">
      <div v-for="(preview, index) in previews" :key="preview.url" class="relative overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
        <img :src="preview.url" class="h-20 w-full object-contain bg-gray-50 dark:bg-dark-900" :alt="t('tickets.attachments.screenshot', { number: index + 1 })" />
        <button type="button" class="absolute right-1 top-1 rounded-full bg-gray-900/75 p-1 text-white hover:bg-gray-900 disabled:opacity-40" :disabled="disabled || busy" :aria-label="t('tickets.attachments.remove', { number: index + 1 })" @click="remove(index)">
          <Icon name="x" size="sm" />
        </button>
        <p class="px-2 py-0.5 text-[10px] text-gray-500 dark:text-dark-400">{{ Math.ceil(preview.bytes / 1024) }} KB</p>
      </div>
    </div>
    <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { prepareTicketImage, TicketImageError, TICKET_MAX_FILES } from '@/lib/ticketAttachments'

const props = withDefaults(defineProps<{ modelValue: File[]; disabled?: boolean; uploadsEnabled?: boolean }>(), { disabled: false, uploadsEnabled: true })
const emit = defineEmits<{ (e: 'update:modelValue', files: File[]): void; (e: 'busy', value: boolean): void }>()
const { t } = useI18n()
const input = ref<HTMLInputElement>()
const busy = ref(false)
const error = ref('')
const previews = ref<{ url: string; bytes: number }[]>([])
let alive = true
watch(() => props.modelValue, files => {
  for (const item of previews.value) URL.revokeObjectURL(item.url)
  previews.value = files.map(file => ({ url: URL.createObjectURL(file), bytes: file.size }))
}, { immediate: true })
onUnmounted(() => {
  alive = false
  for (const item of previews.value) URL.revokeObjectURL(item.url)
})
async function add(files: File[]) {
  if (!files.length || props.disabled || !props.uploadsEnabled || busy.value) return
  error.value = ''
  if (props.modelValue.length + files.length > TICKET_MAX_FILES) { error.value = t('tickets.attachments.countError'); return }
  busy.value = true; emit('busy', true)
  try {
    const prepared: File[] = []
    for (const file of files) prepared.push(await prepareTicketImage(file))
    if (alive) emit('update:modelValue', [...props.modelValue, ...prepared])
  } catch (cause) {
    if (alive) error.value = t(`tickets.attachments.${cause instanceof TicketImageError ? cause.reason : 'compress'}Error`)
  } finally {
    busy.value = false
    if (alive) emit('busy', false)
  }
}
function select(event: Event) {
  const target = event.target as HTMLInputElement
  const files = Array.from(target.files || [])
  target.value = ''
  void add(files)
}
function drop(event: DragEvent) { void add(Array.from(event.dataTransfer?.files || [])) }
function paste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.files || [])
  if (files.length && !props.disabled && props.uploadsEnabled && !busy.value) { event.preventDefault(); void add(files) }
}
function remove(index: number) {
  emit('update:modelValue', props.modelValue.filter((_, position) => position !== index))
  error.value = ''
}
defineExpose({ paste })
</script>
