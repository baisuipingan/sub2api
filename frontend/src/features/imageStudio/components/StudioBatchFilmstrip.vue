<template>
  <section v-if="images.length > 1" class="min-w-0" :aria-label="t('imageStudio.batch.aria')" data-testid="batch-filmstrip">
    <div class="mb-2 flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-400">
      <h3 class="font-medium text-gray-700 dark:text-dark-200">{{ t('imageStudio.batch.title') }}</h3>
      <span aria-live="polite" class="shrink-0 tabular-nums">{{ t('imageStudio.batch.position', { index: selectedIndex + 1, total: images.length }) }}</span>
    </div>
    <div
      ref="listRef"
      class="flex min-w-0 gap-2 overflow-x-auto overscroll-x-contain px-0.5 py-1 [scrollbar-width:thin]"
      role="listbox"
      :aria-label="t('imageStudio.batch.aria')"
      aria-orientation="horizontal"
      @keydown="onKeyDown"
    >
      <button
        v-for="(image, index) in images"
        :key="image"
        :ref="(element) => setButton(image, element)"
        type="button"
        role="option"
        class="relative h-14 w-14 shrink-0 overflow-hidden rounded-lg border-2 bg-gray-100 outline-none transition focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:bg-white/[0.05] dark:focus-visible:ring-offset-dark-950 sm:h-16 sm:w-16"
        :class="image === selectedImageId ? 'border-primary-500 ring-1 ring-primary-500' : 'border-transparent hover:border-gray-300 dark:hover:border-dark-500'"
        :aria-selected="image === selectedImageId"
        :aria-label="t('imageStudio.batch.selectImage', { index: index + 1, total: images.length })"
        :tabindex="index === selectedIndex ? 0 : -1"
        @click="emit('selectImage', index)"
      >
        <StudioImageThumbnail :image-id="image" />
        <span class="pointer-events-none absolute bottom-0.5 right-0.5 rounded bg-black/65 px-1 text-[10px] leading-4 text-white">{{ index + 1 }}</span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioImageThumbnail from './StudioImageThumbnail.vue'

const props = defineProps<{ images: string[]; selectedImageId: string | null }>()
const emit = defineEmits<{ selectImage: [index: number] }>()
const { t } = useI18n()
const buttons = new Map<string, HTMLButtonElement>()
const listRef = ref<HTMLDivElement | null>(null)
const selectedIndex = computed(() => Math.max(0, props.images.indexOf(props.selectedImageId ?? '')))

function setButton(id: string, element: Element | ComponentPublicInstance | null) {
  if (element instanceof HTMLButtonElement) buttons.set(id, element)
  else buttons.delete(id)
}

watch(() => [props.selectedImageId, props.images] as const, async () => {
  await nextTick()
  const id = props.images[selectedIndex.value]
  const list = listRef.value
  const button = id ? buttons.get(id) : null
  if (!list || !button) return
  const rail = list.getBoundingClientRect()
  const item = button.getBoundingClientRect()
  if (item.left < rail.left) list.scrollLeft += item.left - rail.left
  else if (item.right > rail.right) list.scrollLeft += item.right - rail.right
}, { immediate: true })

async function onKeyDown(event: KeyboardEvent) {
  let index = selectedIndex.value
  switch (event.key) {
    case 'ArrowLeft': index = Math.max(0, index - 1); break
    case 'ArrowRight': index = Math.min(props.images.length - 1, index + 1); break
    case 'Home': index = 0; break
    case 'End': index = props.images.length - 1; break
    default: return
  }
  event.preventDefault()
  emit('selectImage', index)
  await nextTick()
  buttons.get(props.images[index])?.focus()
}
</script>
