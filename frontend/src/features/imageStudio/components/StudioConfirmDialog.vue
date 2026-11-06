<template>
  <ConfirmDialog
    :show="open"
    :title="state.confirmDialog?.title || ''"
    :message="state.confirmDialog?.message || ''"
    :confirm-text="state.confirmDialog?.confirmText || t('imageStudio.common.confirm')"
    :cancel-text="state.confirmDialog?.cancelText || t('imageStudio.common.cancel')"
    :danger="state.confirmDialog?.tone === 'danger'"
    :close-on-escape="false"
    :z-index="110"
    @cancel="close"
    @confirm="confirm"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { storeState as state } from '../stores/store'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'

const { t } = useI18n()
const open = computed(() => Boolean(state.confirmDialog))
useCloseOnEscape(() => open.value, close)

function close() {
  state.setConfirmDialog(null)
}
function confirm() {
  const dialog = state.confirmDialog
  void Promise.resolve(dialog?.action?.()).finally(() => {
    if (state.confirmDialog === dialog) close()
  })
}
</script>
