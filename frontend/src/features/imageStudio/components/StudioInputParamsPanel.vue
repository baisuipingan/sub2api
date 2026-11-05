<template>
  <div v-if="model && capabilities" class="space-y-4">
    <div class="border-t border-gray-100 pt-4 dark:border-white/[0.06]">
      <h3 class="text-sm font-medium text-gray-700 dark:text-dark-200" data-testid="provider-parameters-title">
        {{ model.provider && model.provider !== 'unknown' ? t(`imageStudio.params.providers.${model.provider}`) : t('imageStudio.params.title') }}
      </h3>
      <p aria-live="polite" class="mt-1 text-xs text-gray-500">{{ sizeSummary }}</p>
      <p v-if="model.capabilityStatus === 'unknown'" class="mt-2 text-xs leading-5 text-gray-500" role="status">
        {{ t('imageStudio.params.unknownCapabilities') }}
      </p>
      <p v-else-if="model.capabilityStatus === 'mixed'" class="mt-2 text-xs leading-5 text-gray-500" role="status">
        {{ t('imageStudio.params.sharedCapabilities') }}
      </p>
    </div>

    <!-- 清晰度 -->
    <div v-if="capabilities.resolutions.length > 1">
      <div class="mb-2 flex items-center justify-between">
        <span class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('imageStudio.params.resolution') }}
        </span>
      </div>
      <div
        class="grid gap-2"
        :style="{ gridTemplateColumns: `repeat(${capabilities.resolutions.length}, minmax(0, 1fr))` }"
      >
        <button
          v-for="resolution in capabilities.resolutions"
          :key="resolution"
          type="button"
          class="btn btn-sm border px-2 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-40"
          :class="
            currentResolution === resolution
              ? 'border-primary-500 bg-primary-50 font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
              : 'border-gray-200 text-gray-600 hover:border-gray-300 dark:border-white/[0.1] dark:text-dark-300'
          "
          :aria-pressed="currentResolution === resolution"
          :disabled="!isResolutionAvailable(model, resolution)"
          :title="!isResolutionAvailable(model, resolution) ? t('imageStudio.params.channelSizeUnavailable', { resolution: RESOLUTION_LABELS[resolution] }) : undefined"
          @click="setResolution(resolution)"
        >
          {{ capabilities.customSize && resolution === 'auto' ? t('imageStudio.params.default') : RESOLUTION_LABELS[resolution] }}
        </button>
      </div>
      <p v-if="model.availableSizeTiers && capabilities.resolutions.some((tier) => !isResolutionAvailable(model, tier))" class="mt-2 text-xs leading-5 text-gray-500" role="status">
        {{ t('imageStudio.params.availableSizes', { sizes: model.availableSizeTiers.join(' / ') }) }}
      </p>
    </div>

    <!-- 比例 -->
    <div v-if="capabilities.aspectRatios.length > 1 || capabilities.customSize">
      <span class="mb-2 block text-xs font-medium text-gray-500 dark:text-dark-400">
        {{ t('imageStudio.params.aspectRatio') }}
      </span>
      <div role="group" :aria-label="t('imageStudio.params.aspectRatio')" class="flex flex-wrap gap-2">
        <button
          type="button"
          class="btn btn-sm btn-ghost rounded-lg border px-2.5 py-1.5 text-sm transition"
          :class="
            ratioIsAuto
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
              : 'border-gray-200 text-gray-600 hover:border-gray-300 dark:border-white/[0.1] dark:text-dark-300'
          "
          :aria-pressed="ratioIsAuto"
          @click="setAutoRatio"
        >
          {{ t('imageStudio.params.auto') }}
        </button>
        <button
          v-for="ratio in quickRatios"
          :key="ratio"
          type="button"
          class="btn btn-sm btn-ghost rounded-lg border px-2.5 py-1.5 text-sm transition"
          :class="
            currentRatio === ratio && !ratioIsAuto
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
              : 'border-gray-200 text-gray-600 hover:border-gray-300 dark:border-white/[0.1] dark:text-dark-300'
          "
          :aria-pressed="currentRatio === ratio && !ratioIsAuto"
          @click="setRatio(ratio)"
        >
          {{ ratio }}
        </button>
        <button
          v-if="moreRatios.length > 0 || capabilities.customSize"
          type="button"
          class="rounded-lg border border-gray-200 px-2.5 py-1.5 text-sm text-gray-500 transition hover:border-gray-300 dark:border-white/[0.1] dark:text-dark-400"
          @click="showRatioPicker = true"
        >
          {{ t('imageStudio.params.moreRatios') }}
        </button>
      </div>
    </div>

    <!-- 质量 -->
    <div v-if="capabilities.qualities.length > 1">
      <div class="mb-2 flex items-center justify-between gap-3">
        <span class="text-xs font-medium text-gray-500 dark:text-dark-400">
          {{ t('imageStudio.params.quality') }}
        </span>
      </div>
      <div class="grid grid-cols-2 gap-2">
        <button
          v-for="quality in capabilities.qualities"
          :key="quality"
          type="button"
          class="btn btn-sm btn-ghost rounded-lg border px-2 py-2 text-sm transition"
          :class="
            params.quality === quality
              ? 'border-primary-500 bg-primary-50 font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
              : 'border-gray-200 text-gray-600 hover:border-gray-300 dark:border-white/[0.1] dark:text-dark-300'
          "
          :aria-pressed="params.quality === quality"
          @click="setParams({ quality })"
        >
          {{ qualityLabel(quality) }}
        </button>
      </div>
    </div>

    <div v-if="capabilities.complexities?.length" data-testid="image-complexity">
      <span class="mb-2 block text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('imageStudio.params.complexity') }}</span>
      <div class="grid grid-cols-2 gap-2">
        <button
          v-for="complexity in capabilities.complexities"
          :key="complexity"
          type="button"
          class="btn btn-sm btn-ghost rounded-lg border px-2 py-2 text-sm transition"
          :class="(params.complexity ?? 'auto') === complexity ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300' : 'border-gray-200 text-gray-600 dark:border-white/[0.1] dark:text-dark-300'"
          :aria-pressed="(params.complexity ?? 'auto') === complexity"
          @click="setParams({ complexity })"
        >{{ t(`imageStudio.params.complexityOptions.${complexity}`) }}</button>
      </div>
      <p class="mt-1.5 text-[11px] leading-4 text-gray-500">{{ t('imageStudio.params.complexityHint') }}</p>
    </div>

    <!-- 数量 -->
    <div v-if="maxImageCount > capabilities.imageCount.min" class="flex items-center justify-between gap-3">
      <label for="image-count" class="text-xs font-medium text-gray-500 dark:text-dark-400">
        {{ t('imageStudio.params.count') }}
      </label>
      <div class="flex h-9 items-center overflow-hidden rounded-lg border border-gray-200 dark:border-white/[0.1]">
        <button
          type="button"
          class="btn btn-sm btn-ghost h-9 w-9 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-white/[0.06]"
          :aria-label="t('imageStudio.params.decreaseCount')"
          :title="t('imageStudio.params.decreaseCount')"
          :disabled="params.n <= capabilities.imageCount.min"
          @click="setParams({ n: params.n - 1 })"
        >
          −
        </button>
        <Input
          id="image-count"
          type="number"
          class="w-16 [&_input]:h-9 [&_input]:rounded-none [&_input]:border-x [&_input]:border-y-0 [&_input]:px-1 [&_input]:text-center"
          :min="capabilities.imageCount.min"
          :max="maxImageCount"
          :model-value="params.n"
          :aria-label="t('imageStudio.params.count')"
          @change="onCountChange"
        />
        <button
          type="button"
          class="btn btn-sm btn-ghost h-9 w-9 hover:bg-gray-100 disabled:opacity-30 dark:hover:bg-white/[0.06]"
          :aria-label="t('imageStudio.params.increaseCount')"
          :title="t('imageStudio.params.increaseCount')"
          :disabled="params.n >= maxImageCount"
          @click="setParams({ n: params.n + 1 })"
        >
          +
        </button>
      </div>
    </div>

    <!-- 更多参数 -->
    <button
      v-if="hasAdvancedParams"
      type="button"
      class="btn btn-sm btn-ghost flex w-full items-center justify-between border-t border-gray-100 pt-3 text-sm text-gray-500 transition hover:text-gray-800 dark:border-white/[0.06] dark:text-dark-400 dark:hover:text-dark-200"
      :aria-expanded="advanced"
      @click="advanced = !advanced"
    >
      <span>{{ t('imageStudio.params.moreParams') }}</span>
      <span aria-hidden="true">{{ advanced ? '−' : '+' }}</span>
    </button>

    <div
      v-if="hasAdvancedParams && advanced"
      class="grid grid-cols-2 gap-3 border-t border-gray-100 pt-3 dark:border-white/[0.06]"
    >
      <div v-if="capabilities.outputFormats.length > 1" class="min-w-0">
        <span class="mb-1.5 block text-xs text-gray-500">{{ t('imageStudio.detail.format') }}</span>
        <Select
          :model-value="params.output_format"
          :options="capabilities.outputFormats.map((format) => ({ value: format, label: format.toUpperCase() }))"
          :aria-label="t('imageStudio.detail.format')"
          :searchable="false"
          @update:model-value="setParams({ output_format: $event as TaskParams['output_format'] })"
        />
      </div>

      <label v-if="capabilities.outputCompression && params.output_format !== 'png'" class="min-w-0">
        <span class="mb-1.5 block text-xs text-gray-500">{{ t('imageStudio.params.compression') }}</span>
        <Input
          type="number"
          min="0"
          max="100"
          :aria-label="t('imageStudio.params.compression')"
          :model-value="params.output_compression ?? ''"
          @change="setParams({ output_compression: $event === '' ? null : Math.min(100, Math.max(0, Number($event) || 0)) })"
        />
      </label>

      <div v-if="capabilities.moderation.length > 1" class="min-w-0">
        <span class="mb-1.5 block text-xs text-gray-500">{{ t('imageStudio.detail.moderation') }}</span>
        <Select
          :model-value="params.moderation"
          :options="capabilities.moderation.map((moderation) => ({ value: moderation, label: moderation === 'auto' ? t('imageStudio.params.moderationStandard') : t('imageStudio.params.moderationLoose') }))"
          :aria-label="t('imageStudio.detail.moderation')"
          :searchable="false"
          @update:model-value="setParams({ moderation: $event as TaskParams['moderation'] })"
        />
      </div>

      <div
        v-if="capabilities.transparentOutput && (params.output_format === 'png' || params.output_format === 'webp')"
        class="col-span-2 flex items-center justify-between rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-sm text-gray-700 dark:border-white/[0.1] dark:bg-white/[0.04] dark:text-dark-200"
      >
        <span>{{ t('imageStudio.detail.transparent') }}</span>
        <Toggle
          :model-value="params.transparent_output"
          :aria-label="t('imageStudio.detail.transparent')"
          @update:model-value="setParams({ transparent_output: $event })"
        />
      </div>

      <div v-if="capabilities.maxPartialImages > 0" class="col-span-2 min-w-0">
        <span class="mb-1.5 block text-xs text-gray-500">{{ t('imageStudio.params.partialPreview') }}</span>
        <Select
          :model-value="params.partial_images"
          :options="[{ value: 0, label: t('imageStudio.params.off') }, ...Array.from({ length: capabilities.maxPartialImages }, (_, i) => ({ value: i + 1, label: t('imageStudio.params.partialCount', { count: i + 1 }) }))]"
          :aria-label="t('imageStudio.params.partialPreview')"
          :searchable="false"
          @update:model-value="setParams({ partial_images: Number($event) as TaskParams['partial_images'] })"
        />
        <span class="mt-1 block text-[11px] leading-4 text-amber-600 dark:text-amber-400">
          {{ t('imageStudio.params.partialHint') }}
        </span>
      </div>
    </div>

    <StudioRatioPickerModal
      v-if="showRatioPicker"
      :current-ratio="ratioIsAuto ? '' : currentRatio || exactRatio"
      :ratios="moreRatios"
      :all-preset-ratios="capabilities.aspectRatios"
      :allow-custom="capabilities.customSize"
      @select="setRatio"
      @close="showRatioPicker = false"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioRatioPickerModal from './StudioRatioPickerModal.vue'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { TaskParams } from '../types'
