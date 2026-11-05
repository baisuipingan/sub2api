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
              @update:model-value="setPreference(item.key, $event)"
            />
          </div>
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
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import { storeState as state, clearData, exportData, importData } from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'

const { t } = useI18n()

const importing = ref(false)
const fileRef = ref<HTMLInputElement | null>(null)

const open = computed(() => state.showSettings)

useCloseOnEscape(() => open.value, () => state.setShowSettings(false))

type PrefKey = 'clearInputAfterSubmit' | 'taskCompletionNotification'

const preferences = computed<Array<{ key: PrefKey; label: string }>>(() => [
  { key: 'clearInputAfterSubmit', label: t('imageStudio.settings.prefClearInput') },
  { key: 'taskCompletionNotification', label: t('imageStudio.settings.prefNotify') },
])

function setPreference(key: PrefKey, value: boolean) {
  state.setSettings({ [key]: value })
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
