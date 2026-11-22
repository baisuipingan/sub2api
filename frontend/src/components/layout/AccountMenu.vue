<template>
        <div v-if="user" class="account-menu relative" :class="{ 'account-menu-sidebar': placement === 'sidebar', 'account-menu-collapsed': collapsed }" ref="dropdownRef">
          <button
            ref="triggerRef"
            type="button"
            @click="toggleDropdown"
            :aria-expanded="dropdownOpen"
            @keydown.down.prevent="openAndFocus"
            @keydown.esc="onTriggerEscape"
            class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
            :aria-label="t('common.userMenu')"
          >
            <div class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                :alt="displayName"
                class="h-full w-full object-cover"
              >
              <span v-else>{{ userInitials }}</span>
            </div>
            <div class="account-menu-identity hidden min-w-0 text-left md:block">
              <div class="truncate text-sm font-medium text-gray-900 dark:text-white">
                {{ displayName }}
              </div>
              <div class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.users.roles.' + user.role) }}
              </div>
            </div>
            <Icon :name="placement === 'sidebar' ? 'more' : 'chevronDown'" size="sm" class="account-menu-chevron hidden shrink-0 text-gray-400 md:block" />
          </button>

          <!-- Dropdown Menu -->
          <transition name="dropdown">
            <div v-if="dropdownOpen" ref="menuRef" class="dropdown account-menu-popover right-0 mt-2 w-56" :class="{ 'account-menu-popover-above': placement === 'sidebar' }" @keydown="onMenuKeydown">
              <!-- User Info -->
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="text-sm font-medium text-gray-900 dark:text-white">
                  {{ displayName }}
                </div>
                <div class="text-xs text-gray-500 dark:text-dark-400">{{ user.email }}</div>
              </div>

              <!-- Balance (mobile only) -->
              <div :class="['border-b border-gray-100 px-4 py-2 dark:border-dark-700', placement === 'header' && 'sm:hidden']">
                <div class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('common.balance') }}
                </div>
                <div class="text-sm font-semibold text-primary-600 dark:text-primary-400">
                  {{ formatHeaderMoney(availableBalance) }}
                </div>
                <div v-if="frozenBalance > 0" class="mt-1 text-xs text-amber-600 dark:text-amber-300">
                  {{ balanceFrozenText }} {{ formatHeaderMoney(frozenBalance) }}
                </div>
              </div>

              <div class="py-1">
                <slot name="links" :close="closeDropdown" />
                <router-link to="/profile" @click="closeDropdown" class="dropdown-item">
                  <Icon name="user" size="sm" />
                  {{ t('nav.profile') }}
                </router-link>

                <router-link to="/keys" @click="closeDropdown" class="dropdown-item">
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>

                <a
                  v-if="authStore.isAdmin"
                  href="https://github.com/Wei-Shaw/sub2api"
                  target="_blank"
                  rel="noopener noreferrer"
                  @click="closeDropdown"
                  class="dropdown-item"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      fill-rule="evenodd"
                      clip-rule="evenodd"
                      d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                    />
                  </svg>
                  {{ t('nav.github') }}
                </a>

              </div>

              <!-- Contact Support (only show if configured) -->
              <div
                v-if="contactInfo"
                class="border-t border-gray-100 px-4 py-2.5 dark:border-dark-700"
              >
                <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <svg
                    class="h-3.5 w-3.5 flex-shrink-0"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 01-.825-.242m9.345-8.334a2.126 2.126 0 00-.476-.095 48.64 48.64 0 00-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0011.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155"
                    />
                  </svg>
                  <span>{{ t('common.contactSupport') }}:</span>
                  <span class="font-medium text-gray-700 dark:text-gray-300">{{
                    contactInfo
                  }}</span>
                </div>
              </div>

              <div v-if="showOnboardingButton" class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button @click="handleReplayGuide" class="dropdown-item w-full">
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 14a1 1 0 110 2 1 1 0 010-2zm1.07-7.75c0-.6-.49-1.25-1.32-1.25-.7 0-1.22.4-1.43 1.02a1 1 0 11-1.9-.62A3.41 3.41 0 0111.8 5c2.02 0 3.25 1.4 3.25 2.9 0 2-1.83 2.55-2.43 3.12-.43.4-.47.75-.47 1.23a1 1 0 01-2 0c0-1 .16-1.82 1.1-2.7.69-.64 1.82-1.05 1.82-2.06z"
                    />
                  </svg>
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button
                  @click="handleLogout"
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75"
                    />
                  </svg>
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import Icon from '@/components/icons/Icon.vue'
import { formatCurrentBillingAmount } from '@/utils/billingCurrency'

