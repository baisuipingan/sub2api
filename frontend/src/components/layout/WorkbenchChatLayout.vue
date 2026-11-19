<template>
  <div class="workbench-chat-layout" :class="{ 'chat-sidebar-collapsed': collapsed, 'chat-mobile-open': mobileOpen }">
    <button v-if="mobileOpen && !desktop" class="chat-nav-backdrop" tabindex="-1" aria-label="关闭导航" @click="closeMobile()" />

    <aside
      :id="sidebarId"
      ref="sidebarRef"
      class="chat-navigation"
      :inert="!desktop && !mobileOpen"
      :aria-hidden="!desktop && !mobileOpen ? true : undefined"
      :role="!desktop && mobileOpen ? 'dialog' : undefined"
      :aria-modal="!desktop && mobileOpen ? true : undefined"
      aria-label="工作台导航"
    >
      <div class="chat-brand-row">
        <RouterLink to="/workbench/chat" class="chat-brand" :title="appStore.siteName" @click="closeMobile(false)">
          <img v-if="appStore.publicSettingsLoaded" :src="siteLogo || '/logo.svg'" alt="" width="24" height="24" />
          <span v-if="!collapsed">{{ appStore.siteName }}</span>
          <span v-else class="sr-only">{{ appStore.siteName }}</span>
        </RouterLink>
        <button v-if="!desktop" ref="closeButtonRef" type="button" class="chat-nav-icon" aria-label="关闭导航" @click="closeMobile()"><Icon name="x" size="md" /></button>
        <button v-else-if="!collapsed" type="button" class="chat-nav-icon" aria-label="收起侧栏" @click="appStore.toggleSidebar()"><Icon name="chevronLeft" size="sm" /></button>
      </div>

      <UserAreaSwitcher
        v-if="authStore.isAdmin || !appStore.backendModeEnabled"
        class="chat-area-switcher"
        area="workbench"
        :collapsed="collapsed"
        :console-to="consoleDestination"
        workbench-to="/workbench/chat"
        @navigate="closeMobile(false)"
      />

      <nav class="chat-module-nav" aria-label="工作台功能">
        <button type="button" class="chat-nav-link chat-new-conversation" :title="collapsed ? '新建对话' : undefined" @click="newConversation">
          <Icon name="edit" size="sm" /><span v-if="!collapsed">新建对话</span><span v-else class="sr-only">新建对话</span>
        </button>
        <RouterLink to="/workbench/image" class="chat-nav-link" :title="collapsed ? t('nav.imageStudio') : undefined" @click="closeMobile(false)">
          <Icon name="sparkles" size="sm" /><span v-if="!collapsed">{{ t('nav.imageStudio') }}</span><span v-else class="sr-only">{{ t('nav.imageStudio') }}</span>
        </RouterLink>
        <RouterLink v-if="chatStore.status?.files_enabled" to="/workbench/files" class="chat-nav-link" :title="collapsed ? '我的文件' : undefined" @click="closeMobile(false)">
          <Icon name="inbox" size="sm" /><span v-if="!collapsed">我的文件</span><span v-else class="sr-only">我的文件</span>
        </RouterLink>
      </nav>

      <ConversationList v-if="!collapsed" :show-new-button="false" />
      <div v-else class="chat-history-spacer" />

      <footer class="chat-sidebar-footer">
        <RouterLink v-if="authStore.isAdmin" to="/admin/workbench" class="chat-nav-link chat-admin-entry" :title="collapsed ? t('nav.workbenchManagement') : undefined" @click="closeMobile(false)">
          <Icon name="cog" size="sm" /><span v-if="!collapsed">{{ t('nav.workbenchManagement') }}</span><span v-else class="sr-only">{{ t('nav.workbenchManagement') }}</span>
        </RouterLink>
        <div v-if="!collapsed" class="chat-billing-row">
          <span :title="t('common.availableBalance')">{{ formatCurrentBillingAmount(Number(authStore.user?.balance || 0), 2) }}</span>
          <SubscriptionProgressMini v-if="subscriptionEnabled" placement="top" />
        </div>
        <div class="chat-global-actions">
          <AnnouncementBell v-if="authStore.user" />
          <LocaleSwitcher placement="top" :compact="collapsed" />
          <button type="button" class="chat-nav-icon" :title="isDark ? '切换浅色模式' : '切换深色模式'" :aria-label="isDark ? '切换浅色模式' : '切换深色模式'" @click="toggleTheme"><Icon :name="isDark ? 'sun' : 'moon'" size="sm" /></button>
          <button v-if="desktop && collapsed" type="button" class="chat-nav-icon" aria-label="展开侧栏" @click="appStore.toggleSidebar()"><Icon name="chevronRight" size="sm" /></button>
        </div>
        <AccountMenu placement="sidebar" :collapsed="collapsed">
          <template #links="{ close }">
            <RouterLink v-if="authStore.isAdmin || !appStore.backendModeEnabled" :to="desktopDownloadPath('workbench', 'personal')" class="dropdown-item" @click="close"><Icon name="download" size="sm" />MiloCode</RouterLink>
            <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="dropdown-item" @click="close"><Icon name="book" size="sm" />{{ t('nav.docs') }}</a>
            <RouterLink v-if="modelPlazaEnabled" :to="{ path: '/model-plaza', query: { embedded: '1' } }" class="dropdown-item" @click="close"><Icon name="grid" size="sm" />{{ t('nav.modelPlaza') }}</RouterLink>
            <RouterLink to="/tickets" class="dropdown-item" @click="close"><Icon name="chatBubble" size="sm" />{{ t('nav.tickets') }}<span v-if="ticketStore.userUnreadCount" class="chat-ticket-unread">{{ ticketStore.userUnreadCount }}</span></RouterLink>
            <RouterLink v-if="subscriptionEnabled && collapsed" to="/subscriptions" class="dropdown-item" @click="close"><Icon name="creditCard" size="sm" />{{ t('nav.mySubscriptions') }}</RouterLink>
          </template>
        </AccountMenu>
      </footer>
    </aside>

    <div class="chat-main-column">
      <header class="chat-mobile-header" :inert="mobileOpen && !desktop">
        <button ref="mobileTriggerRef" type="button" class="chat-nav-icon" aria-label="打开工作台导航" :aria-expanded="mobileOpen" :aria-controls="sidebarId" @click="openMobile"><Icon name="menu" size="md" /></button>
        <span>对话助手</span>
        <button type="button" class="chat-nav-icon" aria-label="新建对话" @click="newConversation"><Icon name="edit" size="sm" /></button>
      </header>
      <main ref="mainRef" class="chat-layout-main" :inert="mobileOpen && !desktop" tabindex="-1"><slot /></main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { useWorkbenchChatStore } from '@/stores/workbenchChat'
