<template>
  <details class="card" data-test="agent-profile-catalog">
    <summary class="cursor-pointer px-6 py-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.agentSettings.profileCatalog.title') }}</summary>
    <div class="space-y-4 border-t border-gray-100 p-6 dark:border-dark-700">
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.profileCatalog.description') }}</p>
      <p v-if="errorKey" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(errorKey) }}</p>
      <p v-if="noticeKey" role="status" class="text-sm text-green-700 dark:text-green-400">{{ t(noticeKey) }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <template v-if="status">
        <dl class="grid min-w-0 gap-4 rounded-xl bg-gray-50 p-4 text-sm sm:grid-cols-3 dark:bg-dark-900/50">
          <div><dt class="text-xs text-gray-500">{{ t('admin.agentSettings.profileCatalog.revision') }}</dt><dd class="mt-1 break-all font-medium" data-test="profile-catalog-revision">{{ status.revision }}</dd></div>
          <div><dt class="text-xs text-gray-500">{{ t('admin.agentSettings.profileCatalog.count') }}</dt><dd class="mt-1 font-medium">{{ status.profile_count }}</dd></div>
          <div><dt class="text-xs text-gray-500">{{ t('admin.agentSettings.profileCatalog.lastSuccess') }}</dt><dd class="mt-1 font-medium">{{ lastSuccess }}</dd></div>
        </dl>
        <p class="text-xs text-gray-500">{{ t(`admin.agentSettings.profileCatalog.${status.using_fallback ? 'builtin' : 'remote'}`) }}</p>
        <p v-if="status.last_error && !errorKey" data-test="profile-catalog-last-error" class="text-sm text-amber-700 dark:text-amber-300">{{ t('admin.agentSettings.profileCatalog.refreshFailed') }}</p>
        <Input id="agent-profile-source" v-model="sourceDraft" type="url" :label="t('admin.agentSettings.profileCatalog.source')" :hint="t('admin.agentSettings.profileCatalog.sourceHint')" :maxlength="2048" :disabled="busy || disabled || !active" :error="sourceInvalid ? t('admin.agentSettings.profileCatalog.sourceInvalid') : ''" autocomplete="off" />
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" data-test="profile-catalog-save" class="btn btn-secondary btn-sm" :disabled="busy || disabled || !active || sourceInvalid" @click="saveSource">{{ t('admin.agentSettings.profileCatalog.save') }}</button>
          <button type="button" data-test="profile-catalog-refresh" class="btn btn-secondary btn-sm" :disabled="busy || disabled || !active || !status.source_url" @click="refresh">{{ t('admin.agentSettings.profileCatalog.refresh') }}</button>
        </div>
        <p class="text-xs text-gray-500">{{ t('admin.agentSettings.profileCatalog.retained') }}</p>
      </template>
      <button v-else-if="!loading" type="button" data-test="profile-catalog-retry" class="btn btn-secondary btn-sm" :disabled="busy || disabled || !active" @click="load">{{ t('admin.agentSettings.profileCatalog.reload') }}</button>
    </div>
    <TotpStepUpDialog :controller="stepUp" />
  </details>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Input from '@/components/common/Input.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { getDesktopAgentProfileCatalog, refreshDesktopAgentProfileCatalog, updateDesktopAgentProfileCatalogSource } from '@/api/admin/desktopAgent'
import type { DesktopAgentProfileCatalogStatus } from '@/api/admin/desktopAgent'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, StepUpCancelledError, useStepUp } from '@/composables/useStepUp'

const props = withDefaults(defineProps<{ active?: boolean; disabled?: boolean }>(), { active: true, disabled: false })
const emit = defineEmits<{ updated: [] }>()
const { t, locale } = useI18n()
const stepUp = useStepUp()
const status = ref<DesktopAgentProfileCatalogStatus | null>(null)
const sourceDraft = ref('')
const loading = ref(false)
const writing = ref(false)
const errorKey = ref('')
const noticeKey = ref('')
const busy = computed(() => loading.value || writing.value)
let alive = true
let generation = 0
const lastSuccess = computed(() => {
  const value = status.value?.last_success_at
  if (!value) return t('admin.agentSettings.profileCatalog.neverSynced')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? t('admin.agentSettings.profileCatalog.neverSynced') : date.toLocaleString(locale.value)
})
const sourceInvalid = computed(() => {
  const value = sourceDraft.value.trim()
  if (!value) return false
  try {
    const url = new URL(value)
    return !/^https:\/\/[^/?#@]+(?:\/[^?#]*)?$/.test(value) || !url.hostname || !!url.username || !!url.password || !!url.search || !!url.hash || /[\\\s]/.test(value)
  } catch { return true }
})
function current(id: number) { return alive && props.active && id === generation }
function apply(value: DesktopAgentProfileCatalogStatus, replaceDraft: boolean) {
  status.value = value
  if (replaceDraft) sourceDraft.value = value.source_url
}
function report(error: unknown, fallback: string) {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    errorKey.value = stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled'
  } else errorKey.value = fallback
}
async function load() {
  if (!alive || !props.active || busy.value) return
  const id = ++generation
  loading.value = true
  errorKey.value = ''
  try {
    const value = await getDesktopAgentProfileCatalog()
    if (current(id)) apply(value, true)
  } catch (error) {
    if (current(id)) report(error, 'admin.agentSettings.profileCatalog.loadFailed')
  } finally { if (current(id)) loading.value = false }
}
async function mutate(action: () => Promise<DesktopAgentProfileCatalogStatus>, sourceWrite: boolean) {
  if (!alive || !props.active || props.disabled || busy.value || !status.value) return
  const id = ++generation
  writing.value = true
  errorKey.value = ''
  noticeKey.value = ''
  try {
    const value = await stepUp.run(async () => {
      if (!current(id)) throw new StepUpCancelledError()
      try { return await action() }
      catch (error) { if (!current(id)) throw new StepUpCancelledError(); throw error }
    })
    if (current(id)) {
      apply(value, sourceWrite)
      noticeKey.value = `admin.agentSettings.profileCatalog.${sourceWrite ? 'saved' : 'refreshed'}`
      emit('updated')
    } else if (alive) status.value = null
  } catch (error) {
    if (current(id)) report(error, `admin.agentSettings.profileCatalog.${sourceWrite ? 'saveFailed' : 'refreshFailed'}`)
  } finally {
    if (alive) {
      writing.value = false
      if (props.active && !status.value) void load()
    }
  }
}
function saveSource() {
  if (sourceInvalid.value) return
  const value = sourceDraft.value.trim()
  return mutate(() => updateDesktopAgentProfileCatalogSource(value), true)
}
function refresh() { return mutate(refreshDesktopAgentProfileCatalog, false) }
watch(sourceDraft, () => { noticeKey.value = '' })
watch(() => props.active, active => {
  if (active) { if (!status.value && !writing.value) void load() }
  else {
    generation += 1
    loading.value = false
    if (writing.value) status.value = null
    stepUp.onCancel()
  }
}, { immediate: true })
onBeforeUnmount(() => { alive = false; generation += 1; stepUp.onCancel() })
</script>
