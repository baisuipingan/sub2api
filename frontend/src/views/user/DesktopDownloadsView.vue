<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <section class="card overflow-hidden" aria-labelledby="desktop-download-heading">
        <div class="grid lg:grid-cols-[1.45fr_1fr]">
          <div class="min-w-0 p-6 sm:p-10">
            <div class="mb-6 inline-flex items-center gap-2 rounded-full bg-primary-50 px-3 py-1.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              <img :src="miloCodeIcon" alt="" width="24" height="24" class="h-6 w-6 rounded-md bg-white object-contain" data-testid="milocode-download-badge-icon" />
              {{ t('desktop.downloads.eyebrow') }}
            </div>
            <p class="text-sm font-semibold tracking-wide text-primary-600 dark:text-primary-400">MiloCode</p>
            <h1 id="desktop-download-heading" class="mt-3 max-w-xl text-3xl font-semibold leading-tight tracking-tight text-gray-900 dark:text-white sm:text-4xl">{{ t('desktop.downloads.headline') }}</h1>
            <p class="mt-5 max-w-xl text-base leading-7 text-gray-600 dark:text-gray-300">{{ t('desktop.downloads.description') }}</p>
          </div>

          <aside class="border-t border-gray-100 bg-gradient-to-br from-primary-50 via-gray-50 to-white p-6 dark:border-dark-700 dark:from-primary-950/40 dark:via-dark-800 dark:to-dark-900 sm:p-10 lg:border-l lg:border-t-0" aria-labelledby="desktop-features-heading">
            <div class="h-16 w-16 overflow-hidden rounded-2xl bg-white shadow-sm ring-1 ring-primary-100 dark:ring-primary-800/50">
              <img :src="miloCodeIcon" alt="MiloCode" width="64" height="64" class="h-full w-full object-contain" data-testid="milocode-download-product-icon" />
            </div>
            <h2 id="desktop-features-heading" class="mt-6 text-xl font-semibold text-gray-900 dark:text-white">{{ t('desktop.downloads.featuresTitle') }}</h2>
            <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('desktop.downloads.featuresDescription') }}</p>
            <div class="mt-8 space-y-6">
              <div v-for="feature in features" :key="feature.key" class="flex gap-3">
                <div class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-white/80 text-primary-600 ring-1 ring-gray-100 dark:bg-dark-800 dark:text-primary-400 dark:ring-dark-700">
                  <Icon :name="feature.icon" size="md" aria-hidden="true" />
                </div>
                <div class="min-w-0">
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t(`desktop.downloads.${feature.key}Title`) }}</h3>
                  <p class="mt-1 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ t(`desktop.downloads.${feature.key}Description`) }}</p>
                </div>
              </div>
            </div>
          </aside>
        </div>
        <div class="border-t border-gray-100 p-6 dark:border-dark-700 sm:p-10">
          <div v-if="loading" class="flex min-h-44 items-center justify-center rounded-xl border border-gray-100 dark:border-dark-700" aria-live="polite">
            <LoadingSpinner size="lg" />
          </div>
          <div v-else-if="failed" class="rounded-xl border border-gray-200 p-5 dark:border-dark-700" role="alert">
            <EmptyState :title="t('desktop.downloads.loadFailed')" :description="t('desktop.downloads.retryHint')" :action-text="t('desktop.downloads.retry')" :action-icon="false" @action="load" />
          </div>
          <div v-else class="grid gap-4 md:grid-cols-3">
            <section v-for="item in platforms" :key="item.platform" class="flex min-w-0 flex-col rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900/40" :data-platform="item.platform">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h2 class="font-semibold text-gray-900 dark:text-white">{{ item.platform === 'win32-x64' ? 'Windows' : 'macOS' }}</h2>
                <span v-if="item.available" class="rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">v{{ item.version }}</span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ platformArchitecture(item.platform) }}</p>
              <template v-if="item.available && item.url">
                <button v-if="canAccelerateDownload(item)" type="button" class="btn btn-primary mt-4 w-full gap-2" :disabled="Boolean(acceleratedDownload)" :data-testid="`accelerated-download-${item.platform}`" @click="startAcceleratedDownload(item)">
                  <Icon name="bolt" size="md" class="shrink-0" aria-hidden="true" />
                  {{ t('desktop.downloads.acceleratedDownload') }}
                </button>
                <a :href="item.url" class="btn w-full gap-2" :class="canAccelerateDownload(item) ? 'btn-secondary mt-2' : 'btn-primary mt-4'" rel="noopener noreferrer" download>
                  <Icon name="download" size="md" class="shrink-0" aria-hidden="true" />
                  {{ t(canAccelerateDownload(item) ? 'desktop.downloads.ordinaryDownload' : item.platform === 'win32-x64' ? 'desktop.downloads.windowsDownload' : 'desktop.downloads.macDownload') }}
                </a>
              </template>
              <button v-else type="button" class="btn btn-secondary mt-4 w-full" disabled>{{ t('desktop.downloads.unavailable') }}</button>
              <div v-if="acceleratedDownload?.platform === item.platform" class="mt-3 space-y-2" role="status" aria-live="polite" data-testid="accelerated-download-progress">
                <p class="text-xs text-gray-600 dark:text-gray-300">{{ t(acceleratedDownload.phase === 'choosing' ? 'desktop.downloads.chooseSaveLocation' : 'desktop.downloads.downloading') }}</p>
                <template v-if="acceleratedDownload.phase === 'downloading'">
                  <progress class="block h-2 w-full overflow-hidden rounded-full accent-primary-600" :value="acceleratedDownload.writtenBytes" :max="acceleratedDownload.totalBytes" :aria-label="t('desktop.downloads.downloadProgress')" />
                  <div class="flex items-center justify-between gap-2 text-xs">
                    <span class="tabular-nums text-gray-500 dark:text-dark-400">{{ downloadProgress }}</span>
                    <button type="button" class="font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300" data-testid="cancel-accelerated-download" @click="cancelAcceleratedDownload">{{ t('desktop.downloads.cancelDownload') }}</button>
                  </div>
                </template>
              </div>
              <p v-if="savedDownloadPlatform === item.platform" class="mt-3 text-xs text-green-700 dark:text-green-400" role="status" data-testid="accelerated-download-complete">{{ t('desktop.downloads.downloadSaved') }}</p>
              <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ item.available ? packageDetails(item) : t('desktop.downloads.unavailableHint') }}</p>
              <p v-if="item.available && item.platform.startsWith('darwin-')" class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400" data-testid="mac-package-install-hint">
                {{ t(item.format === 'dmg' ? 'desktop.downloads.macDmgInstallHint' : 'desktop.downloads.macZipInstallHint') }}
              </p>
            </section>
          </div>
          <p v-if="!loading && !failed && platforms.some(item => item.available)" class="mt-4 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('desktop.downloads.firstInstallHint') }}</p>
        </div>
      </section>

      <section class="card p-6 sm:p-8" aria-labelledby="desktop-install-heading">
        <h2 id="desktop-install-heading" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('desktop.downloads.getStarted') }}</h2>
        <ol class="mt-6 grid gap-6 md:grid-cols-3">
          <li v-for="(step, index) in installSteps" :key="step" class="flex gap-3">
            <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary-50 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ index + 1 }}</span>
            <div>
              <h3 class="text-sm font-semibold leading-7 text-gray-900 dark:text-white">{{ t(`desktop.downloads.${step}Title`) }}</h3>
              <p class="mt-1 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ t(`desktop.downloads.${step}Description`) }}</p>
            </div>
          </li>
        </ol>
      </section>

      <section class="card p-6 sm:p-8" aria-labelledby="desktop-mac-help-heading" data-testid="mac-install-help">
        <div class="flex items-start gap-3">
          <Icon name="infoCircle" size="lg" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-400" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <h2 id="desktop-mac-help-heading" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('desktop.downloads.macHelpTitle') }}</h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('desktop.downloads.macUnsigned') }}</p>
            <div class="mt-5 grid gap-6 lg:grid-cols-[1fr_auto] lg:items-start">
              <div class="min-w-0 space-y-3">
                <p class="text-sm leading-6 text-gray-600 dark:text-gray-400">{{ t('desktop.downloads.macHelperSteps') }}</p>
                <pre class="overflow-x-auto rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-xs leading-6 text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200"><code>bash ~/Downloads/MiloCode-macOS-Helper.command</code></pre>
                <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('desktop.downloads.macHelperScope') }}</p>
              </div>
              <a href="/downloads/MiloCode-macOS-Helper.command" class="btn btn-secondary gap-2" download="MiloCode-macOS-Helper.command" data-testid="mac-helper-download">
                <Icon name="download" size="md" class="shrink-0" aria-hidden="true" />
                {{ t('desktop.downloads.macHelperDownload') }}
              </a>
            </div>
            <p class="mt-5 border-t border-gray-100 pt-4 text-xs leading-6 text-gray-500 dark:border-dark-700 dark:text-dark-400">{{ t('desktop.downloads.macManualAlternative') }}</p>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon, { type IconName } from '@/components/icons/Icon.vue'
