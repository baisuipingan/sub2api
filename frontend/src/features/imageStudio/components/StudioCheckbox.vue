<template>
  <label class="flex cursor-pointer items-center gap-2.5 text-sm text-gray-700 dark:text-dark-200">
    <Toggle :model-value="checked" :aria-label="label || undefined" @update:model-value="emit('change', $event)" />
    <span v-if="label">{{ label }}</span>
  </label>
</template>

<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'

withDefaults(defineProps<{
  checked: boolean
  label?: string
  tone?: 'primary' | 'danger'
}>(), { tone: 'primary', label: undefined })

const emit = defineEmits<{ change: [checked: boolean] }>()
</script>
