<template>
  <div
    v-if="open"
    class="fixed inset-0 z-[100] flex items-center justify-center bg-black/35 p-4 backdrop-blur-sm"
    @click="state.setShowSettings(false)"
  >
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="settings-title"
      class="flex max-h-[90vh] w-full max-w-lg flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-2xl dark:border-white/[0.1] dark:bg-dark-900"
      @click.stop
    >
      <div
        class="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-white/[0.08]"
      >
        <div>
          <h2 id="settings-title" class="text-lg font-semibold text-gray-950 dark:text-white">
            {{ t('imageStudio.settings.title') }}
          </h2>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
            {{ t('imageStudio.settings.subtitle') }}
          </p>
        </div>
        <button
          type="button"
          class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-white/[0.06] dark:hover:text-dark-200"
          :aria-label="t('imageStudio.common.close')"
          @click="state.setShowSettings(false)"
        >
          <StudioIcon name="CloseIcon" class="h-5 w-5" />
        </button>
      </div>

      <div class="min-h-0 flex-1 space-y-6 overflow-y-auto p-5">
        <!-- 连接状态：仅展示，不再允许手动配置 API Key -->
        <section class="rounded-xl border border-gray-200 p-4 dark:border-white/[0.1]">
          <div class="flex items-center justify-between gap-4">
            <div>
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('imageStudio.settings.connectionTitle') }}
              </h3>
              <p
                class="mt-1 text-sm"
                :class="
                  connected
                    ? 'text-emerald-600 dark:text-emerald-400'
                    : 'text-gray-500 dark:text-dark-400'
                "
              >
                {{ connectionText }}
              </p>
            </div>
            <!-- 密钥由系统下发，用户不能手填；这里提供「重新获取」，
                 用于密钥失效/切换分组后自救（原来指向已删除的连接弹窗） -->
            <button
              type="button"
              class="shrink-0 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 transition hover:bg-gray-50 disabled:opacity-50 dark:border-white/[0.1] dark:text-dark-200 dark:hover:bg-white/[0.06]"
              :disabled="refreshingKey"
              @click="handleRefreshKey"
            >
              {{ refreshingKey ? t('imageStudio.settings.refreshingKey') : t('imageStudio.settings.refreshKey') }}
            </button>
          </div>
        </section>

        <!-- 创作偏好 -->
        <section>
          <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('imageStudio.settings.preferences') }}
          </h3>
          <div
            class="divide-y divide-gray-100 rounded-xl border border-gray-200 dark:divide-white/[0.08] dark:border-white/[0.1]"
          >
            <label
              v-for="item in preferences"
              :key="item.key"
              class="flex items-center justify-between gap-4 px-4 py-3 text-sm text-gray-700 dark:text-dark-200"
            >
              <span>{{ item.label }}</span>
              <StudioCheckbox
                :checked="state.settings[item.key]"
                :label="''"
                @change="toggle(item.key)"
              />
            </label>
          </div>
        </section>

        <!-- 本地数据 -->
        <section>
          <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('imageStudio.settings.localData') }}
          </h3>
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 dark:border-white/[0.1] dark:text-dark-200 dark:hover:bg-white/[0.06]"
              @click="exportData({ exportConfig: true, exportTasks: true })"
            >
              <StudioIcon name="ExportIcon" class="h-4 w-4" />{{ t('imageStudio.settings.exportBackup') }}
            </button>
            <button
              type="button"
              class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-white/[0.1] dark:text-dark-200 dark:hover:bg-white/[0.06]"
              :disabled="importing"
              @click="fileRef?.click()"
            >
              <StudioIcon name="ImportIcon" class="h-4 w-4" />{{
                importing ? t('imageStudio.settings.importing') : t('imageStudio.settings.importBackup')
              }}
            </button>
            <button
              type="button"
              class="rounded-lg border border-red-200 px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:border-red-500/30 dark:text-red-400 dark:hover:bg-red-500/10"
              @click="clearData({ clearConfig: false, clearTasks: true })"
            >
              {{ t('imageStudio.settings.clearHistory') }}
            </button>
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
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import StudioCheckbox from './StudioCheckbox.vue'
import { storeState as state, clearData, exportData, importData } from '../stores/store'
import { ensureStudioApiKey, resetStudioApiKeyCache } from '../lib/studioAuth'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'
import { formatMiloRouterBalance } from '../lib/miloRouterDisplay'

const { t } = useI18n()

const refreshingKey = ref(false)

/** 重新向后端取一次密钥（换过分组、密钥被停用时用） */
async function handleRefreshKey() {
  if (refreshingKey.value) return
  refreshingKey.value = true
  try {
    resetStudioApiKeyCache()
    const key = await ensureStudioApiKey()
    state.showToast(
      key ? t('imageStudio.settings.refreshKeySuccess') : t('imageStudio.settings.refreshKeyFailed'),
      key ? 'success' : 'error',
    )
  } finally {
    refreshingKey.value = false
  }
}

const importing = ref(false)
const fileRef = ref<HTMLInputElement | null>(null)

const open = computed(() => state.showSettings)

useCloseOnEscape(() => open.value, () => state.setShowSettings(false))
usePreventBackgroundScroll(() => open.value)

const connected = computed(() => state.settings.miloRouter.connectionStatus === 'connected')

const connectionText = computed(() => {
  if (!connected.value) return t('imageStudio.settings.notConnected')
  const balance = formatMiloRouterBalance(state.settings.miloRouter.billing?.balance)
  return balance !== undefined
    ? t('imageStudio.settings.connectedWithBalance', { balance })
    : t('imageStudio.settings.connected')
})

type PrefKey = 'clearInputAfterSubmit' | 'enterSubmit' | 'taskCompletionNotification'

const preferences = computed<Array<{ key: PrefKey; label: string }>>(() => [
  { key: 'clearInputAfterSubmit', label: t('imageStudio.settings.prefClearInput') },
  { key: 'enterSubmit', label: t('imageStudio.settings.prefEnterSubmit') },
  { key: 'taskCompletionNotification', label: t('imageStudio.settings.prefNotify') },
])

function toggle(key: PrefKey) {
  state.setSettings({ [key]: !state.settings[key] })
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
</script>
