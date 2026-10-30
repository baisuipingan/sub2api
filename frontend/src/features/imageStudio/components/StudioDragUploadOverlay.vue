<template>
  <div
    v-if="visible"
    class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-2xl border-2 border-dashed border-primary-400 bg-primary-50/85 backdrop-blur-sm dark:border-primary-500/60 dark:bg-primary-950/70"
  >
    <div class="text-center">
      <StudioIcon name="ImageUploadIcon" class="mx-auto h-8 w-8 text-primary-500" />
      <p class="mt-2 text-sm font-medium text-primary-700 dark:text-primary-300">
        {{ atImageLimit ? t('imageStudio.drag.limitReached') : t('imageStudio.drag.dropHere') }}
      </p>
      <p v-if="!atImageLimit && maxImages > 0" class="mt-1 text-xs text-primary-600/80 dark:text-primary-400/80">
        {{ t('imageStudio.drag.maxHint', { max: maxImages }) }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'

defineProps<{
  visible: boolean
  atImageLimit: boolean
  maxImages: number
}>()

const { t } = useI18n()
</script>