import { getDesktopDownloads, type DesktopDownload, type DesktopDownloadPlatform } from '@/api/desktopDownloads'
import { sanitizeUrl } from '@/utils/url'
import { downloadReleaseInRanges, type ReleaseDownloadWriter } from '@/utils/acceleratedReleaseDownload'
import miloCodeIcon from '@/assets/milocode-icon.png'

const { t } = useI18n()
const features: { key: string; icon: IconName }[] = [
  { key: 'accountFeature', icon: 'userCircle' },
  { key: 'modelsFeature', icon: 'sparkles' },
  { key: 'updatesFeature', icon: 'refresh' },
]
const installSteps = ['installStep', 'loginStep', 'projectStep']
const loading = ref(true)
const failed = ref(false)
const downloads = ref<DesktopDownload[]>([])
interface SaveFileHandle {
  createWritable(): Promise<ReleaseDownloadWriter>
}
interface DownloadWindow extends Window {
  showSaveFilePicker?: (options: { suggestedName: string }) => Promise<SaveFileHandle>
}
const acceleratedSupported = window.isSecureContext && typeof (window as DownloadWindow).showSaveFilePicker === 'function'
const acceleratedDownload = ref<{
  platform: DesktopDownloadPlatform
  phase: 'choosing' | 'downloading'
  writtenBytes: number
  totalBytes: number
} | null>(null)
const savedDownloadPlatform = ref<DesktopDownloadPlatform | null>(null)
let downloadController: AbortController | null = null
let active = true
const platforms = computed(() => (['win32-x64', 'darwin-arm64', 'darwin-x64'] as DesktopDownloadPlatform[]).map(platform => {
  const item = downloads.value.find(value => value.platform === platform)
  const url = item?.url?.startsWith('https://') ? sanitizeUrl(item.url) : ''
  return { ...item, platform, url, available: Boolean(item?.available && item.version && url) }
}))

