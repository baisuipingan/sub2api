<template>
  <StatusBadge
    :status="status"
    :label="label"
    class="inline-flex shrink-0 rounded px-2 py-1 font-medium [&>span:last-child]:text-xs [&>span:last-child]:text-inherit"
    :class="colorClass"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'
import StatusBadge from '@/components/common/StatusBadge.vue'

const props = defineProps<{ task: TaskRecord }>()
const { t } = useI18n()

const partial = computed(
  () => props.task.status === 'done' && Boolean(props.task.outputErrors?.length),
)
const status = computed(() => props.task.status === 'error' ? 'error' : props.task.status === 'running' || partial.value ? 'warning' : 'success')

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
