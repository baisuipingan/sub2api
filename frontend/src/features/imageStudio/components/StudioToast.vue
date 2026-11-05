<template><span v-if="false" /></template>

<script setup lang="ts">
import { watch } from 'vue'
import { useAppStore } from '@/stores/app'
import { storeState as state } from '../stores/store'

// App.vue owns the shared Toast renderer; forward studio notifications to it.
const app = useAppStore()
watch(() => state.toast, (toast) => {
  if (toast) app.showToast(toast.type, toast.message, toast.type === 'error' ? 6000 : 3000)
}, { immediate: true })
</script>
