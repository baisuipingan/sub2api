<template>
  <div class="flex-none border-t border-gray-200 bg-white p-3 dark:border-dark-800 dark:bg-dark-950 sm:px-5 sm:py-4">
    <div class="mx-auto max-w-5xl">
      <!-- 编辑方式 -->
      <div class="flex gap-1 overflow-x-auto pb-2" :aria-label="t('imageStudio.edit.modeAria')">
        <button
          v-for="item in MODES"
          :key="item.id"
          type="button"
          class="shrink-0 rounded-lg px-3 py-1.5 text-xs font-medium transition disabled:cursor-not-allowed disabled:opacity-35"
          :class="
            mode === item.id
              ? 'bg-primary-600 text-white'
              : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-white/[0.07] dark:text-dark-300 dark:hover:bg-white/[0.12]'
          "
          :disabled="isModeDisabled(item.id) || busy"
          @click="emit('modeChange', item.id)"
        >
          {{ t(`imageStudio.edit.modes.${item.id}`) }}
        </button>
      </div>

      <div class="flex flex-col gap-2 sm:flex-row sm:items-stretch">
        <div
          class="flex min-w-0 flex-1 items-center rounded-xl border border-gray-200 bg-gray-50 px-3 focus-within:border-primary-500 focus-within:ring-2 focus-within:ring-primary-500/10 dark:border-white/[0.1] dark:bg-white/[0.04]"
        >
          <textarea
            :value="prompt"
            :disabled="busy"
            :placeholder="promptPlaceholder"
            :aria-label="t('imageStudio.edit.promptAria')"
            rows="2"
            class="max-h-28 min-h-12 w-full resize-none bg-transparent py-2.5 text-sm text-gray-900 outline-none placeholder:text-gray-400 disabled:opacity-60 dark:text-white"
            @input="emit('promptChange', ($event.target as HTMLTextAreaElement).value)"
          />
        </div>

        <div class="grid grid-cols-2 gap-2 sm:flex sm:items-stretch">
          <label class="col-span-2 min-w-0 sm:col-span-1">
            <span class="sr-only">{{ t('imageStudio.edit.modelAria') }}</span>
            <select
              :value="selectedModelId"
              :disabled="busy"
              class="h-full min-h-11 w-full rounded-xl border border-gray-200 bg-white px-3 text-xs font-medium text-gray-700 outline-none focus:border-primary-500 dark:border-white/[0.1] dark:bg-dark-900 dark:text-dark-200 sm:max-w-56"
              @change="emit('modelChange', ($event.target as HTMLSelectElement).value)"
            >
              <option v-for="item in models" :key="item.id" :value="item.id">
                {{ item.name }} · {{ profileLabel(item) }}
              </option>
            </select>
          </label>

          <button
            type="button"
            class="min-h-11 rounded-xl border border-gray-200 px-3 text-xs font-medium text-gray-700 transition hover:bg-gray-50 disabled:opacity-35 dark:border-white/[0.1] dark:text-dark-300 dark:hover:bg-white/[0.06]"
            :disabled="!canSubmit"
            @click="emit('save')"
          >
            {{ t('imageStudio.edit.saveToWorkspace') }}
          </button>
          <button
            type="button"
            class="min-h-11 rounded-xl bg-primary-600 px-4 text-xs font-semibold text-white shadow-sm transition hover:bg-primary-700 disabled:opacity-35"
            :disabled="!canSubmit"
            @click="emit('generate')"
          >
            {{ busy ? t('imageStudio.edit.processing') : t('imageStudio.edit.generate') }}
          </button>
        </div>
      </div>

      <div
        class="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-gray-500 dark:text-dark-400"
      >
        <span>{{ statusText }}</span>
        <label v-if="model && model.capabilities.qualities.length > 1" class="flex items-center gap-1.5">
          {{ t('imageStudio.detail.quality') }}
          <select
            :value="params.quality"
            :disabled="busy"
            class="rounded-md border border-gray-200 bg-white px-1.5 py-1 outline-none dark:border-white/[0.1] dark:bg-dark-900"
            @change="emit('paramsChange', { ...params, quality: ($event.target as HTMLSelectElement).value as TaskParams['quality'] })"
          >
            <option v-for="quality in model.capabilities.qualities" :key="quality" :value="quality">
              {{ quality === 'auto' ? t('imageStudio.params.auto') : quality }}
            </option>
          </select>
        </label>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ImageEditMode, TaskParams } from '../../types'
import { getImageEditProfile, type ImageModelDefinition } from '../../lib/modelRegistry'

const props = defineProps<{
  mode: ImageEditMode
  prompt: string
  params: TaskParams
  models: ImageModelDefinition[]
  selectedModelId: string
  busy: boolean
  canUseMask: boolean
  canRemoveBackground: boolean
}>()

const emit = defineEmits<{
  modeChange: [mode: ImageEditMode]
  promptChange: [prompt: string]
  modelChange: [modelId: string]
  paramsChange: [params: TaskParams]
  save: []
  generate: []
}>()

const { t } = useI18n()

const MODES: Array<{ id: ImageEditMode }> = [
  { id: 'region' },
  { id: 'erase' },
  { id: 'background' },
  { id: 'full' },
]

const model = computed(() => props.models.find((item) => item.id === props.selectedModelId))

const modeNeedsMask = computed(() => props.mode === 'region' || props.mode === 'erase')
const promptRequired = computed(() => props.mode === 'region' || props.mode === 'full')

const unavailable = computed(() =>
  modeNeedsMask.value
    ? !props.canUseMask
    : props.mode === 'background' && !props.canRemoveBackground,
)

const canSubmit = computed(
  () =>
    !props.busy &&
    !unavailable.value &&
    (!promptRequired.value || Boolean(props.prompt.trim())),
)

const promptPlaceholder = computed(() => {
  if (props.mode === 'erase') return t('imageStudio.edit.erasePlaceholder')
  if (props.mode === 'background') return t('imageStudio.edit.backgroundPlaceholder')
  return t('imageStudio.edit.promptPlaceholder')
})

const statusText = computed(() => {
  if (unavailable.value) return t('imageStudio.edit.unsupportedMode')
  if (!model.value) return t('imageStudio.edit.pickModel')
  return `${profileLabel(model.value)}: ${profileDescription(model.value)}`
})

function isModeDisabled(mode: ImageEditMode) {
  if (mode === 'region' || mode === 'erase') return !props.canUseMask
  if (mode === 'background') return !props.canRemoveBackground
  return false
}

/** 模型档位标签统一走 i18n；不要把本地化文案当作判断条件 */
function profileLabel(item: ImageModelDefinition) {
  return t(`imageStudio.edit.profileLabel.${getImageEditProfile(item)}`)
}

function profileDescription(item: ImageModelDefinition) {
  const profile = getImageEditProfile(item)
  if (profile === 'fast') return t('imageStudio.edit.profileFast')
  if (profile === 'precision') return t('imageStudio.edit.profilePrecise')
  return t('imageStudio.edit.profileCompat')
}
</script>
