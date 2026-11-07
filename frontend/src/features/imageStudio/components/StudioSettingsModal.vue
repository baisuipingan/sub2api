<template>
  <BaseDialog
    :show="open"
    :title="t('imageStudio.settings.title')"
    width="normal"
    :z-index="100"
    :close-on-escape="false"
    close-on-click-outside
    @close="state.setShowSettings(false)"
  >
    <p class="mb-5 text-sm text-gray-500 dark:text-dark-400">{{ t('imageStudio.settings.subtitle') }}</p>
    <div class="space-y-6">
      <!-- 创作偏好 -->
      <section>
        <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.settings.preferences') }}
        </h3>
        <div
          class="divide-y divide-gray-100 rounded-xl border border-gray-200 dark:divide-white/[0.08] dark:border-white/[0.1]"
        >
          <div
            v-for="item in preferences"
            :key="item.key"
            class="flex items-center justify-between gap-4 px-4 py-3 text-sm text-gray-700 dark:text-dark-200"
          >
            <span>{{ item.label }}</span>
            <Toggle
              :model-value="state.settings[item.key]"
              :aria-label="item.label"
              :disabled="item.key === 'taskCompletionNotification' && requestingNotification"
              :aria-busy="item.key === 'taskCompletionNotification' && requestingNotification"
              @update:model-value="setPreference(item.key, $event)"
            />
          </div>
        </div>
        <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('imageStudio.settings.notificationHint') }}</p>
      </section>

      <section v-if="state.galleryInitialized && state.galleryUserId" data-testid="studio-cloud-gallery-settings">
        <div class="mb-3 flex items-center justify-between gap-2">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('imageStudio.gallery.title') }}</h3>
          <button type="button" class="btn btn-ghost btn-sm" :title="t('imageStudio.gallery.refresh')" :aria-label="t('imageStudio.gallery.refresh')" @click="refreshGalleryStats">
            <StudioIcon name="RefreshIcon" class="h-4 w-4" />
          </button>
        </div>
        <div class="rounded-xl border border-gray-200 p-4 text-xs dark:border-white/[0.1]">
          <dl v-if="state.galleryStats" class="grid grid-cols-1 gap-x-4 gap-y-2 text-gray-600 sm:grid-cols-2 dark:text-dark-300">
            <div>{{ t('imageStudio.gallery.taskCount', { count: state.galleryStats.tasks, limit: state.galleryStats.max_tasks }) }}</div>
            <div>{{ t('imageStudio.gallery.taskBreakdown', { done: state.galleryStats.done_tasks, failed: state.galleryStats.failed_tasks }) }}</div>
            <div>{{ t('imageStudio.gallery.outputCount', { count: state.galleryStats.output_images }) }}</div>
            <div>{{ t('imageStudio.gallery.assetCount', { count: state.galleryStats.assets, limit: state.galleryStats.max_assets }) }}</div>
            <div class="sm:col-span-2">{{ t('imageStudio.gallery.usedSpace', { used: formatBytes(state.galleryStats.used_bytes, 1), limit: formatBytes(state.galleryStats.max_bytes, 1) }) }}</div>
          </dl>
          <p v-else class="text-gray-500 dark:text-dark-400">{{ t('imageStudio.gallery.statsUnavailable') }}</p>
          <div class="mt-3 border-t border-gray-100 pt-3 dark:border-white/[0.08]">
            <p class="text-gray-500 dark:text-dark-400">
              {{ t('imageStudio.gallery.statusSummary', syncCounts) }}
            </p>
            <p v-if="state.galleryImportProgress" class="mt-1 text-gray-500 dark:text-dark-400">
              {{ t('imageStudio.gallery.importProgress', state.galleryImportProgress) }}
            </p>
            <div class="mt-2 flex flex-wrap gap-2">
              <button v-if="syncCounts.failed" type="button" class="btn btn-secondary btn-sm" @click="retryFailedGalleryTasks">
                {{ t('imageStudio.gallery.retryAll') }}
              </button>
              <button type="button" class="btn btn-ghost btn-sm" @click="setGallerySyncPaused(!state.gallerySyncPaused)">
                {{ t(state.gallerySyncPaused ? 'imageStudio.gallery.resumeSync' : 'imageStudio.gallery.pauseSync') }}
              </button>
            </div>
          </div>
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('imageStudio.gallery.pauseHint') }}</p>
      </section>

      <!-- 本地数据 -->
      <section>
        <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.settings.localData') }}
        </h3>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn btn-secondary btn-sm gap-2"
            @click="exportData({ exportConfig: true, exportTasks: true })"
          >
            <StudioIcon name="ExportIcon" class="h-4 w-4" />{{ t('imageStudio.settings.exportBackup') }}
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm gap-2"
            :disabled="importing"
            @click="fileRef?.click()"
          >
            <StudioIcon name="ImportIcon" class="h-4 w-4" />{{
              importing ? t('imageStudio.settings.importing') : t('imageStudio.settings.importBackup')
            }}
          </button>
          <button
            type="button"
            class="btn btn-danger btn-sm"
            @click="clearData({ clearConfig: false, clearTasks: true })"
          >
            {{ state.galleryUserId ? t('imageStudio.settings.clearLocalCache') : t('imageStudio.settings.clearHistory') }}
          </button>
          <button
            v-if="state.galleryInitialized && state.galleryUserId"
            type="button"
            class="btn btn-secondary btn-sm"
            @click="confirmLegacyImport"
          >{{ t('imageStudio.settings.importLegacyHistory') }}</button>
        </div>
        <input
          ref="fileRef"
          type="file"
          accept=".zip"
          class="hidden"
          @change="handleImport"
        />
      </section>

      <p class="text-xs leading-relaxed text-gray-500 dark:text-dark-400">
        {{ t('imageStudio.settings.footerHint') }}
      </p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import { storeState as state, clearData, exportData, importData, importLegacyHistory, refreshGalleryStats, retryFailedGalleryTasks, setGallerySyncPaused } from '../stores/store'
