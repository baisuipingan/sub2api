<template>
  <div
    class="border-t border-gray-200 pt-4 dark:border-dark-600"
    data-testid="image-size-capability-section"
  >
    <div class="mb-3">
      <label class="input-label mb-0">{{ t('admin.accounts.imageSizeCapability.title') }}</label>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.imageSizeCapability.hint') }}
      </p>
    </div>

    <div v-if="modelValue.length > 0" class="mb-3 space-y-3">
      <div
        v-for="(row, index) in modelValue"
        :key="row.rowId"
        class="rounded-xl border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-600 dark:bg-dark-800/40"
        data-testid="image-size-capability-row"
      >
        <div class="mb-3 flex items-center justify-between gap-2">
          <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
            {{ t('admin.accounts.imageSizeCapability.ruleLabel', { index: index + 1 }) }}
          </span>
          <button
            type="button"
            class="flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
            :aria-label="t('admin.accounts.imageSizeCapability.remove')"
            @click="emit('remove', index)"
          >
            <Icon name="trash" size="sm" aria-hidden="true" />
            {{ t('admin.accounts.imageSizeCapability.remove') }}
          </button>
        </div>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_7rem] lg:grid-cols-[minmax(0,1fr)_7rem_minmax(0,1fr)]">
          <div class="min-w-0">
            <label :for="fieldId('model', row.rowId)" class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">
              {{ t('admin.accounts.imageSizeCapability.modelColumn') }}
            </label>
            <input
              :id="fieldId('model', row.rowId)"
              :value="row.model"
              @input="updateRow(row.rowId, 'model', ($event.target as HTMLInputElement).value)"
              type="text"
              :list="modelListId"
              data-testid="image-size-model"
              class="input w-full min-w-0 font-mono text-sm"
              :placeholder="t('admin.accounts.imageSizeCapability.modelPlaceholder')"
              :aria-invalid="validationAttempted && errors.has(row.rowId) || undefined"
              :aria-describedby="validationAttempted && errors.has(row.rowId) ? fieldId('error', row.rowId) : undefined"
            />
          </div>
          <div class="min-w-0">
            <label :for="fieldId('tier', row.rowId)" class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">
              {{ t('admin.accounts.imageSizeCapability.tierColumn') }}
            </label>
            <Select
              :id="fieldId('tier', row.rowId)"
              :model-value="row.tier"
              @update:model-value="updateRow(row.rowId, 'tier', String($event ?? ''))"
              :options="imageSizeTierOptions"
              :searchable="false"
              :error="validationAttempted && errors.has(row.rowId)"
              :aria-label="t('admin.accounts.imageSizeCapability.tierColumn')"
              :aria-describedby="validationAttempted && errors.has(row.rowId) ? fieldId('error', row.rowId) : undefined"
            />
          </div>
          <div class="min-w-0 sm:col-span-2 lg:col-span-1">
            <label :for="fieldId('upstream', row.rowId)" class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">
              {{ t('admin.accounts.imageSizeCapability.upstreamColumn') }}
            </label>
            <input
              :id="fieldId('upstream', row.rowId)"
              :value="row.upstream"
              @input="updateRow(row.rowId, 'upstream', ($event.target as HTMLInputElement).value)"
              type="text"
              class="input w-full min-w-0 font-mono text-sm"
              :placeholder="t('admin.accounts.imageSizeCapability.upstreamPlaceholder')"
              :aria-invalid="validationAttempted && errors.has(row.rowId) || undefined"
              :aria-describedby="validationAttempted && errors.has(row.rowId) ? fieldId('error', row.rowId) : undefined"
            />
          </div>
        </div>
        <p
          v-if="validationAttempted && errors.has(row.rowId)"
          :id="fieldId('error', row.rowId)"
          class="mt-2 text-xs text-red-600 dark:text-red-400"
          role="alert"
        >
          {{ errors.get(row.rowId) }}
        </p>
      </div>
    </div>

    <datalist :id="modelListId">
      <option v-for="model in modelCandidates" :key="model" :value="model" />
    </datalist>

    <button
      type="button"
      class="w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
      @click="emit('add')"
    >
      <svg class="mr-1 inline h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
      </svg>
      {{ t('admin.accounts.imageSizeCapability.add') }}
    </button>

    <p v-if="modelValue.length === 0" class="mt-2 text-xs text-gray-400">
      {{ t('admin.accounts.imageSizeCapability.emptyHint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { IMAGE_SIZE_TIERS } from '@/composables/useImageSizeCapabilities'
import type { ImageSizeCapabilityRow } from '@/composables/useImageSizeCapabilities'

const props = defineProps<{
  modelValue: ImageSizeCapabilityRow[]
  errors: ReadonlyMap<number, string>
  validationAttempted: boolean
  modelCandidates: readonly string[]
}>()
const emit = defineEmits<{
  'update:modelValue': [rows: ImageSizeCapabilityRow[]]
  add: []
  remove: [index: number]
}>()
const { t } = useI18n()
const instanceId = useId()
const modelListId = `image-size-capability-models-${instanceId}`
const imageSizeTierOptions = IMAGE_SIZE_TIERS.map((tier) => ({ value: tier, label: tier.toUpperCase() }))

function fieldId(field: string, rowId: number) {
  return `image-size-${field}-${instanceId}-${rowId}`
}

function updateRow(rowId: number, field: 'model' | 'tier' | 'upstream', value: string) {
  emit('update:modelValue', props.modelValue.map((row) => row.rowId === rowId ? { ...row, [field]: value } : row))
}
</script>
