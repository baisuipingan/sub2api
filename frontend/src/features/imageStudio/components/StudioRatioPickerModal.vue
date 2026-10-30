<template>
  <div
    class="fixed inset-0 z-[70] flex items-center justify-center bg-black/35 p-4"
    @click="emit('close')"
  >
    <div
      role="dialog"
      aria-modal="true"
      class="relative w-full max-w-md rounded-2xl border border-gray-200 bg-white p-5 shadow-2xl dark:border-white/[0.1] dark:bg-dark-900"
      @click.stop
    >
      <div class="flex items-start justify-between gap-4">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('imageStudio.ratio.title') }}
          </h2>
          <p class="mt-1 text-xs text-gray-400">
            {{ t('imageStudio.ratio.current', { ratio: currentRatio || t('imageStudio.ratio.byModel') }) }}
          </p>
        </div>
        <button
          type="button"
          class="rounded-lg p-2 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-white/[0.06]"
          :aria-label="t('imageStudio.common.close')"
          :title="t('imageStudio.common.close')"
          @click="emit('close')"
        >
          <StudioIcon name="CloseIcon" class="h-5 w-5" />
        </button>
      </div>

      <div class="mt-5 space-y-4">
        <div v-if="ratios.length > 0" class="grid grid-cols-3 gap-2">
          <button
            v-for="ratio in ratios"
            :key="ratio"
            type="button"
            class="rounded-lg border px-3 py-2 text-sm"
            :class="
              !customSelected && selectedRatio === ratio
                ? 'border-primary-500 bg-primary-50 text-primary-600 dark:bg-primary-500/10'
                : 'border-gray-200 text-gray-600 dark:border-white/[0.1] dark:text-dark-300'
            "
            @click="choosePreset(ratio)"
          >
            {{ ratio }}
          </button>
        </div>

        <div v-if="allowCustom" class="space-y-3">
          <button
            type="button"
            class="w-full rounded-lg border px-3 py-2 text-sm"
            :class="
              customSelected
                ? 'border-primary-500 bg-primary-50 text-primary-600 dark:bg-primary-500/10'
                : 'border-gray-200 text-gray-600 dark:border-white/[0.1] dark:text-dark-300'
            "
            @click="customSelected = true"
          >
            {{ t('imageStudio.ratio.custom') }}
          </button>

          <label v-if="customSelected" class="block">
            <span class="mb-1.5 block text-xs text-gray-500">
              {{ t('imageStudio.ratio.ratioLabel') }}
            </span>
            <input
              v-model="customRatio"
              :placeholder="t('imageStudio.ratio.placeholder')"
              class="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-primary-500 dark:border-white/[0.1] dark:bg-white/[0.04] dark:text-white"
            />
            <span v-if="!activeCustomRatio" class="mt-1 block text-xs text-red-500">
              {{ t('imageStudio.ratio.invalid') }}
            </span>
          </label>
        </div>
      </div>

      <div class="mt-5 flex justify-end border-t border-gray-100 pt-4 dark:border-white/[0.08]">
        <button
          type="button"
          class="rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="!canApply"
          @click="choose"
        >
          {{ t('imageStudio.ratio.apply') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../composables/usePreventBackgroundScroll'
import { parseRatio } from '../lib/size'

const props = defineProps<{
  currentRatio: string
  ratios: string[]
  allPresetRatios: string[]
  allowCustom: boolean
}>()

const emit = defineEmits<{
  select: [ratio: string]
  close: []
}>()

const { t } = useI18n()

const currentIsCustom = computed(
  () => Boolean(props.currentRatio) && !props.allPresetRatios.includes(props.currentRatio),
)
const selectedRatio = ref(props.ratios.includes(props.currentRatio) ? props.currentRatio : '')
const customSelected = ref(currentIsCustom.value)
const customRatio = ref(currentIsCustom.value ? props.currentRatio : '5:4')

useCloseOnEscape(() => true, () => emit('close'))
usePreventBackgroundScroll(() => true)

const activeCustomRatio = computed(() => parseRatio(customRatio.value))
const canApply = computed(() =>
  customSelected.value ? Boolean(activeCustomRatio.value) : Boolean(selectedRatio.value),
)

function choosePreset(ratio: string) {
  selectedRatio.value = ratio
  customSelected.value = false
}

function choose() {
  if (!canApply.value) return
  emit('select', customSelected.value ? customRatio.value : selectedRatio.value)
  emit('close')
}
</script>
