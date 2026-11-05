<template>
  <BaseDialog
    :show="true"
    :title="t('imageStudio.ratio.title')"
    width="narrow"
    :z-index="70"
    :close-on-escape="false"
    close-on-click-outside
    @close="emit('close')"
  >
    <p class="mb-5 text-sm text-gray-500 dark:text-dark-400">
      {{ t('imageStudio.ratio.current', { ratio: currentRatio || t('imageStudio.ratio.byModel') }) }}
    </p>
    <div class="space-y-4">
      <div v-if="ratios.length > 0" class="grid grid-cols-3 gap-2">
        <button
          v-for="ratio in ratios"
          :key="ratio"
          type="button"
          class="btn btn-sm"
          :class="!customSelected && selectedRatio === ratio ? 'btn-primary' : 'btn-secondary'"
          :aria-pressed="!customSelected && selectedRatio === ratio"
          @click="choosePreset(ratio)"
        >
          {{ ratio }}
        </button>
      </div>
      <div v-if="allowCustom" class="space-y-3">
        <button
          type="button"
          class="btn btn-sm w-full"
          :class="customSelected ? 'btn-primary' : 'btn-secondary'"
          :aria-pressed="customSelected"
          @click="customSelected = true"
        >
          {{ t('imageStudio.ratio.custom') }}
        </button>
        <Input
          v-if="customSelected"
          :id="customRatioId"
          v-model="customRatio"
          :label="t('imageStudio.ratio.ratioLabel')"
          :placeholder="t('imageStudio.ratio.placeholder')"
          :error="!activeCustomRatio ? t('imageStudio.ratio.invalid') : undefined"
          @enter="choose"
        />
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" :disabled="!canApply" @click="choose">
          {{ t('imageStudio.ratio.apply') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'
import { parseRatio } from '../lib/size'

const props = defineProps<{
  currentRatio: string
  ratios: string[]
  allPresetRatios: string[]
  allowCustom: boolean
}>()
const emit = defineEmits<{ select: [ratio: string]; close: [] }>()
const { t } = useI18n()
const customRatioId = `image-studio-ratio-${useId()}`
const currentIsCustom = computed(() => Boolean(props.currentRatio) && !props.allPresetRatios.includes(props.currentRatio))
const selectedRatio = ref(props.ratios.includes(props.currentRatio) ? props.currentRatio : '')
const customSelected = ref(currentIsCustom.value && props.allowCustom)
const customRatio = ref(currentIsCustom.value ? props.currentRatio : '5:4')
useCloseOnEscape(() => true, () => emit('close'))
const activeCustomRatio = computed(() => parseRatio(customRatio.value))
const canApply = computed(() => customSelected.value ? Boolean(activeCustomRatio.value) : Boolean(selectedRatio.value))

function choosePreset(ratio: string) {
  selectedRatio.value = ratio
  customSelected.value = false
}
function choose() {
  if (!canApply.value) return
  emit('select', customSelected.value ? customRatio.value.trim() : selectedRatio.value)
  emit('close')
}
</script>
