<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="handleClose">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div><p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p><p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p></div>
      </div>
      <div v-if="loading" class="flex justify-center py-8"><svg class="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg></div>
      <div v-else-if="loadFailed" class="space-y-3 py-4 text-center"><p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('desktop.keyListFailed') }}</p><button type="button" class="btn btn-secondary btn-sm" @click="load()">{{ t('common.tryAgain') }}</button></div>
      <div v-else-if="apiKeys.length === 0" class="py-8 text-center"><p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p></div>
      <div v-else ref="scrollContainerRef" class="max-h-96 space-y-3 overflow-y-auto" @scroll="closeGroupSelector">
        <div v-for="key in apiKeys" :key="key.id" data-test="admin-user-key" :data-key-id="key.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-start justify-between">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex items-center gap-2"><span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span><span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">{{ isDesktopAgentKey(key) && key.status === 'disabled' ? t('desktop.revokedState') : key.status }}</span></div>
              <p class="truncate font-mono text-sm text-gray-500">{{ key.key.substring(0, 20) }}...{{ key.key.substring(key.key.length - 8) }}</p>
            </div>
          </div>
          <div class="mt-3 flex flex-wrap gap-4 text-xs text-gray-500">
            <div class="flex items-center gap-1">
              <span>{{ t('admin.users.group') }}:</span>
              <button
                :ref="(el) => setGroupButtonRef(key.id, el)"
                data-test="admin-key-group"
                @click="openGroupSelector(key)"
                class="-mx-1 -my-0.5 flex cursor-pointer items-center gap-1 rounded-md px-1 py-0.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
                :disabled="key.is_system || updatingKeyIds.has(key.id) || credentialSubmitting"
                :title="key.is_system ? t('keys.systemKeyManaged') : undefined"
              >
                <GroupBadge
                  v-if="key.group_id && key.group"
                  :name="key.group.name"
                  :platform="key.group.platform"
                  :subscription-type="key.group.subscription_type"
                  :rate-multiplier="key.group.rate_multiplier"
                  :peak-rate-enabled="key.group.peak_rate_enabled"
                  :peak-start="key.group.peak_start"
                  :peak-end="key.group.peak_end"
                  :peak-rate-multiplier="key.group.peak_rate_multiplier"
                />
                <span v-else class="text-gray-400 italic">{{ t('admin.users.none') }}</span>
                <svg v-if="updatingKeyIds.has(key.id)" class="h-3 w-3 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>
                <svg v-else-if="!key.is_system" class="h-3 w-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9" /></svg>
              </button>
            </div>
            <div class="flex items-center gap-1"><span>{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</span></div>
          </div>
          <div v-if="isDesktopAgentKey(key)" class="mt-3 flex flex-wrap items-center gap-3 border-t border-gray-100 pt-3 dark:border-dark-700">
            <p class="min-w-0 flex-1 text-xs text-gray-500 dark:text-dark-400">{{ t('desktop.adminManagedHint') }}</p>
            <button v-if="key.status === 'active'" type="button" data-test="admin-agent-revoke" :disabled="credentialSubmitting" class="btn btn-secondary btn-sm text-amber-700 dark:text-amber-400" @click="openCredentialAction(key, 'revoke')">{{ t('desktop.revoke') }}</button>
            <button v-else-if="canRestoreDesktopKey(key)" type="button" data-test="admin-agent-restore" :disabled="credentialSubmitting" class="btn btn-secondary btn-sm" @click="openCredentialAction(key, 'restore')">{{ t('desktop.adminRestore') }}</button>
            <span v-else data-test="admin-agent-unavailable" class="text-xs text-gray-500 dark:text-dark-400">{{ t('desktop.adminRestoreUnavailable') }}</span>
          </div>
        </div>
      </div>
      <div v-if="!loading && !loadFailed && apiKeys.length" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-400">
        <span v-if="hasMoreKeys">{{ t('desktop.keysLoaded', { count: apiKeys.length, total: totalKeys }) }}</span>
        <span v-else data-test="admin-keys-all-loaded">{{ t('desktop.keysAllLoaded', { count: apiKeys.length }) }}</span>
        <button v-if="hasMoreKeys" type="button" data-test="admin-keys-load-more" :disabled="loadingMore || credentialSubmitting" class="btn btn-secondary btn-sm" @click="loadMore">{{ loadingMore ? t('common.loading') : t('desktop.keysLoadMore') }}</button>
      </div>
    </div>
  </BaseDialog>

  <!-- Group Selector Dropdown -->
  <Teleport to="body">
    <div
      v-if="groupSelectorKeyId !== null && dropdownPosition"
      ref="dropdownRef"
      class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-64 overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 dark:bg-dark-800 dark:ring-white/10"
      :style="{ top: dropdownPosition.top + 'px', left: dropdownPosition.left + 'px' }"
    >
      <div class="max-h-64 overflow-y-auto p-1.5">
        <!-- Unbind option -->
        <button
          @click="changeGroup(selectedKeyForGroup!, null)"
          :class="[
            'flex w-full items-center rounded-lg px-3 py-2 text-sm transition-colors',
            !selectedKeyForGroup?.group_id
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
        >
          <span class="text-gray-500 italic">{{ t('admin.users.none') }}</span>
          <svg
            v-if="!selectedKeyForGroup?.group_id"
            class="ml-auto h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
            fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"
          ><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
        </button>
        <!-- Group options -->
        <button
          v-for="group in allGroups"
          :key="group.id"
          @click="changeGroup(selectedKeyForGroup!, group.id)"
          :class="[
            'flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors',
            selectedKeyForGroup?.group_id === group.id
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
        >
          <GroupOptionItem
            :name="group.name"
            :platform="group.platform"
            :subscription-type="group.subscription_type"
            :rate-multiplier="group.rate_multiplier"
            :peak-rate-enabled="group.peak_rate_enabled"
            :peak-start="group.peak_start"
            :peak-end="group.peak_end"
            :peak-rate-multiplier="group.peak_rate_multiplier"
            :description="group.description"
            :selected="selectedKeyForGroup?.group_id === group.id"
          />
        </button>
      </div>
    </div>
  </Teleport>
  <ConfirmDialog :show="credentialAction !== null" :title="t(credentialAction?.action === 'restore' ? 'desktop.adminRestore' : 'desktop.revoke')" :message="t(credentialAction?.action === 'restore' ? 'desktop.adminRestoreConfirm' : 'desktop.adminRevokeConfirm', { email: credentialAction?.email || '' })" :danger="credentialAction?.action === 'revoke'" :loading="credentialSubmitting" @confirm="confirmCredentialAction" @cancel="cancelCredentialAction" />
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AdminUser, AdminGroup, ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { manageUserDesktopCredential } from '@/api/admin/desktopCredentials'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, StepUpCancelledError } from '@/composables/useStepUp'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close'])
const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const credentialAction = ref<{ userId: number; keyId: number; email: string; action: 'revoke' | 'restore' } | null>(null)
const credentialSubmitting = ref(false)
let credentialGeneration = 0

