<template>
  <div
    v-if="state.toast"
    class="pointer-events-none fixed bottom-24 left-1/2 z-[120]"
  >
    <div
      class="flex w-max max-w-[calc(100vw-32px)] items-center gap-2.5 rounded-full border border-gray-200/60 bg-white/95 px-5 py-3.5 text-sm font-medium text-gray-700 shadow-[0_8px_30px_rgb(0,0,0,0.12)] ring-1 ring-black/5 backdrop-blur-xl dark:border-white/[0.08] dark:bg-dark-900/95 dark:text-dark-200 dark:ring-white/10 sm:max-w-[min(44rem,80vw)]"
    >
      <span class="flex-shrink-0">
        <span
          v-if="state.toast.type === 'success'"
          class="flex h-5 w-5 items-center justify-center rounded-full bg-emerald-100 text-emerald-600 dark:bg-emerald-900/50 dark:text-emerald-400"
        >
          <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
          </svg>
        </span>
        <span
          v-else-if="state.toast.type === 'error'"
          class="flex h-5 w-5 items-center justify-center rounded-full bg-red-100 text-red-600 dark:bg-red-900/50 dark:text-red-400"
        >
          <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </span>
        <span
          v-else
          class="flex h-5 w-5 items-center justify-center rounded-full bg-primary-100 text-primary-600 dark:bg-primary-900/50 dark:text-primary-400"
        >
          <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2.5"
              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
        </span>
      </span>
      <span class="whitespace-pre-line text-center leading-5">{{ state.toast.message }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { storeState as state } from '../stores/store'
</script>
