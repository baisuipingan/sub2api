<template>
  <span
    class="inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-md px-2 py-0.5 text-[11px] font-medium"
    :class="variantClass"
  >
    <span class="h-1.5 w-1.5 rounded-full" :class="dotClass" aria-hidden="true" />
    {{ t(`tickets.status.${status}`) }}
  </span>
</template>

<script setup lang="ts">
/**
 * 工单状态徽章。
 *
 * 用户侧列表、管理端表格、详情弹窗三处共用，避免各写一套颜色映射后走样。
 * 状态只有三种，用「底色 + 圆点」表达：待处理（蓝）、待用户回复（绿）、已关闭（灰）。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TicketStatus } from '@/api/tickets'

const props = defineProps<{ status: TicketStatus }>()

const { t } = useI18n()

const variantClass = computed(() => {
  switch (props.status) {
    case 'pending':
      return 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
    case 'replied':
      return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300'
    default:
      return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
  }
})

const dotClass = computed(() => {
  switch (props.status) {
    case 'pending':
      return 'bg-primary-500'
    case 'replied':
      return 'bg-emerald-500'
    default:
      return 'bg-gray-400 dark:bg-dark-400'
  }
})
</script>