const apiKeys = ref<ApiKey[]>([])
const allGroups = ref<AdminGroup[]>([])
const loading = ref(false)
const loadFailed = ref(false)
const loadingMore = ref(false)
const totalKeys = ref(0)
const keysPage = ref(1)
const hasMoreKeys = computed(() => apiKeys.value.length < totalKeys.value)
let requestVersion = 0
const updatingKeyIds = ref(new Set<number>())
const groupSelectorKeyId = ref<number | null>(null)
const dropdownPosition = ref<{ top: number; left: number } | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const scrollContainerRef = ref<HTMLElement | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())

const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

watch(() => [props.show, props.user?.id] as const, ([show], _, onCleanup) => {
  onCleanup(() => { requestVersion++ })
  invalidateCredentialAction()
  closeGroupSelector()
  if (show && props.user) {
    load()
    loadGroups()
  }
})

const load = async (pageCount = 1) => {
  if (!props.user) return
  const userId = props.user.id
  const version = ++requestVersion
  apiKeys.value = []
  loading.value = true
  loadFailed.value = false
  loadingMore.value = false
  totalKeys.value = 0
  keysPage.value = 1
  groupButtonRefs.value.clear()
  try {
    const pages = await Promise.all(Array.from({ length: pageCount }, (_, page) => page === 0
      ? adminAPI.users.getUserApiKeys(userId)
      : adminAPI.users.getUserApiKeys(userId, { page: page + 1, page_size: 20 })))
    if (version !== requestVersion) return
    apiKeys.value = [...new Map(pages.flatMap(page => page.items || []).map(key => [key.id, key])).values()]
    totalKeys.value = pages[0].total ?? apiKeys.value.length
    keysPage.value = pageCount
  } catch (error) {
    if (version !== requestVersion) return
    loadFailed.value = true
    console.error('Failed to load API keys:', error)
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

const loadMore = async () => {
  if (!props.show || !props.user || loading.value || loadingMore.value || credentialSubmitting.value || !hasMoreKeys.value) return
  const version = requestVersion
  const userId = props.user.id
  const page = keysPage.value + 1
  loadingMore.value = true
  try {
    const result = await adminAPI.users.getUserApiKeys(userId, { page, page_size: 20 })
    if (version !== requestVersion || props.user?.id !== userId || !props.show) return
    apiKeys.value = [...new Map([...apiKeys.value, ...(result.items || [])].map(key => [key.id, key])).values()]
    keysPage.value = page
    totalKeys.value = result.items?.length ? (result.total ?? totalKeys.value) : apiKeys.value.length
  } catch {
    if (version === requestVersion) appStore.showError(t('desktop.keyListFailed'))
  } finally {
    if (version === requestVersion) loadingMore.value = false
  }
}

const loadGroups = async () => {
  try {
    const groups = await adminAPI.groups.getAll()
    allGroups.value = groups
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const DROPDOWN_HEIGHT = 272 // max-h-64 = 16rem = 256px + padding
const DROPDOWN_GAP = 4

const openGroupSelector = (key: ApiKey) => {
  if (key.is_system || credentialSubmitting.value) return
  if (groupSelectorKeyId.value === key.id) {
    closeGroupSelector()
  } else {
    const buttonEl = groupButtonRefs.value.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const spaceBelow = window.innerHeight - rect.bottom
      const openUpward = spaceBelow < DROPDOWN_HEIGHT && rect.top > spaceBelow
      dropdownPosition.value = {
        top: openUpward ? rect.top - DROPDOWN_HEIGHT - DROPDOWN_GAP : rect.bottom + DROPDOWN_GAP,
        left: rect.left
      }
    }
    groupSelectorKeyId.value = key.id
  }
}

const closeGroupSelector = () => {
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
}

const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
  if (key.is_system || credentialSubmitting.value) return
  closeGroupSelector()
  if (key.group_id === newGroupId || (!key.group_id && newGroupId === null)) return

  updatingKeyIds.value.add(key.id)
  try {
    const result = await adminAPI.apiKeys.updateApiKeyGroup(key.id, newGroupId)
    // Update local data
    const idx = apiKeys.value.findIndex((k) => k.id === key.id)
    if (idx !== -1) {
      apiKeys.value[idx] = result.api_key
    }
    if (result.auto_granted_group_access && result.granted_group_name) {
      appStore.showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
    } else {
      appStore.showSuccess(t('admin.users.groupChangedSuccess'))
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.groupChangeFailed'))
  } finally {
    updatingKeyIds.value.delete(key.id)
  }
}

const handleKeyDown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && groupSelectorKeyId.value !== null) {
    event.stopPropagation()
    closeGroupSelector()
  }
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (dropdownRef.value && !dropdownRef.value.contains(target)) {
    // Check if the click is on one of the group trigger buttons
    for (const el of groupButtonRefs.value.values()) {
      if (el.contains(target)) return
    }
    closeGroupSelector()
  }
}

const handleClose = () => {
  invalidateCredentialAction()
  closeGroupSelector()
  emit('close')
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeyDown, true)
})

