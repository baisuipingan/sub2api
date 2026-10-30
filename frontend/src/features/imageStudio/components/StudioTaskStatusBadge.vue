<template>
  <span
    class="inline-flex shrink-0 items-center gap-1.5 rounded px-2 py-1 text-xs font-medium"
    :class="colorClass"
  >
    <span aria-hidden="true" class="h-1.5 w-1.5 rounded-full bg-current" />
    {{ label }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'

const props = defineProps<{ task: TaskRecord }>()
const { t } = useI18n()

const partial = computed(
  () => props.task.status === 'done' && Boolean(props.task.outputErrors?.length),
)

const colorClass = computed(() => {
  if (props.task.status === 'running') {
    return 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
  }
  if (props.task.status === 'error') {
    return 'bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-300'
  }
  if (partial.value) {
    return 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300'
  }
  return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300'
})

const label = computed(() => {
  if (props.task.status === 'running') return t('imageStudio.status.running')
  if (props.task.status === 'error') return t('imageStudio.status.error')
  if (partial.value) return t('imageStudio.status.partial')
  return t('imageStudio.status.done')
})
</script>