import { useTicketStore } from '@/stores/tickets'
import { useWorkspaceNavigationStore } from '@/stores/workspaceNavigation'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { formatCurrentBillingAmount } from '@/utils/billingCurrency'
import { sanitizeUrl } from '@/utils/url'
import { desktopDownloadPath } from '@/router/desktopDownloadRoutes'
import AccountMenu from './AccountMenu.vue'
import UserAreaSwitcher from './UserAreaSwitcher.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import ConversationList from '@/components/workbench/chat/ConversationList.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const route = useRoute(), router = useRouter()
const appStore = useAppStore(), authStore = useAuthStore(), chatStore = useWorkbenchChatStore()
const ticketStore = useTicketStore(), workspaceNavigation = useWorkspaceNavigationStore()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
const sidebarId = `chat-navigation-${useId()}`
const desktopQuery = window.matchMedia('(min-width: 1024px)')
const desktop = ref(desktopQuery.matches), mobileOpen = ref(false)
const collapsed = computed(() => desktop.value && appStore.sidebarCollapsed)
const sidebarRef = ref<HTMLElement | null>(null), mainRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null), mobileTriggerRef = ref<HTMLButtonElement | null>(null)
const isDark = ref(document.documentElement.classList.contains('dark'))
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const subscriptionEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
let previousOverflow: string | null = null

