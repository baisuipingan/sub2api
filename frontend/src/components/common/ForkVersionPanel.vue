<template>
  <div class="relative">
    <button
      type="button"
      data-testid="fork-version-trigger"
      class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
      :class="
        hasUpdate
          ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400 dark:hover:bg-amber-900/50'
          : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400 dark:hover:bg-dark-700'
      "
      :title="triggerTitle"
      @click="dropdownOpen = !dropdownOpen"
    >
      <span v-if="currentVersion" class="font-medium">v{{ currentVersion }}</span>
      <span v-else class="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-dark-600"></span>
      <span v-if="hasUpdate" class="relative flex h-2 w-2">
        <span
          class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"
        ></span>
        <span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
      </span>
    </button>

    <transition name="fork-dropdown">
      <div
        v-if="dropdownOpen"
        data-testid="fork-version-panel"
        class="absolute left-0 z-50 mt-2 w-80 overflow-hidden whitespace-normal rounded-xl border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
      >
        <div
          class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"
        >
          <span class="text-sm font-medium text-gray-700 dark:text-dark-300">
            {{ t('version.forkCurrentVersion') }}
          </span>
          <button
            type="button"
            class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-dark-200"
            :disabled="loading"
            :title="t('version.refresh')"
            @click="refreshVersion"
          >
            <Icon name="refresh" size="sm" :stroke-width="2" :class="{ 'animate-spin': loading }" />
          </button>
        </div>

        <div v-if="loading" class="flex items-center justify-center py-8">
          <svg class="h-6 w-6 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
        </div>

        <div v-else class="space-y-3 p-4">
          <div class="text-center">
            <div class="inline-flex items-center gap-2">
              <span v-if="currentVersion" class="text-2xl font-bold text-gray-900 dark:text-white">
                v{{ currentVersion }}
              </span>
              <span v-else class="text-2xl font-bold text-gray-400 dark:text-dark-500">--</span>
              <span
                v-if="statusHealthy"
                data-testid="fork-version-synced"
                class="flex h-5 w-5 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30"
              >
                <svg
                  class="h-3 w-3 text-green-600 dark:text-green-400"
                  fill="currentColor"
                  viewBox="0 0 20 20"
                >
                  <path
                    fill-rule="evenodd"
                    d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                    clip-rule="evenodd"
                  />
                </svg>
              </span>
            </div>
            <p
              class="mt-1 text-xs"
              :class="
                hasUpdate
                  ? 'text-amber-600 dark:text-amber-400'
                  : statusHealthy
                    ? 'text-green-600 dark:text-green-400'
                    : 'text-gray-500 dark:text-dark-400'
              "
            >
              {{
                hasUpdate
                  ? t('version.forkOfficialUpdateAvailable')
                  : statusHealthy
                    ? t('version.forkBaselineSynced')
                    : t('version.forkVersionUnknown')
              }}
            </p>
          </div>

          <dl class="space-y-2 rounded-lg border border-gray-100 p-3 text-xs dark:border-dark-700">
            <div class="flex items-start justify-between gap-3">
              <dt class="text-gray-500 dark:text-dark-400">{{ t('version.forkOfficialBaseline') }}</dt>
              <dd class="font-mono text-gray-700 dark:text-dark-200">
                {{ upstreamVersion ? `v${upstreamVersion}` : '--' }}
              </dd>
            </div>
            <div class="flex items-start justify-between gap-3">
              <dt class="text-gray-500 dark:text-dark-400">{{ t('version.forkOfficialLatest') }}</dt>
              <dd class="font-mono text-gray-700 dark:text-dark-200">
                {{ latestVersion ? `v${latestVersion}` : '--' }}
              </dd>
            </div>
          </dl>

          <p
            v-if="versionWarning"
            data-testid="fork-version-warning"
            class="rounded-lg border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800/50 dark:bg-red-900/20 dark:text-red-400"
          >
            {{ versionWarning }}
          </p>

          <a
            v-if="hasUpdate && releaseInfo?.html_url"
            :href="releaseInfo.html_url"
            target="_blank"
            rel="noopener noreferrer"
            class="group flex items-center justify-between gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800/50 dark:bg-amber-900/20"
          >
            <div>
              <p class="text-sm font-medium text-amber-700 dark:text-amber-300">
                {{ t('version.releaseNotes') }}
              </p>
              <p class="mt-1 text-xs text-amber-600/80 dark:text-amber-400/80">
                {{ releaseInfo.name || `v${latestVersion}` }}
              </p>
            </div>
            <Icon
              name="arrowRight"
              size="sm"
              :stroke-width="2"
              class="text-amber-500 transition-transform group-hover:translate-x-0.5"
            />
          </a>

          <button
            v-if="hasUpdate"
            type="button"
            disabled
            data-testid="fork-update-disabled"
            class="flex w-full cursor-not-allowed items-center justify-center gap-2 rounded-lg bg-gray-200 px-4 py-2 text-sm font-medium text-gray-500 opacity-80 dark:bg-dark-700 dark:text-dark-400"
            :title="t('version.forkExternalManagedHint')"
          >
            {{ t('version.forkUpdateDisabled') }}
          </button>

          <div
            class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-xs text-blue-700 dark:border-blue-800/50 dark:bg-blue-900/20 dark:text-blue-300"
          >
            <p class="font-medium">{{ t('version.forkExternalManaged') }}</p>
            <p class="mt-1 leading-5">{{ t('version.forkExternalManagedHint') }}</p>
            <code
              class="mt-2 block select-all whitespace-pre-wrap break-all rounded bg-white/70 p-2 font-mono text-[10px] leading-4 text-blue-800 dark:bg-dark-900/50 dark:text-blue-200"
            >./scripts/fork-release.sh status
./scripts/fork-release.sh prepare v{{ latestVersion || upstreamVersion || '<tag>' }}
./scripts/fork-release.sh publish v{{ latestVersion || upstreamVersion || '<tag>' }}</code>
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores'

const props = defineProps<{
  version?: string
}>()

const { t } = useI18n()
const appStore = useAppStore()
const dropdownOpen = ref(false)

const loading = computed(() => appStore.versionLoading)
const currentVersion = computed(() => appStore.currentVersion || props.version || '')
const upstreamVersion = computed(() => appStore.upstreamVersion)
const latestVersion = computed(() => appStore.latestVersion)
const hasUpdate = computed(() => appStore.hasUpdate)
const versionWarning = computed(() => appStore.versionWarning)
const releaseInfo = computed(() => appStore.releaseInfo)
const statusHealthy = computed(
  () => !hasUpdate.value && !versionWarning.value && Boolean(upstreamVersion.value)
)

const triggerTitle = computed(() => {
  if (hasUpdate.value) return t('version.forkOfficialUpdateAvailable')
  if (statusHealthy.value) return t('version.forkBaselineSynced')
  return t('version.forkVersionUnknown')
})

async function refreshVersion() {
  await appStore.fetchVersion(true)
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target as Element | null
  if (
    !target?.closest('[data-testid="fork-version-trigger"]') &&
    !target?.closest('[data-testid="fork-version-panel"]')
  ) {
    dropdownOpen.value = false
  }
}

onMounted(() => {
  void appStore.fetchVersion(false)
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.fork-dropdown-enter-active,
.fork-dropdown-leave-active {
  transition: all 0.2s ease;
}

.fork-dropdown-enter-from,
.fork-dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
