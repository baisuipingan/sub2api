<template>
  <section class="card space-y-5 p-6" data-test="desktop-release-storage" aria-labelledby="desktop-release-storage-title">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0 flex-1">
        <h3 id="desktop-release-storage-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.desktopReleaseStorage.title') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.desktopReleaseStorage.description') }}</p>
      </div>
      <button type="button" data-test="release-storage-reload" class="btn btn-secondary btn-sm" :disabled="busy || !active" @click="load">{{ t('common.refresh') }}</button>
    </div>

    <p v-if="errorKey" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(errorKey) }}</p>
    <p v-if="noticeKey" role="status" class="text-sm text-green-700 dark:text-green-400">{{ t(noticeKey) }}</p>
    <p v-if="loading || (saving && !loaded)" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>

    <template v-if="loaded">
      <div class="rounded-xl bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:bg-dark-900/50 dark:text-gray-300">
        <p data-test="release-storage-status" class="font-medium">{{ t(`admin.desktopReleaseStorage.${configured ? 'configured' : 'notConfigured'}`) }}</p>
        <p class="mt-1">{{ t('admin.desktopReleaseStorage.inheritedConnection') }}</p>
        <p v-if="!backupConfigured" data-test="release-storage-backup-missing" class="mt-2 text-amber-700 dark:text-amber-300">{{ t('admin.desktopReleaseStorage.backupMissing') }}</p>
      </div>

      <fieldset class="grid min-w-0 gap-5 md:grid-cols-2" :disabled="busy || !active">
        <Input id="desktop-release-bucket" v-model="draft.bucket" :label="t('admin.desktopReleaseStorage.bucket')" placeholder="milocode-releases" :hint="t('admin.desktopReleaseStorage.bucketHint')" :maxlength="63" autocomplete="off" :disabled="busy || !active" :error="submitted && validationKey === 'bucketInvalid' ? t('admin.desktopReleaseStorage.bucketInvalid') : ''" />
        <Input id="desktop-release-domain" v-model="draft.public_base_url" :label="t('admin.desktopReleaseStorage.domain')" placeholder="https://download.editor.vip" :hint="t('admin.desktopReleaseStorage.domainHint')" :maxlength="2048" autocomplete="off" :disabled="busy || !active" :error="submitted && validationKey === 'domainInvalid' ? t('admin.desktopReleaseStorage.domainInvalid') : ''" />
      </fieldset>
      <p v-if="submitted && validationKey === 'bothRequired'" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.desktopReleaseStorage.bothRequired') }}</p>
      <p v-if="releaseBaseUrl" class="break-all text-xs text-gray-500 dark:text-gray-400" data-test="release-storage-base">{{ t('admin.desktopReleaseStorage.releaseBase', { url: releaseBaseUrl }) }}</p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.desktopReleaseStorage.clientTrustHint') }}</p>

      <div class="flex flex-wrap items-center gap-3">
        <button type="button" data-test="release-storage-save" class="btn btn-primary" :disabled="busy || !active" @click="save">{{ saving ? t('common.saving') : t('admin.desktopReleaseStorage.save') }}</button>
        <button type="button" data-test="release-storage-test" class="btn btn-secondary" :disabled="busy || !active || !backupConfigured" @click="testDraft">{{ testing ? t('admin.desktopReleaseStorage.testing') : t('admin.desktopReleaseStorage.test') }}</button>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.desktopReleaseStorage.disableHint') }}</span>
      </div>
    </template>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Input from '@/components/common/Input.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { getDesktopReleaseStorage, testDesktopReleaseStorage, updateDesktopReleaseStorage } from '@/api/admin/desktopReleaseStorage'
import type { DesktopReleaseStorageConfig, DesktopReleaseStorageDraft } from '@/api/admin/desktopReleaseStorage'
import { extractApiErrorCode } from '@/utils/apiError'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, StepUpCancelledError, useStepUp } from '@/composables/useStepUp'

const props = withDefaults(defineProps<{ active?: boolean }>(), { active: true })
const { t } = useI18n()
const stepUp = useStepUp()
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const configured = ref(false)
const backupConfigured = ref(false)
const submitted = ref(false)
const errorKey = ref('')
const noticeKey = ref('')
const draft = ref<DesktopReleaseStorageDraft>({ bucket: '', public_base_url: '' })
const busy = computed(() => loading.value || saving.value || testing.value)
let alive = true
let generation = 0

