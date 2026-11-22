<template>
  <section id="office-storage" class="card scroll-mt-24 p-5 sm:p-6" :aria-busy="loading">
    <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t(section + 'title') }}</h2>
        <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t(section + 'hint') }}</p>
      </div>
      <button type="button" class="btn btn-ghost btn-sm" :disabled="busy" @click="load">{{ t('common.refresh') }}</button>
    </div>

    <p v-if="error" class="mb-4 break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <fieldset class="min-w-0 space-y-4" :disabled="!settings || busy">
      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label for="office-storage-source" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-dark-200">{{ t(section + 'source') }}</label>
          <select id="office-storage-source" v-model="form.source" class="input">
            <option value="backup">{{ t(section + 'backup') }}</option>
            <option value="images">{{ t(section + 'images') }}</option>
            <option value="custom">{{ t(section + 'custom') }}</option>
          </select>
        </div>
        <Input
          id="office-storage-bucket"
          v-model="form.bucket"
          :label="t(section + 'bucket')"
          :placeholder="form.source === 'images' ? t(section + 'inheritBucket') : 'milo-office'"
        />
        <template v-if="form.source === 'custom'">
          <Input id="office-storage-endpoint" v-model="form.endpoint" :label="t('admin.backup.s3.endpoint')" placeholder="https://<account_id>.r2.cloudflarestorage.com" />
          <Input id="office-storage-region" v-model="form.region" :label="t('admin.backup.s3.region')" placeholder="auto" />
          <Input id="office-storage-access-key" v-model="form.access_key_id" :label="t('admin.backup.s3.accessKeyId')" autocomplete="off" />
          <Input id="office-storage-secret" v-model="form.secret_access_key" :label="t('admin.backup.s3.secretAccessKey')" :placeholder="settings?.secret_configured ? t('admin.backup.s3.secretConfigured') : ''" type="password" autocomplete="new-password" />
          <div class="flex items-center gap-3">
            <Toggle id="office-storage-path-style" v-model="form.force_path_style" :aria-label="t('admin.backup.s3.forcePathStyle')" />
            <label for="office-storage-path-style" class="text-sm text-gray-700 dark:text-dark-200">{{ t('admin.backup.s3.forcePathStyle') }}</label>
          </div>
        </template>
      </div>
      <div class="space-y-1 text-xs leading-5 text-gray-500 dark:text-dark-400" aria-live="polite">
        <p v-if="settings?.configured">{{ t(section + 'effectiveBucket', { bucket: settings.effective_bucket }) }}</p>
        <p v-if="settings?.target_in_use">{{ t(section + 'targetInUse') }}</p>
        <p>{{ t(section + 'cors') }}</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="button" data-testid="office-storage-test" class="btn btn-secondary btn-sm" @click="test">{{ testing ? t('common.loading') : t('admin.backup.s3.testConnection') }}</button>
        <button type="button" data-testid="office-storage-save" class="btn btn-primary btn-sm" @click="save">{{ saving ? t('common.loading') : t('common.save') }}</button>
      </div>
    </fieldset>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import type { OfficeStorageConfig, OfficeStorageConfigResponse } from '@/api/admin/backup'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Input from '@/components/common/Input.vue'
import Toggle from '@/components/common/Toggle.vue'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'
import { useAppStore } from '@/stores'

const section = 'admin.workbenchSettings.officeStorage.'
const { t } = useI18n()
const app = useAppStore()
const stepUp = useStepUp()
const settings = ref<OfficeStorageConfigResponse>()
const form = ref<OfficeStorageConfig>({
  source: 'backup', bucket: '', endpoint: '', region: 'auto', access_key_id: '', secret_access_key: '', force_path_style: true,
})
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const error = ref('')
const busy = computed(() => loading.value || saving.value || testing.value)
let disposed = false

function apply(response: OfficeStorageConfigResponse) {
  settings.value = response
  form.value = { ...response.config, region: response.config.region || 'auto', secret_access_key: '' }
}

function errorMessage(cause: unknown) {
  if (isStepUpBlocked(cause)) {
    return t(stepUpBlockReason(cause) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  }
  const reason = (cause as { reason?: string })?.reason
  if (reason === 'OFFICE_STORAGE_TARGET_IN_USE') {
    return t(section + 'targetInUse')
  }
  if (reason === 'OFFICE_STORAGE_PROBE_FAILED') return t(section + 'probeFailed')
  return (cause as { message?: string })?.message || t('errors.networkError')
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await adminAPI.backup.getOfficeStorageConfig()
    if (!disposed) apply(response)
  } catch (cause) {
    if (disposed) return
    settings.value = undefined
    error.value = errorMessage(cause)
  } finally {
    if (!disposed) loading.value = false
  }
}

function draft(): OfficeStorageConfig {
  return form.value.source === 'custom' ? { ...form.value } : {
    source: form.value.source, bucket: form.value.bucket, endpoint: '', region: '', access_key_id: '', force_path_style: false,
  }
}

async function test() {
  if (!settings.value || busy.value) return
  testing.value = true
  error.value = ''
  try {
    const result = await adminAPI.backup.testOfficeStorageConnection(draft())
    if (disposed) return
    if (result.ok) app.showSuccess(t(section + 'tested'))
    else error.value = result.message || t('admin.backup.s3.testFailed')
  } catch (cause) {
    if (!disposed) error.value = errorMessage(cause)
  } finally {
    if (!disposed) testing.value = false
  }
}

async function save() {
  if (!settings.value || busy.value) return
  saving.value = true
  error.value = ''
  const payload = draft()
  try {
    const result = await stepUp.run(() => adminAPI.backup.updateOfficeStorageConfig(payload))
    if (disposed) return
    apply(result)
    app.showSuccess(t(section + 'saved'))
  } catch (cause) {
    if (!disposed && !isStepUpCancelled(cause)) error.value = errorMessage(cause)
  } finally {
    if (!disposed) saving.value = false
  }
}

watch(() => form.value.source, () => { form.value.secret_access_key = ''; error.value = '' })
onMounted(load)
onBeforeUnmount(() => { disposed = true; form.value.secret_access_key = '' })
</script>