import { formatBytes } from '@/utils/format'
import { getLegacyTasks } from '../lib/db'
import { captureGallerySession, isGallerySessionCurrent } from '../lib/gallerySession'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { getBrowserNotificationSession, requestBrowserNotificationPermission } from '../lib/browserNotification'

const { t } = useI18n()

const importing = ref(false)
const requestingNotification = ref(false)
const fileRef = ref<HTMLInputElement | null>(null)

const open = computed(() => state.showSettings)
const syncCounts = computed(() => ({
  pending: Object.values(state.gallerySyncStatus ?? {}).filter((status) => status === 'pending').length,
  syncing: Object.values(state.gallerySyncStatus ?? {}).filter((status) => status === 'syncing').length,
  failed: Object.values(state.gallerySyncStatus ?? {}).filter((status) => status === 'error').length,
}))

useCloseOnEscape(() => open.value, () => state.setShowSettings(false))

type PrefKey = 'clearInputAfterSubmit' | 'taskCompletionNotification'

const preferences = computed<Array<{ key: PrefKey; label: string }>>(() => [
  { key: 'clearInputAfterSubmit', label: t('imageStudio.settings.prefClearInput') },
  { key: 'taskCompletionNotification', label: t('imageStudio.settings.prefNotify') },
])

async function setPreference(key: PrefKey, value: boolean) {
  if (key !== 'taskCompletionNotification' || !value) {
    state.setSettings({ [key]: value })
    return
  }
  if (requestingNotification.value) return
  const session = getBrowserNotificationSession()
  requestingNotification.value = true
  try {
    const result = await requestBrowserNotificationPermission()
    if (getBrowserNotificationSession() !== session) return
    state.setSettings({ taskCompletionNotification: result.ok })
    if (!result.ok) {
      const messages = {
        unsupported: 'imageStudio.settings.notificationUnsupported',
        insecure: 'imageStudio.settings.notificationInsecure',
        denied: 'imageStudio.settings.notificationDenied',
        default: 'imageStudio.settings.notificationPermissionRequired',
        error: 'imageStudio.settings.notificationEnableFailed',
      }
      state.showToast(t(messages[result.reason]), 'error')
    }
  } catch {
    if (getBrowserNotificationSession() === session) {
      state.setSettings({ taskCompletionNotification: false })
      state.showToast(t('imageStudio.settings.notificationEnableFailed'), 'error')
    }
  } finally {
    requestingNotification.value = false
  }
}

async function handleImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  importing.value = true
  try {
    await importData(file, { importConfig: true, importTasks: true })
  } finally {
    importing.value = false
  }
}

async function confirmLegacyImport() {
  if (!state.galleryInitialized || !state.galleryUserId) return
  const session = captureGallerySession()
  const userId = state.galleryUserId
  let count = 0
  try {
    count = (await getLegacyTasks()).length
  } catch {
    if (isGallerySessionCurrent(session) && state.galleryUserId === userId) {
      state.showToast(t('imageStudio.gallery.importFailed'), 'error')
    }
    return
  }
  if (!isGallerySessionCurrent(session) || state.galleryUserId !== userId) return
  state.setShowSettings(false)
  state.setConfirmDialog({
    title: t('imageStudio.settings.importLegacyHistory'),
    message: t('imageStudio.settings.importLegacyHint', { count, userId }),
    confirmText: t('imageStudio.settings.importLegacyHistory'),
    cancelText: t('imageStudio.common.cancel'),
    action: () => isGallerySessionCurrent(session) && state.galleryUserId === userId
      ? importLegacyHistory()
      : undefined,
  })
}
</script>
