<template>
  <section
    :aria-label="t('imageStudio.creation.aria')"
    class="flex min-h-0 flex-col border-b border-gray-200 bg-white dark:border-white/[0.08] dark:bg-dark-950 lg:h-full lg:overflow-hidden lg:border-b-0 lg:border-r"
  >
    <div class="shrink-0 border-b border-gray-100 px-5 py-4 dark:border-white/[0.06]">
      <div class="flex items-center justify-between gap-3">
        <div>
          <p class="text-xs font-medium uppercase tracking-[0.18em] text-primary-600 dark:text-primary-400">
            {{ t('imageStudio.creation.eyebrow') }}
          </p>
          <h2 class="mt-1 text-lg font-semibold text-gray-950 dark:text-white">
            {{ t('imageStudio.creation.title') }}
          </h2>
        </div>
        <span
          class="rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-500 dark:bg-white/[0.06] dark:text-dark-400"
        >
          {{ t('imageStudio.creation.modelCount', { count: state.modelCatalog.length }) }}
        </span>
      </div>

      <div class="mt-4">
        <span class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">
          {{ t('imageStudio.creation.modelLabel') }}
        </span>
        <StudioModelSelect
          :models="state.modelCatalog"
          :selected-model-id="state.settings.selectedModelId"
          :disabled="state.catalogStatus !== 'ready' || !state.modelCatalog.length"
          :placeholder="modelPlaceholder"
          :refreshing="state.catalogStatus === 'loading'"
          :refresh-disabled="!state.settings.miloRouter.apiKey.trim()"
          @change="state.selectModel"
          @refresh="state.refreshImageModelCatalog()"
        />
      </div>

      <div
        v-if="state.catalogStatus === 'error'"
        class="mt-3 flex items-center justify-between gap-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600 dark:bg-red-500/10 dark:text-red-300"
      >
        <span>{{ state.catalogError }}</span>
        <button
          type="button"
          class="shrink-0 font-medium hover:underline"
          @click="state.refreshImageModelCatalog()"
        >
          {{ t('imageStudio.creation.refetch') }}
        </button>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto px-4 py-4 lg:overflow-hidden">
      <StudioInputBar embedded />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioModelSelect from './StudioModelSelect.vue'
import StudioInputBar from './StudioInputBar.vue'
import { storeState as state } from '../stores/store'

const { t } = useI18n()

const connected = computed(() => state.settings.miloRouter.connectionStatus === 'connected')

const modelPlaceholder = computed(() => {
  if (state.catalogStatus === 'loading') return t('imageStudio.input.loadingModels')
  return connected.value
    ? t('imageStudio.creation.noModel')
    : t('imageStudio.creation.connectFirst')
})
</script>