withDefaults(defineProps<{ placement?: 'header' | 'sidebar'; collapsed?: boolean }>(), { placement: 'header', collapsed: false })
const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const user = computed(() => authStore.user)
const contactInfo = computed(() => appStore.contactInfo)
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const displayName = computed(() => user.value?.username || user.value?.email?.split('@')[0] || '')
const userInitials = computed(() => displayName.value.substring(0, 2).toUpperCase())
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const balanceFrozenText = computed(() => t('common.frozenBalance'))
const showOnboardingButton = computed(() => !authStore.isSimpleMode && user.value?.role === 'admin' && route.meta.workspaceArea !== 'workbench')
const dropdownRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const menuRef = ref<HTMLElement | null>(null)
const dropdownOpen = ref(false)
function formatHeaderMoney(value: number) { return formatCurrentBillingAmount(Number.isFinite(value) ? value : 0, 2) }
function closeDropdown() { dropdownOpen.value = false }
function closeAndRestore() { closeDropdown(); triggerRef.value?.focus() }
function onTriggerEscape(event: KeyboardEvent) { if (dropdownOpen.value) { event.preventDefault(); event.stopPropagation(); closeAndRestore() } }
function toggleDropdown() { dropdownOpen.value = !dropdownOpen.value }
async function openAndFocus() { dropdownOpen.value = true; await nextTick(); menuItems()[0]?.focus() }
function menuItems() { return [...(menuRef.value?.querySelectorAll<HTMLElement>('a[href], button:not([disabled])') ?? [])] }
function onMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeAndRestore(); return }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const items = menuItems(), current = items.indexOf(document.activeElement as HTMLElement)
  const target = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (current + (event.key === 'ArrowUp' ? -1 : 1) + items.length) % items.length
  items[target]?.focus()
}
async function handleLogout() {
  closeDropdown()
  try { await authStore.logout() } catch (error) { console.error('Logout error:', error) }
  await router.push('/login')
}
function handleReplayGuide() { closeDropdown(); onboardingStore.replay() }
function handleClickOutside(event: MouseEvent) { if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) closeDropdown() }
function handleFocusOutside(event: FocusEvent) { if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) closeDropdown() }
watch(() => [route.fullPath, user.value?.id], closeDropdown)
onMounted(() => { document.addEventListener('click', handleClickOutside); document.addEventListener('focusin', handleFocusOutside) })
onBeforeUnmount(() => { document.removeEventListener('click', handleClickOutside); document.removeEventListener('focusin', handleFocusOutside) })
</script>

<style scoped>
.account-menu-sidebar > button { width:100%; padding:4px; gap:8px; border-radius:8px; }
.account-menu-sidebar .account-menu-identity { display:block; flex:1; }
.account-menu-sidebar .account-menu-chevron { display:block; }
.account-menu-collapsed > button { justify-content:center; padding:2px; }
.account-menu-collapsed .account-menu-identity,.account-menu-collapsed .account-menu-chevron { display:none; }
.account-menu-popover { z-index:70; max-width:calc(100vw - 24px); max-height:calc(100dvh - 32px); overflow-y:auto; }
.account-menu-popover-above { inset:auto auto calc(100% + 8px) 0; margin:0; }
.dropdown-enter-active,.dropdown-leave-active { transition:opacity .15s ease,transform .15s ease; }
.dropdown-enter-from,.dropdown-leave-to { opacity:0; transform:translateY(-4px); }
@media (prefers-reduced-motion:reduce) { .dropdown-enter-active,.dropdown-leave-active { transition:none; } }
</style>
