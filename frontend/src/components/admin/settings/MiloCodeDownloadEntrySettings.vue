<template>
  <section class="card flex items-start justify-between gap-4 p-6" aria-labelledby="milocode-download-entry-title">
    <div class="min-w-0">
      <h3 id="milocode-download-entry-title" class="font-semibold text-gray-900 dark:text-white">
        {{ t('admin.agentSettings.downloadEntryTitle') }}
      </h3>
      <p id="milocode-download-entry-description" class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.agentSettings.downloadEntryDescription') }}
      </p>
    </div>
    <Toggle
      :model-value="enabled"
      :disabled="saving || !appStore.cachedPublicSettings"
      :aria-busy="saving"
      aria-labelledby="milocode-download-entry-title"
      aria-describedby="milocode-download-entry-description"
      @update:model-value="save"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { updateMiloCodeDownloadEntry } from '@/api/admin/settings'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'

const { t } = useI18n()
const appStore = useAppStore()
const saving = ref(false)
const enabled = computed(() => resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.miloCodeDownloadEntry))

async function save(value: boolean) {
  if (saving.value || value === enabled.value) return
  saving.value = true
  try {
    const updated = await updateMiloCodeDownloadEntry(value)
    // Let an already-running settings request finish before forcing a fresh read.
    await appStore.fetchPublicSettings()
    if (appStore.cachedPublicSettings) {
      appStore.cachedPublicSettings.milocode_download_entry_enabled = updated.milocode_download_entry_enabled
    }
    await appStore.fetchPublicSettings(true)
    appStore.showSuccess(t('common.saved'))
  } catch {
    appStore.showError(t('admin.agentSettings.downloadEntrySaveFailed'))
  } finally {
    saving.value = false
  }
}
</script>
