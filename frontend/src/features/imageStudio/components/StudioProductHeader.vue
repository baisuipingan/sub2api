<template>
  <!-- 嵌入到 sub2api 后不再需要站点 Logo 与大标题（外层已有）；
       这里只保留连接状态与设置入口，避免出现双重顶栏。

       注意：不要加 sticky —— 外层 AppHeader 已经是 sticky top-0，
       两个吸顶条会抢占同一个位置，滚动时压在正文上。（曾出现
       「创作」标题被遮掉一半的问题） -->
  <header
    class="border-b border-gray-200/80 bg-white dark:border-white/[0.08] dark:bg-dark-950"
  >
    <div
      class="mx-auto flex min-h-10 w-full max-w-[1600px] items-center justify-end gap-2 px-5 py-1"
    >
      <!-- 标题已由外层 AppHeader 提供，这里只保留连接状态与设置入口，
           避免出现两行重复的「生图工作台」 -->
      <div class="ml-auto flex items-center gap-3">
        <button
          type="button"
          class="flex items-center gap-2 whitespace-nowrap rounded-lg px-2 py-1.5 text-xs transition hover:bg-gray-50 dark:hover:bg-white/[0.06]"
          :class="
            connected
              ? 'text-emerald-600 dark:text-emerald-400'
              : 'text-gray-600 dark:text-dark-300'
          "
          @click="state.openConnectionDialog('manage')"
        >
          <span
            class="h-2 w-2 rounded-full"
            :class="connected ? 'bg-emerald-500' : 'bg-gray-400'"
          />
          {{ statusLabel }}
        </button>

        <button
          type="button"
          class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 transition hover:bg-gray-50 dark:border-white/[0.1] dark:text-dark-200 dark:hover:bg-white/[0.06]"
          @click="state.setShowSettings(true)"
        >
          <StudioIcon name="SettingsIcon" class="h-4 w-4" />
          {{ t('imageStudio.header.settings') }}
        </button>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import { storeState as state } from '../stores/store'
import { formatMiloRouterBalance } from '../lib/miloRouterDisplay'
import type { MiloRouterConnectionStatus } from '../types'

const { t } = useI18n()

const connected = computed(() => state.settings.miloRouter.connectionStatus === 'connected')

const statusLabel = computed(() => {
  const status = state.settings.miloRouter.connectionStatus as MiloRouterConnectionStatus
  if (status === 'checking') return t('imageStudio.header.connecting')
  if (status === 'error') return t('imageStudio.header.error')
  if (status !== 'connected') return t('imageStudio.header.disconnected')
  const balance = formatMiloRouterBalance(state.settings.miloRouter.billing?.balance)
  return balance !== undefined
    ? t('imageStudio.header.connectedWithBalance', { balance })
    : t('imageStudio.header.connected')
})
</script>
