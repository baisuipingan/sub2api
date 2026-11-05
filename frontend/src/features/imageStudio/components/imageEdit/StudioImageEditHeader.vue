<template>
  <div
    class="flex flex-none items-center justify-between gap-3 border-b border-gray-200 bg-white px-4 py-3 dark:border-dark-800 dark:bg-dark-950 sm:px-6 sm:py-3.5"
  >
    <div class="flex min-w-0 items-center gap-3">
      <button
        type="button"
        class="btn btn-sm btn-ghost -ml-2 rounded-xl p-2 text-gray-500 transition hover:bg-gray-100 disabled:opacity-40 dark:text-dark-400 dark:hover:bg-dark-800"
        :disabled="busy"
        :aria-label="t('imageStudio.edit.cancelAria')"
        :title="t('imageStudio.common.cancel')"
        @click="emit('close')"
      >
        <svg class="h-5 w-5 sm:h-6 sm:w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18 18 6M6 6l12 12" />
        </svg>
      </button>
      <div class="min-w-0">
        <h2 id="image-edit-title" class="truncate text-sm font-semibold text-gray-900 dark:text-white sm:text-base">
          {{ t('imageStudio.edit.title') }}
        </h2>
        <p class="hidden text-xs text-gray-500 dark:text-dark-400 sm:block">
          {{ t('imageStudio.edit.subtitle') }}
        </p>
      </div>
    </div>

    <button
      v-if="hasSavedMask"
      type="button"
      class="btn btn-sm btn-ghost shrink-0 rounded-xl bg-gray-100 px-3.5 py-2 text-xs font-medium text-gray-700 transition hover:bg-red-50 hover:text-red-600 disabled:opacity-40 dark:bg-white/[0.08] dark:text-dark-300 dark:hover:bg-red-500/20 dark:hover:text-red-400 sm:text-sm"
      :disabled="busy"
      @click="emit('removeMask')"
    >
      {{ t('imageStudio.edit.removeMask') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  busy: boolean
  hasSavedMask: boolean
}>()

const emit = defineEmits<{ close: []; removeMask: [] }>()
const { t } = useI18n()
</script>
