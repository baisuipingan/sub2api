<template>
  <div
    v-if="state.confirmDialog"
    class="fixed inset-0 z-[110] flex items-center justify-center bg-black/35 p-4"
    @click="close"
  >
    <div
      role="dialog"
      aria-modal="true"
      class="relative w-full max-w-sm rounded-2xl border border-gray-200 bg-white p-5 shadow-2xl dark:border-white/[0.1] dark:bg-dark-900"
      @click.stop
    >
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">
        {{ state.confirmDialog.title }}
      </h2>
      <p class="mt-2 whitespace-pre-line text-sm leading-6 text-gray-600 dark:text-dark-300">
        {{ state.confirmDialog.message }}
      </p>
      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          class="rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 transition hover:bg-gray-50 dark:border-white/[0.1] dark:text-dark-300 dark:hover:bg-white/[0.06]"
          @click="close"
        >
          {{ state.confirmDialog.cancelText || t('imageStudio.common.cancel') }}
        </button>
        <button
          type="button"
          class="rounded-lg px-3 py-2 text-sm font-medium text-white transition"
          :class="
            state.confirmDialog.tone === 'danger'
              ? 'bg-red-600 hover:bg-red-700'
              : 'bg-primary-600 hover:bg-primary-700'
          "
          @click="confirm"
        >
          {{ state.confirmDialog.confirmText || t('imageStudio.common.confirm') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeState as state } from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'

const { t } = useI18n()

const open = computed(() => Boolean(state.confirmDialog))

useCloseOnEscape(() => open.value, () => close())
usePreventBackgroundScroll(() => open.value)

function close() {
  state.setConfirmDialog(null)
}

function confirm() {
  void Promise.resolve(state.confirmDialog?.action?.()).finally(() => close())
}
</script>