const consoleDestination = computed(() => {
  const visiblePaths = ['/keys', '/tickets', '/profile', '/downloads']
  if (!authStore.isAdmin) visiblePaths.push('/dashboard')
  if (!authStore.isSimpleMode) {
    visiblePaths.push('/usage', '/redeem')
    if (canUseBatchImage.value) visiblePaths.push('/batch-image')
  }
  if (isFeatureFlagEnabled(FeatureFlags.channelMonitor)) visiblePaths.push('/monitor')
  if (isFeatureFlagEnabled(FeatureFlags.mcp)) visiblePaths.push('/settings/mcp')
  if (!authStore.isSimpleMode) {
    if (isFeatureFlagEnabled(FeatureFlags.availableChannels)) visiblePaths.push('/available-channels')
    if (subscriptionEnabled.value) visiblePaths.push('/subscriptions')
    if (isFeatureFlagEnabled(FeatureFlags.payment)) visiblePaths.push('/purchase', '/orders')
    if (isFeatureFlagEnabled(FeatureFlags.affiliate)) visiblePaths.push('/affiliate')
  }
  visiblePaths.push(...(appStore.cachedPublicSettings?.custom_menu_items ?? []).filter(item => item.visibility === 'user').map(item => `/custom/${item.id}`))
  return workspaceNavigation.destination(router, 'console', 'personal', { isAdmin: authStore.isAdmin, backendModeEnabled: appStore.backendModeEnabled, isSimpleMode: authStore.isSimpleMode, visiblePaths })
})

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
async function newConversation() { await chatStore.newConversation(); closeMobile(false); await router.push('/workbench/chat'); mainRef.value?.focus() }
async function openMobile() { mobileOpen.value = true; await nextTick(); closeButtonRef.value?.focus() }
function closeMobile(restoreFocus = true) { if (!mobileOpen.value) return; mobileOpen.value = false; if (restoreFocus) void nextTick(() => mobileTriggerRef.value?.focus()) }
function viewportChanged(event: MediaQueryListEvent) { desktop.value = event.matches; if (event.matches) closeMobile(false) }
function handleNavigationKey(event: KeyboardEvent) {
  if (!mobileOpen.value || desktop.value || event.defaultPrevented) return
  // Teleported dialogs and select menus keep their own keyboard/focus handling.
  const active = document.activeElement
  if (active?.closest('[role="dialog"], .select-dropdown-portal') && !sidebarRef.value?.contains(active)) return
  if (event.key === 'Escape') { event.preventDefault(); closeMobile(); return }
  if (event.key !== 'Tab') return
  const items = [...(sidebarRef.value?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), [tabindex="0"]') ?? [])].filter(el => getComputedStyle(el).display !== 'none' && getComputedStyle(el).visibility !== 'hidden')
  const first = items[0], last = items[items.length - 1]
  if (event.shiftKey && (active === first || !sidebarRef.value?.contains(active))) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && (active === last || !sidebarRef.value?.contains(active))) { event.preventDefault(); first?.focus() }
}
watch(mobileOpen, open => {
  if (open) { previousOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden' }
  else if (previousOverflow !== null) { document.body.style.overflow = previousOverflow; previousOverflow = null }
})
watch(() => route.fullPath, () => closeMobile(false))
watch(() => authStore.user?.id ?? null, userId => workspaceNavigation.syncUser(userId), { immediate: true, flush: 'sync' })
watch(() => route.fullPath, () => workspaceNavigation.remember(route), { immediate: true })
onMounted(() => {
  const savedTheme = localStorage.getItem('theme')
  isDark.value = savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', isDark.value)
  desktopQuery.addEventListener('change', viewportChanged)
  document.addEventListener('keydown', handleNavigationKey)
  ticketStore.startPolling(authStore.isAdmin)
  void refreshBatchImageAccess()
})
onBeforeUnmount(() => {
  desktopQuery.removeEventListener('change', viewportChanged)
  document.removeEventListener('keydown', handleNavigationKey)
  if (previousOverflow !== null) document.body.style.overflow = previousOverflow
  ticketStore.stopPolling()
})
</script>

<style scoped>
.workbench-chat-layout { --workbench-chat-header-height:0px; --chat-nav-width:240px; display:flex; width:100%; height:100dvh; min-height:0; overflow:hidden; color:#1e1e2e; background:#fff; }
.chat-sidebar-collapsed { --chat-nav-width:58px; }
.chat-navigation { position:relative; z-index:40; display:flex; flex:0 0 var(--chat-nav-width); width:var(--chat-nav-width); min-height:0; height:100dvh; flex-direction:column; gap:12px; border-right:.5px solid #e8e9eb; padding:20px 12px 12px; background:#fff; }
.chat-brand-row { display:flex; height:32px; flex-shrink:0; align-items:center; justify-content:space-between; gap:8px; }
.chat-brand { display:flex; min-width:0; align-items:center; gap:8px; font-size:16px; font-weight:600; color:inherit; text-decoration:none; }
.chat-brand img { width:24px; height:24px; flex-shrink:0; object-fit:contain; }
.chat-brand span { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.chat-nav-icon { display:inline-flex; width:32px; height:32px; flex-shrink:0; align-items:center; justify-content:center; border:0; border-radius:8px; color:inherit; background:transparent; cursor:pointer; }
.chat-nav-icon:hover,.chat-nav-link:hover { background:rgb(128 128 128 / 8%); }
.chat-nav-icon:focus-visible,.chat-nav-link:focus-visible,.chat-brand:focus-visible { outline:2px solid #2b7fff; outline-offset:2px; }
.chat-area-switcher { flex-shrink:0; padding:0; }
.chat-module-nav { display:flex; flex-shrink:0; flex-direction:column; gap:4px; }
.chat-nav-link { display:flex; min-width:0; min-height:32px; align-items:center; gap:8px; border:0; border-radius:8px; padding:4px 8px; color:inherit; font-size:14px; line-height:24px; text-align:left; text-decoration:none; background:transparent; cursor:pointer; }
.chat-new-conversation { background:#f5f5f5; }
.chat-navigation :deep(.bs-chat-history-heading) { min-height:28px; padding:0 8px; }
.chat-history-spacer { flex:1; min-height:0; }
.chat-sidebar-footer { display:flex; flex-shrink:0; flex-direction:column; gap:8px; border-top:.5px solid #e8e9eb; padding-top:8px; }
.chat-global-actions { display:flex; align-items:center; justify-content:space-between; gap:2px; }
.chat-billing-row { display:flex; min-height:28px; align-items:center; justify-content:space-between; gap:8px; padding:0 4px; color:#6b7280; font-size:12px; }
.chat-ticket-unread { margin-left:auto; color:#ef4444; font-size:12px; }
.chat-main-column { display:flex; flex:1; min-width:0; min-height:0; height:100dvh; flex-direction:column; }
.chat-layout-main { flex:1; min-width:0; min-height:0; overflow:hidden; outline:none; }
.chat-mobile-header { display:none; }
.chat-sidebar-collapsed .chat-navigation { padding-left:8px; padding-right:8px; gap:16px; }
.chat-sidebar-collapsed .chat-brand-row,.chat-sidebar-collapsed .chat-brand { justify-content:center; }
.chat-sidebar-collapsed .chat-module-nav .chat-nav-link,.chat-sidebar-collapsed .chat-admin-entry { justify-content:center; padding:4px; }
.chat-sidebar-collapsed .chat-global-actions { flex-direction:column; gap:4px; }
.chat-nav-backdrop { position:fixed; inset:0; z-index:39; border:0; background:rgb(0 0 0 / 35%); }
:global(.dark) .workbench-chat-layout { color:#e5e7eb; background:#171719; }
:global(.dark) .chat-navigation { background:#1e1e20; border-color:#333336; }
:global(.dark) .chat-sidebar-footer { border-color:#333336; }
:global(.dark) .chat-new-conversation { background:#2c2c2e; }
@media (max-width:1023px) {
  .workbench-chat-layout { --workbench-chat-header-height:48px; }
  .chat-navigation { position:fixed; inset:0 auto 0 0; width:240px; transform:translateX(-100%); visibility:hidden; }
  .chat-mobile-open .chat-navigation { transform:translateX(0); visibility:visible; }
  .chat-mobile-header { display:flex; flex:0 0 48px; align-items:center; justify-content:space-between; gap:8px; border-bottom:.5px solid #e8e9eb; padding:0 8px; font-size:14px; font-weight:500; }
  :global(.dark) .chat-mobile-header { border-color:#333336; }
}
</style>
