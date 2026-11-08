<template>
  <section id="image-storage" class="card scroll-mt-24 p-5 sm:p-6" data-testid="image-storage-settings" :aria-busy="loading">
    <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.backup.imageStorage.title') }}</h2>
        <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.sharedStorageHint') }}</p>
      </div>
      <button type="button" class="btn btn-ghost btn-sm" :disabled="loading || saving || testing" @click="loadConfig">{{ t('common.refresh') }}</button>
    </div>

    <p v-if="loadError" class="mb-4 break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ loadError }}</p>
    <fieldset class="min-w-0 space-y-5" :disabled="!loaded || loading || saving || testing">
      <div class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex items-start justify-between gap-4">
          <div>
            <label for="async-image-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.backup.imageStorage.enabled') }}</label>
            <p id="async-image-description" class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.asyncHint') }}</p>
          </div>
          <Toggle id="async-image-enabled" v-model="form.enabled" aria-describedby="async-image-description" :aria-label="t('admin.backup.imageStorage.enabled')" />
        </div>
        <div class="flex items-start justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700">
          <div>
            <label for="studio-gallery-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.backup.imageStorage.studioGalleryEnabled') }}</label>
            <p id="studio-gallery-description" class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.backup.imageStorage.studioGalleryDescription') }}</p>
          </div>
          <Toggle id="studio-gallery-enabled" :model-value="form.studio_gallery_enabled" aria-describedby="studio-gallery-description studio-gallery-private-warning" :aria-label="t('admin.backup.imageStorage.studioGalleryEnabled')" @update:model-value="setGalleryEnabled" />
        </div>
        <div class="flex items-start justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700">
          <div>
            <label for="studio-jobs-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.backup.imageStorage.studioJobsEnabled') }}</label>
            <p id="studio-jobs-description" class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.backup.imageStorage.studioJobsDescription') }}</p>
          </div>
          <Toggle id="studio-jobs-enabled" v-model="form.studio_jobs_enabled" :disabled="!form.studio_gallery_enabled" aria-describedby="studio-jobs-description" :aria-label="t('admin.backup.imageStorage.studioJobsEnabled')" />
        </div>
        <p id="studio-gallery-private-warning" role="note" class="text-xs leading-5 text-amber-700 dark:text-amber-300">{{ t('admin.backup.imageStorage.studioGalleryPrivateBucketWarning') }}</p>
      </div>

      <div class="flex items-center justify-between gap-4">
        <label for="image-storage-reuse-backup" class="text-sm text-gray-700 dark:text-dark-200">{{ t('admin.backup.imageStorage.reuseBackupS3') }}</label>
        <Toggle id="image-storage-reuse-backup" v-model="form.reuse_backup_s3" :aria-label="t('admin.backup.imageStorage.reuseBackupS3')" />
      </div>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Input id="image-storage-bucket" v-model="form.bucket" :label="t('admin.backup.imageStorage.bucket')" :placeholder="form.reuse_backup_s3 ? t('admin.backup.imageStorage.bucketInherited') : ''" />
        <Input id="image-storage-prefix" v-model="form.prefix" :label="t('admin.backup.imageStorage.prefix')" placeholder="images/" />
        <template v-if="!form.reuse_backup_s3">
          <Input id="image-storage-endpoint" v-model="form.endpoint" :label="t('admin.backup.s3.endpoint')" placeholder="https://<account_id>.r2.cloudflarestorage.com" />
          <Input id="image-storage-region" v-model="form.region" :label="t('admin.backup.s3.region')" placeholder="auto" />
          <Input id="image-storage-access-key" v-model="form.access_key_id" :label="t('admin.backup.s3.accessKeyId')" autocomplete="off" />
          <Input id="image-storage-secret" v-model="form.secret_access_key" type="password" :label="t('admin.backup.s3.secretAccessKey')" autocomplete="new-password" :placeholder="secretConfigured ? t('admin.backup.s3.secretConfigured') : ''" />
          <div class="flex items-center justify-between gap-4 md:col-span-2">
            <label for="image-storage-force-path" class="text-sm text-gray-700 dark:text-dark-200">{{ t('admin.backup.s3.forcePathStyle') }}</label>
            <Toggle id="image-storage-force-path" v-model="form.force_path_style" :aria-label="t('admin.backup.s3.forcePathStyle')" />
          </div>
        </template>
        <Input id="image-storage-public-url" v-model="form.public_base_url" :label="t('admin.backup.imageStorage.publicBaseUrl')" :placeholder="t('admin.backup.imageStorage.publicBaseUrlPlaceholder')" />
        <Input id="image-storage-presign-hours" :model-value="form.presign_expiry_hours" type="number" min="1" step="1" :label="t('admin.backup.imageStorage.presignExpiryHours')" @update:model-value="form.presign_expiry_hours = Number($event)" />
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="button" data-testid="image-storage-test" class="btn btn-secondary btn-sm" @click="testConnection">{{ testing ? t('common.loading') : t('admin.backup.s3.testConnection') }}</button>
        <button type="button" data-testid="image-storage-save" class="btn btn-primary btn-sm" @click="saveConfig">{{ saving ? t('common.loading') : t('common.save') }}</button>
      </div>
    </fieldset>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import type { ImageStorageConfig } from '@/api/admin/backup'