import type { ImageModelDefinition, ImageResolutionTier } from '../lib/modelRegistry'
import { getModelResolution } from '../lib/modelRegistry'
import { MAX_OUTPUT_IMAGES, parameterImageSize, usesNativeGeometry, isResolutionAvailable } from '../lib/paramCompatibility'
import { parseRatio } from '../lib/size'

const props = defineProps<{
  params: TaskParams
  model: ImageModelDefinition | null
}>()

const emit = defineEmits<{ setParams: [patch: Partial<TaskParams>] }>()
const { t } = useI18n()

const advanced = ref(false)
const showRatioPicker = ref(false)

const capabilities = computed(() => props.model?.capabilities)

const RESOLUTION_LABELS: Record<ImageResolutionTier, string> = {
  auto: t('imageStudio.params.default'),
  '0.5K': '512',
  '1K': '1K',
  '2K': '2K',
  '4K': '4K',
}

function setParams(patch: Partial<TaskParams>) {
  emit('setParams', patch)
}

function qualityLabel(quality: TaskParams['quality']) {
  return t(`imageStudio.params.qualityOptions.${quality}`)
}

function getExactRatio(size: string, fallback: string) {
  const match = size.match(/^(\d+)x(\d+)$/i)
  if (!match) return fallback
  const width = Number(match[1])
  const height = Number(match[2])
  const gcd = (a: number, b: number): number => (b === 0 ? a : gcd(b, a % b))
  const divisor = gcd(width, height)
  return `${width / divisor}:${height / divisor}`
}