function platformArchitecture(platform: DesktopDownloadPlatform): string {
  return platform === 'win32-x64' ? 'x64' : platform === 'darwin-arm64' ? 'Apple Silicon · ARM64' : 'Intel · x64'
}

function canAccelerateDownload(item: DesktopDownload): boolean {
  return Boolean(acceleratedSupported && item.size && Number.isSafeInteger(item.size) && item.size > 0)
}
const downloadProgress = computed(() => {
  if (!acceleratedDownload.value) return ''
  const { writtenBytes, totalBytes } = acceleratedDownload.value
  return `${(writtenBytes / totalBytes * 100).toFixed(0)}% · ${(writtenBytes / 1024 / 1024).toFixed(1)} / ${(totalBytes / 1024 / 1024).toFixed(1)} MB`
})

function ordinaryDownload(url: string) {
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = ''
  anchor.rel = 'noopener noreferrer'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
}

function packageFilename(item: DesktopDownload): string {
  const filename = item.url ? new URL(item.url).pathname.split('/').pop() : ''
  return filename || `MiloCode-${item.version}-${item.platform}.${item.format || 'bin'}`
}

async function startAcceleratedDownload(item: DesktopDownload) {
  if (acceleratedDownload.value || !item.url || !item.size || !Number.isSafeInteger(item.size) || item.size <= 0) return
  const picker = (window as DownloadWindow).showSaveFilePicker
  if (!acceleratedSupported || !picker) return

  const controller = new AbortController()
  downloadController = controller
  savedDownloadPlatform.value = null
  acceleratedDownload.value = { platform: item.platform, phase: 'choosing', writtenBytes: 0, totalBytes: item.size }
  try {
    // Invoke the picker before any await so browsers retain the user's activation.
    const handle = await picker.call(window, { suggestedName: packageFilename(item) })
    if (!active || controller.signal.aborted) return
    const writer = await handle.createWritable()
    if (!active || controller.signal.aborted) {
      await writer.abort()
      return
    }
    acceleratedDownload.value = { platform: item.platform, phase: 'downloading', writtenBytes: 0, totalBytes: item.size }
    await downloadReleaseInRanges({
      url: item.url,
      size: item.size,
      writer,
      signal: controller.signal,
      onProgress: progress => {
        if (active && acceleratedDownload.value) Object.assign(acceleratedDownload.value, progress)
      },
    })
    if (active && !controller.signal.aborted) savedDownloadPlatform.value = item.platform
  } catch (error) {
    const userCancelled = controller.signal.aborted || (error instanceof DOMException && error.name === 'AbortError')
    if (active && !userCancelled) ordinaryDownload(item.url)
  } finally {
    if (downloadController === controller) {
      downloadController = null
      acceleratedDownload.value = null
    }
  }
}

function cancelAcceleratedDownload() {
  downloadController?.abort(new DOMException('The download was cancelled.', 'AbortError'))
}

function packageDetails(item: DesktopDownload): string {
  const details = [item.format?.toUpperCase()]
  if (item.size && Number.isFinite(item.size) && item.size > 0) details.push(`${(item.size / 1024 / 1024).toFixed(1)} MB`)
  return details.filter(Boolean).join(' · ')
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
onBeforeUnmount(() => { active = false; cancelAcceleratedDownload() })
</script>
