<template>
  <div class="flex items-start gap-2">
    <Select
      :key="selectionKey"
      class="min-w-0 flex-1"
      :model-value="selectedModelId"
      :options="modelOptions"
      :placeholder="placeholder"
      :disabled="disabled || refreshing"
      :searchable="true"
      :search-placeholder="t('imageStudio.modelSelect.searchPlaceholder')"
      :empty-text="t('imageStudio.modelSelect.noMatch')"
      :aria-label="t('imageStudio.modelSelect.ariaLabel', { name: selectedModel?.id ?? placeholder })"
      @update:model-value="chooseModel"
    />
    <button
      type="button"
      data-testid="refresh-models"
      class="btn btn-secondary btn-icon shrink-0"
      :disabled="refreshDisabled || refreshing"
      :aria-label="refreshLabel"
      :title="refreshLabel"
      :aria-busy="refreshing"
      @click="refreshModels"
    >
      <StudioIcon name="RefreshIcon" class="h-4 w-4" :class="refreshing ? 'animate-spin motion-reduce:animate-none' : ''" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { ImageModelDefinition } from '../lib/modelRegistry'
import StudioIcon from './StudioIcon.vue'

const props = withDefaults(defineProps<{
  models: ImageModelDefinition[]
  selectedModelId: string
  disabled: boolean
  placeholder: string
  refreshing?: boolean
  refreshDisabled?: boolean
}>(), { refreshing: false, refreshDisabled: false })

const emit = defineEmits<{ change: [modelId: string]; refresh: [] }>()
const { t } = useI18n()
const selectionKey = ref(0)
const modelOptions = computed(() => props.models.map((model) => ({ value: model.id, label: model.id })))
const selectedModel = computed(() => props.models.find((model) => model.id === props.selectedModelId))
const refreshLabel = computed(() => t(props.refreshing ? 'imageStudio.modelSelect.refreshing' : 'imageStudio.modelSelect.refresh'))

function refreshModels() {
  if (props.refreshing || props.refreshDisabled) return
  // Recreate the field to close its teleported dropdown and clear its search.
  selectionKey.value += 1
  emit('refresh')
}

function chooseModel(value: string | number | boolean | null) {
  if (typeof value === 'string' && props.models.some((model) => model.id === value)) emit('change', value)
}
</script>