import { useAppStore } from '@/stores'
import Input from '@/components/common/Input.vue'
import Toggle from '@/components/common/Toggle.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const testing = ref(false)
const loadError = ref('')
const secretConfigured = ref(false)
const form = ref<ImageStorageConfig>({
  enabled: false, studio_gallery_enabled: false, studio_jobs_enabled: false,
  reuse_backup_s3: true, bucket: '', prefix: 'images/', public_base_url: '',
  presign_expiry_hours: 24, max_download_bytes: 33554432, endpoint: '', region: 'auto',
  access_key_id: '', secret_access_key: '', force_path_style: false,
})
let disposed = false

function errorMessage(error: unknown) {
  if (isStepUpBlocked(error)) return t(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  return (error as { message?: string })?.message || t('errors.networkError')
}

async function loadConfig() {
  if (disposed) return
  loading.value = true
  loadError.value = ''
  try {
    const { config, secret_configured } = await adminAPI.backup.getImageStorageConfig()
    if (disposed) return
    form.value = {
      ...config,
      studio_gallery_enabled: config.studio_gallery_enabled === true,
      studio_jobs_enabled: config.studio_gallery_enabled === true && config.studio_jobs_enabled === true,
      prefix: config.prefix || 'images/', region: config.region || 'auto', secret_access_key: '',
    }
    secretConfigured.value = secret_configured
    loaded.value = true
  } catch (error) {
    if (!disposed) { loaded.value = false; loadError.value = errorMessage(error) }
  } finally { if (!disposed) loading.value = false }
}

function setGalleryEnabled(enabled: boolean) {
  form.value.studio_gallery_enabled = enabled
  if (!enabled) form.value.studio_jobs_enabled = false
}

async function saveConfig() {
  if (!loaded.value || loading.value || saving.value || testing.value) return
  saving.value = true
  const payload = { ...form.value }
  try {
    await stepUp.run(() => adminAPI.backup.updateImageStorageConfig(payload))
    if (disposed) return
    form.value.secret_access_key = ''
    appStore.showSuccess(t('admin.backup.imageStorage.saved'))
    await loadConfig()
  } catch (error) {
    if (!disposed && !isStepUpCancelled(error)) appStore.showError(errorMessage(error))
  } finally { if (!disposed) saving.value = false }
}

async function testConnection() {
  if (!loaded.value || loading.value || saving.value || testing.value) return
  testing.value = true
  try {
    const result = await adminAPI.backup.testImageStorageConnection({ ...form.value })
    if (disposed) return
    if (result.ok) appStore.showSuccess(result.message || t('admin.backup.s3.testSuccess'))
    else appStore.showError(result.message || t('admin.backup.s3.testFailed'))
  } catch (error) { if (!disposed) appStore.showError(errorMessage(error)) }
  finally { if (!disposed) testing.value = false }
}

onMounted(loadConfig)
onBeforeUnmount(() => { disposed = true; form.value.secret_access_key = '' })
</script>