/** 在一组预设比例里找最接近的（对数距离，容差 0.01） */
function closestRatio(ratio: string, ratios: string[]) {
  const parsedTarget = parseRatio(ratio)
  if (!parsedTarget) return ''
  const target = parsedTarget.width / parsedTarget.height
  const closest = ratios
    .map((r) => {
      const parsed = parseRatio(r)
      return {
        ratio: r,
        delta: parsed
          ? Math.abs(Math.log(target / (parsed.width / parsed.height)))
          : Number.POSITIVE_INFINITY,
      }
    })
    .sort((a, b) => a.delta - b.delta)[0]
  return closest && closest.delta < 0.01 ? closest.ratio : ''
}

const currentResolution = computed(() =>
  getModelResolution(props.model, props.params.size, props.params.resolution),
)

const exactRatio = computed(() =>
  usesNativeGeometry(props.model) && props.params.aspect_ratio && props.params.aspect_ratio !== 'auto'
    ? props.params.aspect_ratio
    : getExactRatio(props.params.size, props.model?.defaults.aspectRatio ?? '1:1'),
)

const ratioIsAuto = computed(() => usesNativeGeometry(props.model)
  ? !props.params.aspect_ratio || props.params.aspect_ratio === 'auto'
  : props.params.size === 'auto')