onUnmounted(() => {
  requestVersion++
  invalidateCredentialAction()
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeyDown, true)
})

function isDesktopAgentKey(key: ApiKey) {
  return key.is_system === true && key.purpose === 'desktop_agent'
}
function canRestoreDesktopKey(key: ApiKey) {
  if (!isDesktopAgentKey(key) || key.status !== 'disabled') return false
  if (key.quota > 0 && key.quota_used >= key.quota) return false
  return !key.expires_at || new Date(key.expires_at).getTime() > Date.now()
}
function invalidateCredentialAction() {
  credentialGeneration++
  credentialAction.value = null
  credentialSubmitting.value = false
  stepUp.onCancel()
}
function cancelCredentialAction() {
  if (!credentialSubmitting.value) credentialAction.value = null
}
function openCredentialAction(key: ApiKey, action: 'revoke' | 'restore') {
  if (!props.show || !props.user || credentialSubmitting.value || !isDesktopAgentKey(key)) return
  if (action === 'revoke' ? key.status !== 'active' : !canRestoreDesktopKey(key)) return
  closeGroupSelector()
  credentialAction.value = { userId: props.user.id, keyId: key.id, email: props.user.email, action }
}
async function confirmCredentialAction() {
  if (!credentialAction.value || credentialSubmitting.value) return
  const captured = { ...credentialAction.value }
  const current = credentialGeneration
  const valid = () => current === credentialGeneration && props.show && props.user?.id === captured.userId
  const key = apiKeys.value.find(item => item.id === captured.keyId)
  if (!valid() || !key || !isDesktopAgentKey(key) || (captured.action === 'restore' ? !canRestoreDesktopKey(key) : key.status !== 'active')) {
    credentialAction.value = null
    return
  }
  credentialSubmitting.value = true
  try {
    await stepUp.run(async () => {
      // 原因：二次验证等待期间切换用户或关闭弹窗，不可重试先前目标的操作。
      if (!valid()) throw new StepUpCancelledError()
      try {
        return await manageUserDesktopCredential(captured.action, captured.userId, captured.keyId)
      } catch (error) {
        // 原因：失效请求迟到的403不能重新唤起已关闭或其他用户的验证弹窗。
        if (!valid()) throw new StepUpCancelledError()
        throw error
      }
    })
    if (!valid()) return
    credentialAction.value = null
    appStore.showSuccess(t(captured.action === 'restore' ? 'desktop.adminRestored' : 'desktop.adminRevoked'))
    await load(keysPage.value)
  } catch (error) {
    if (!valid() || isStepUpCancelled(error)) return
    if (isStepUpBlocked(error)) {
      appStore.showError(t(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled'))
    } else {
      appStore.showError(t('desktop.credentialFailed'))
    }
  } finally {
    if (valid()) credentialSubmitting.value = false
  }
}
</script>
