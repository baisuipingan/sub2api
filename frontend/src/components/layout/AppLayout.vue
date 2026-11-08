<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <!-- 左偏移必须与 AppSidebar 实际宽度一致（展开 220px / 收起 58px），
         用 ml-64(256px) 会多出 36px 空白缝隙 -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[58px]' : 'lg:ml-[220px]']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import { useTicketStore } from '@/stores/tickets'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const authStore = useAuthStore()
const route = useRoute()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: route.meta.workspaceArea !== 'workbench'
})

const onboardingStore = useOnboardingStore()
const ticketStore = useTicketStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
  // 侧栏工单红点：进入应用后开始轮询，管理员额外拉一次管理端未读
  ticketStore.startPolling(isAdmin.value)
})

onBeforeUnmount(() => {
  ticketStore.stopPolling()
})

defineExpose({ replayTour })
</script>
