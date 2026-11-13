<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <section class="card p-6 sm:p-8" aria-labelledby="desktop-download-heading">
        <div class="flex items-start gap-4">
          <div class="rounded-2xl bg-primary-50 p-3 text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
            <Icon name="terminal" size="xl" />
          </div>
          <div>
            <h2 id="desktop-download-heading" class="text-xl font-semibold text-gray-900 dark:text-white">MiloCode</h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('desktop.downloads.description') }}</p>
          </div>
        </div>
      </section>

      <div v-if="loading" class="card flex justify-center p-12" aria-live="polite">
        <LoadingSpinner size="lg" />
      </div>
      <section v-else-if="failed" class="card p-6" role="alert">
        <EmptyState :title="t('desktop.downloads.loadFailed')" :description="t('desktop.downloads.retryHint')" :action-text="t('desktop.downloads.retry')" :action-icon="false" @action="load" />
      </section>
      <div v-else class="grid gap-5 sm:grid-cols-2">
        <section v-for="item in platforms" :key="item.platform" class="card flex flex-col p-6" :data-platform="item.platform">
          <div class="flex items-start justify-between gap-3">
            <div>
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ item.platform === 'win32-x64' ? 'Windows' : 'macOS' }}</h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ item.platform === 'win32-x64' ? 'x64' : 'Apple Silicon · ARM64' }}</p>
            </div>
            <span v-if="item.available" class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">v{{ item.version }}</span>
          </div>
          <p class="mt-5 flex-1 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ item.available ? t(item.platform === 'win32-x64' ? 'desktop.downloads.windowsInstall' : 'desktop.downloads.macInstall') : t('desktop.downloads.unavailableHint') }}</p>
          <p v-if="item.available" class="mt-4 text-xs text-gray-500 dark:text-dark-400">{{ item.format?.toUpperCase() }} · {{ formatSize(item.size) }}</p>
          <a v-if="item.available && item.url" :href="item.url" class="btn btn-primary mt-4 w-full" rel="noopener noreferrer" download>
            <Icon name="download" size="md" class="mr-2" />
            {{ t(item.platform === 'win32-x64' ? 'desktop.downloads.windowsDownload' : 'desktop.downloads.macDownload') }}
          </a>
          <button v-else type="button" class="btn btn-secondary mt-4 w-full" disabled>{{ t('desktop.downloads.unavailable') }}</button>
        </section>
      </div>
      <p v-if="!loading && !failed && platforms.some(item => item.available)" class="text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('desktop.downloads.firstInstallHint') }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { getDesktopDownloads, type DesktopDownload, type DesktopDownloadPlatform } from '@/api/desktopDownloads'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const loading = ref(true)
const failed = ref(false)
const downloads = ref<DesktopDownload[]>([])
let active = true
const platforms = computed(() => (['win32-x64', 'darwin-arm64'] as DesktopDownloadPlatform[]).map(platform => {
  const item = downloads.value.find(value => value.platform === platform)
  const url = item?.url?.startsWith('https://') ? sanitizeUrl(item.url) : ''
  return { ...item, platform, url, available: Boolean(item?.available && item.version && url) }
}))

function formatSize(bytes?: number): string {
  return bytes && Number.isFinite(bytes) ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : ''
}

async function load() {
  loading.value = true
  failed.value = false
  try {
    const result = await getDesktopDownloads()
    if (active) downloads.value = result.platforms
  } catch {
    if (active) failed.value = true
  } finally {
    if (active) loading.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => { active = false })
</script>
