<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-5 sm:p-6" data-testid="workbench-access-settings" :aria-busy="loading">
        <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.accessTitle') }}</h2>
            <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.accessHint') }}</p>
          </div>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="loading || saving" @click="loadConfig">{{ t('common.refresh') }}</button>
        </div>
        <p v-if="loadError" class="mb-4 break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ loadError }}</p>
        <fieldset class="min-w-0 space-y-5" :disabled="loading || saving || !loaded">
          <div class="flex items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div>
              <label for="workbench-entry-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.entryEnabled') }}</label>
              <p id="workbench-entry-description" class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.entryHint') }}</p>
            </div>
            <Toggle id="workbench-entry-enabled" v-model="form.image_studio_enabled" :aria-label="t('admin.workbenchSettings.entryEnabled')" aria-describedby="workbench-entry-description" />
          </div>
          <div class="max-w-xl">
            <label for="workbench-group" class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('admin.workbenchSettings.group') }}</label>
            <Select id="workbench-group" :model-value="form.image_studio_group_id" :options="groupOptions" :searchable="groupOptions.length > 6" :aria-label="t('admin.workbenchSettings.group')" :placeholder="t('admin.workbenchSettings.groupPlaceholder')" @update:model-value="form.image_studio_group_id = Number($event) || 0" />
            <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.groupHint') }}</p>
            <p v-if="loaded && !selectedGroupAvailable" class="mt-2 text-xs leading-5 text-amber-700 dark:text-amber-300" role="note">{{ t('admin.workbenchSettings.groupRequired') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" data-testid="workbench-access-save" class="btn btn-primary btn-sm" :disabled="form.image_studio_enabled && !selectedGroupAvailable" @click="saveConfig">{{ saving ? t('common.loading') : t('common.save') }}</button>
            <router-link to="/admin/groups" class="text-sm text-primary-600 hover:text-primary-700 dark:text-primary-400">{{ t('admin.workbenchSettings.manageGroups') }}</router-link>
          </div>
        </fieldset>
      </section>

      <ImageStorageSettings />

      <section class="rounded-xl border border-gray-200 p-5 dark:border-dark-700">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.routingTitle') }}</h2>
        <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.routingHint') }}</p>
        <div class="mt-3 flex flex-wrap gap-3">
          <router-link to="/admin/accounts" class="btn btn-secondary btn-sm">{{ t('admin.workbenchSettings.manageAccounts') }}</router-link>
          <router-link to="/admin/channels" class="btn btn-secondary btn-sm">{{ t('admin.workbenchSettings.manageChannels') }}</router-link>
        </div>
      </section>
      <TotpStepUpDialog :controller="stepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { getWorkbenchConfig, updateWorkbenchConfig } from '@/api/admin/workbench'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import ImageStorageSettings from '@/components/admin/workbench/ImageStorageSettings.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const form = ref({ image_studio_enabled: true, image_studio_group_id: 0 })
const groups = ref<AdminGroup[]>([])
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
let disposed = false

const eligibleGroups = computed(() => groups.value.filter(group => group.status === 'active' && group.allow_image_generation))
const selectedGroupAvailable = computed(() => eligibleGroups.value.some(group => group.id === form.value.image_studio_group_id))
const groupOptions = computed(() => [
  { value: 0, label: t('admin.workbenchSettings.groupPlaceholder') },
  ...(form.value.image_studio_group_id > 0 && !selectedGroupAvailable.value
    ? [{ value: form.value.image_studio_group_id, label: t('admin.workbenchSettings.groupUnavailable', { id: form.value.image_studio_group_id }), disabled: true }]
    : []),
  ...eligibleGroups.value.map(group => ({ value: group.id, label: group.name })),
])

function errorMessage(error: unknown) {
  if (isStepUpBlocked(error)) return t(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  return (error as { message?: string })?.message || t('errors.networkError')
}

async function loadConfig() {
  if (disposed) return
  loading.value = true
  loadError.value = ''
  try {
    const [config, availableGroups] = await Promise.all([getWorkbenchConfig(), adminAPI.groups.getAll()])
    if (disposed) return
    form.value = { image_studio_enabled: config.image_studio_enabled, image_studio_group_id: config.image_studio_group_id }
    groups.value = availableGroups
    loaded.value = true
  } catch (error) {
    if (!disposed) { loaded.value = false; loadError.value = errorMessage(error) }
  } finally { if (!disposed) loading.value = false }
}

async function saveConfig() {
  if (!loaded.value || loading.value || saving.value || (form.value.image_studio_enabled && !selectedGroupAvailable.value)) return
  saving.value = true
  const payload = { ...form.value }
  try {
    const saved = await stepUp.run(() => updateWorkbenchConfig(payload))
    if (disposed) return
    form.value = { image_studio_enabled: saved.image_studio_enabled, image_studio_group_id: saved.image_studio_group_id }
    appStore.showSuccess(t('admin.workbenchSettings.saved'))
    await appStore.fetchPublicSettings(true)
  } catch (error) {
    if (!disposed && !isStepUpCancelled(error)) appStore.showError(errorMessage(error))
  } finally { if (!disposed) saving.value = false }
}

onMounted(loadConfig)
onBeforeUnmount(() => { disposed = true })
</script>