const currentRatio = computed(() =>
  closestRatio(exactRatio.value, capabilities.value?.aspectRatios ?? []),
)

const maxImageCount = computed(() =>
  Math.min(MAX_OUTPUT_IMAGES, capabilities.value?.imageCount.max ?? MAX_OUTPUT_IMAGES),
)

const resolutionLabel = computed(() => {
  if (currentResolution.value === 'auto' && capabilities.value?.customSize) {
    return t('imageStudio.params.defaultResolution')
  }
  return RESOLUTION_LABELS[currentResolution.value]
})

const sizeSummary = computed(() =>
  usesNativeGeometry(props.model)
    ? t('imageStudio.params.nativeSizeSummary', { resolution: resolutionLabel.value, ratio: ratioIsAuto.value ? t('imageStudio.params.auto') : exactRatio.value })
    : props.params.size === 'auto'
    ? t('imageStudio.params.sizeAutoSummary', { resolution: resolutionLabel.value })
    : t('imageStudio.params.sizeSummary', {
        size: props.params.size.replace('x', ' × '),
        ratio: exactRatio.value,
      }),
)

const quickRatios = computed(() =>
  ['1:1', '16:9', '4:3'].filter((r) => capabilities.value?.aspectRatios.includes(r)),
)

const moreRatios = computed(() =>
  (capabilities.value?.aspectRatios ?? []).filter((r) => !quickRatios.value.includes(r)),
)

const hasAdvancedParams = computed(() => {
  const cap = capabilities.value
  if (!cap) return false
  return (
    cap.outputFormats.length > 1 ||
    Boolean(cap.outputCompression) ||
    cap.moderation.length > 1 ||
    Boolean(cap.transparentOutput) ||
    cap.maxPartialImages > 0
  )
})

function setResolution(resolution: ImageResolutionTier) {
  const model = props.model
  const cap = capabilities.value
  if (!model || !cap) return
  if (!isResolutionAvailable(model, resolution)) return
  if (usesNativeGeometry(model)) {
    setParams({ resolution })
    return
  }

  if (props.params.size === 'auto') {
    const size =
      cap.customSize && resolution !== 'auto'
        ? parameterImageSize(model, resolution, model.defaults.aspectRatio)
        : null
    setParams(size ? { resolution, size } : { resolution })
    return
  }

  const sizeTier =
    resolution === 'auto'
      ? model.defaults.resolution === 'auto'
        ? '1K'
        : model.defaults.resolution
      : resolution
  const size = parameterImageSize(model, sizeTier, exactRatio.value)
  if (size) setParams({ resolution, size })
}

function setRatio(ratio: string) {
  const model = props.model
  if (!model) return
  if (usesNativeGeometry(model)) {
    setParams({ aspect_ratio: ratio, size: 'auto' })
    return
  }

  const tier =
    currentResolution.value === 'auto'
      ? model.defaults.resolution === 'auto'
        ? '1K'
        : model.defaults.resolution
      : currentResolution.value
  const size = parameterImageSize(model, tier, ratio)
  if (size) setParams({ size })
}

function setAutoRatio() {
  setParams({ size: 'auto', ...(usesNativeGeometry(props.model) ? { aspect_ratio: 'auto' } : {}) })
}

function onCountChange(value: string) {
  const cap = capabilities.value
  const model = props.model
  if (!cap || !model) return
  const raw = Math.floor(Number(value) || model.defaults.imageCount)
  const next = Math.max(cap.imageCount.min, Math.min(maxImageCount.value, raw))
  setParams({ n: next })
}
</script>
