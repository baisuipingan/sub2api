<template>
  <svg v-if="name === 'ImageUploadIcon'" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
    <!-- The shared icon set has no image-with-upload glyph. -->
    <rect x="3" y="4" width="18" height="16" rx="2.5" stroke-width="1.8" />
    <circle cx="8.5" cy="9" r="1.5" stroke-width="1.8" />
    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="m5.5 17 4.25-4.25 2.75 2.75 2.25-2.25L18.5 17M16.5 4v5m-2-2 2 2 2-2" />
  </svg>
  <Icon v-else :name="sharedNames[name]" :size="sharedSize" :style="fractionalSize" aria-hidden="true" />
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import type { IconName } from '@/components/icons/Icon.vue'
import { computed, useAttrs } from 'vue'

const sharedNames = {
  CopyIcon: 'copy',
  TrashIcon: 'trash',
  PlusIcon: 'plus',
  CloseIcon: 'x',
  ChevronLeftIcon: 'chevronLeft',
  ChevronRightIcon: 'chevronRight',
  ExportIcon: 'download',
  ImportIcon: 'upload',
  DownloadIcon: 'download',
  EditIcon: 'edit',
  RefreshIcon: 'refresh',
  DragHandleIcon: 'menu',
  SettingsIcon: 'cog',
  CreditIcon: 'dollar',
  SearchIcon: 'search',
  ClockIcon: 'clock',
} as const satisfies Record<string, IconName>

type StudioIconName = keyof typeof sharedNames | 'ImageUploadIcon'
const props = withDefaults(defineProps<{
  name: StudioIconName
  size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl'
}>(), { size: 'sm' })
const attrs = useAttrs()
// Preserve existing compact and canvas-toolbar dimensions while using Icon's
// public size API. The shared component has no 14px size preset.
const classes = computed(() => String(attrs.class ?? '').split(/\s+/))
const sharedSize = computed(() => classes.value.includes('h-3') ? 'xs' : props.size)
const fractionalSize = computed(() => classes.value.includes('h-3.5') ? { width: '0.875rem', height: '0.875rem' } : undefined)
</script>