function publicOrigin(value: string): string | null {
  try {
    if (!/^https:\/\/[^/?#]+\/?$/i.test(value)) return null
    const url = new URL(value)
    if (url.protocol !== 'https:' || !url.hostname || url.username || url.password || url.search || url.hash || url.pathname !== '/' || /[\\\s]/.test(value)) return null
    return url.origin
  } catch { return null }
}
const validationKey = computed(() => {
  const bucket = draft.value.bucket.trim()
  const domain = draft.value.public_base_url.trim()
  if (!bucket && !domain) return ''
  if (!bucket || !domain) return 'bothRequired'
  if (!/^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(bucket) || bucket.includes('..')) return 'bucketInvalid'
  if (!publicOrigin(domain)) return 'domainInvalid'
  return ''
})
const releaseBaseUrl = computed(() => {
  const origin = publicOrigin(draft.value.public_base_url.trim())
  return origin ? `${origin}/milocode/` : ''
})
function current(id: number) { return alive && props.active && id === generation }
function snapshot(): DesktopReleaseStorageDraft {
  return { bucket: draft.value.bucket.trim(), public_base_url: draft.value.public_base_url.trim() ? publicOrigin(draft.value.public_base_url.trim()) || '' : '' }
}
function applyConfig(value: DesktopReleaseStorageConfig) {
  draft.value = { bucket: value.bucket || '', public_base_url: value.public_base_url || '' }
  backupConfigured.value = value.backup_s3_configured === true
  configured.value = value.configured === true
  loaded.value = true
  submitted.value = false
}
function report(error: unknown, fallback: string) {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    errorKey.value = stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled'
    return
  }
  const code = extractApiErrorCode(error)
  errorKey.value = code === 'DESKTOP_RELEASE_STORAGE_INVALID' ? 'admin.desktopReleaseStorage.rejected' : fallback
}
async function load() {
  if (!alive || !props.active || busy.value) return
  const id = ++generation
  loading.value = true
  errorKey.value = ''
  noticeKey.value = ''
  try {
    const result = await getDesktopReleaseStorage()
    if (current(id)) applyConfig(result)
  } catch (error) {
    if (current(id)) report(error, 'admin.desktopReleaseStorage.loadFailed')
  } finally { if (current(id)) loading.value = false }
}
async function save() {
  if (!loaded.value || !props.active || busy.value || !alive) return
  submitted.value = true
  errorKey.value = ''
  noticeKey.value = ''
  if (validationKey.value) return
  const payload = snapshot()
  const id = ++generation
  saving.value = true
  try {
    const result = await stepUp.run(async () => {
      if (!current(id)) throw new StepUpCancelledError()
      try { return await updateDesktopReleaseStorage(payload) }
      catch (error) { if (!current(id)) throw new StepUpCancelledError(); throw error }
    })
    if (current(id)) {
      applyConfig(result)
      noticeKey.value = 'admin.desktopReleaseStorage.saved'
    }
  } catch (error) {
    if (current(id)) report(error, 'admin.desktopReleaseStorage.saveFailed')
  } finally {
    if (alive) {
      saving.value = false
      // 写请求已到服务端时不能撤回，重新进入面板需等其收口后再读取权威配置。
      if (props.active && !loaded.value) void load()
    }
  }
}
async function testDraft() {
  if (!loaded.value || !props.active || busy.value || !backupConfigured.value || !alive) return
  submitted.value = true
  errorKey.value = ''
  noticeKey.value = ''
  if (validationKey.value || !draft.value.bucket.trim()) {
    if (!validationKey.value) errorKey.value = 'admin.desktopReleaseStorage.bothRequired'
    return
  }
  const id = ++generation
  const payload = snapshot()
  testing.value = true
  try {
    const result = await stepUp.run(async () => {
      if (!current(id)) throw new StepUpCancelledError()
      try { return await testDesktopReleaseStorage(payload) }
      catch (error) { if (!current(id)) throw new StepUpCancelledError(); throw error }
    })
    if (current(id)) {
      if (result.success) noticeKey.value = 'admin.desktopReleaseStorage.testSuccess'
      else errorKey.value = 'admin.desktopReleaseStorage.testFailed'
    }
  } catch (error) {
    if (current(id)) report(error, 'admin.desktopReleaseStorage.testFailed')
  } finally { if (current(id)) testing.value = false }
}
watch(() => props.active, active => {
  if (active) {
    if (!loaded.value) void load()
  } else {
    // 设置面板通过v-show切换：迟到请求不能覆盖重新进入后的草稿或弹出step-up。
    generation += 1
    if (saving.value) loaded.value = false
    loading.value = false
    testing.value = false
    stepUp.onCancel()
  }
}, { immediate: true })
watch(() => [draft.value.bucket, draft.value.public_base_url], () => { noticeKey.value = ''; errorKey.value = '' }, { flush: 'sync' })
onBeforeUnmount(() => { alive = false; generation += 1; stepUp.onCancel() })
</script>
