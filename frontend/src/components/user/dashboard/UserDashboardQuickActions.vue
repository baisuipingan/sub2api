<template>
  <div class="card">
    <div class="flex items-center justify-between border-b border-gray-200/70 px-4 py-3 dark:border-dark-700/60">
      <h2 class="text-[13px] font-semibold text-gray-900 dark:text-white">
        {{ t('dashboard.quickActions') }}
      </h2>
    </div>

    <div class="grid grid-cols-1 gap-2 p-3 sm:grid-cols-2">
      <button
        v-for="action in actions"
        :key="action.path"
        type="button"
        class="qa-tile"
        @click="router.push(action.path)"
      >
        <span class="qa-tile-icon">
          <Icon :name="action.icon" size="sm" :stroke-width="1.9" />
        </span>
        <span class="qa-tile-body">
          <span class="qa-tile-title">{{ action.title }}</span>
          <span class="qa-tile-desc">{{ action.desc }}</span>
        </span>
        <Icon name="chevronRight" size="sm" class="qa-tile-arrow" :stroke-width="1.9" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon, { type IconName } from '@/components/icons/Icon.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'

const router = useRouter()
const { t } = useI18n()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

interface QuickAction {
  path: string
  icon: IconName
  title: string
  desc: string
}

/**
 * 快捷操作列表。
 * 图标统一走中性灰（.qa-tile-icon），不用彩色 —— 颜色留给状态和主操作。
 * 批量生图按权限插入到第 3 位。
 */
const actions = computed<QuickAction[]>(() => {
  const list: QuickAction[] = [
    {
      path: '/keys',
      icon: 'key',
      title: t('dashboard.createApiKey'),
      desc: t('dashboard.generateNewKey'),
    },
    {
      path: '/usage',
      icon: 'chart',
      title: t('dashboard.viewUsage'),
      desc: t('dashboard.checkDetailedLogs'),
    },
    {
      path: '/redeem',
      icon: 'gift',
      title: t('dashboard.redeemCode'),
      desc: t('dashboard.addBalanceWithCode'),
    },
  ]

  if (canUseBatchImage.value) {
    list.splice(2, 0, {
      path: '/batch-image',
      icon: 'sparkles',
      title: t('dashboard.batchImageAgent'),
      desc: t('dashboard.batchImageAgentDesc'),
    })
  }

  return list
})

onMounted(() => {
  void refreshBatchImageAccess()
})
</script>
