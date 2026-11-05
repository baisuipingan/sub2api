<template>
  <div class="flex items-start gap-2">
    <div ref="rootRef" class="relative min-w-0 flex-1" @keydown="handleKeyDown">
      <button
        type="button"
        class="group flex w-full items-center gap-3 rounded-xl border bg-white px-4 py-3 text-left shadow-sm outline-none transition duration-150 disabled:cursor-not-allowed disabled:bg-gray-50 disabled:text-gray-400 dark:bg-white/[0.04] dark:disabled:bg-white/[0.025]"
        :class="
          open
            ? 'border-primary-500 ring-4 ring-primary-500/10 dark:border-primary-400'
            : 'border-gray-200 hover:border-gray-300 hover:shadow dark:border-white/[0.12] dark:hover:border-white/[0.2]'
        "
        :aria-label="t('imageStudio.modelSelect.ariaLabel', { name: selectedModel?.id ?? placeholder })"
        aria-haspopup="listbox"
        :aria-expanded="open"
        :aria-controls="open ? listboxId : undefined"
        :disabled="disabled || refreshing"
        @click="open = !open"
      >
        <span
          class="min-w-0 flex-1 truncate text-sm font-medium"
          :class="selectedModel ? 'text-gray-900 dark:text-white' : 'text-gray-400 dark:text-dark-500'"
        >
          {{ selectedModel?.id ?? placeholder }}
        </span>
        <svg
          aria-hidden="true"
          viewBox="0 0 20 20"
          fill="none"
          class="h-4 w-4 shrink-0 text-gray-400 transition-transform duration-200 group-hover:text-gray-600 dark:group-hover:text-dark-300"
          :class="open ? 'rotate-180 text-primary-500' : ''"
        >
          <path
            d="m5.5 7.5 4.5 4.5 4.5-4.5"
            stroke="currentColor"
            stroke-width="1.7"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>

      <div
        v-if="open"
        class="absolute left-0 right-0 z-30 mt-2 overflow-hidden rounded-xl border border-gray-200 bg-white p-1.5 shadow-[0_14px_38px_rgba(15,23,42,0.16)] dark:border-white/[0.12] dark:bg-dark-900 dark:shadow-black/40"
      >
        <div v-if="models.length > 8" class="p-1 pb-2">
          <input
            ref="searchRef"
            v-model="query"
            :placeholder="t('imageStudio.modelSelect.searchPlaceholder')"
            :aria-label="t('imageStudio.modelSelect.searchPlaceholder')"
            class="w-full rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:bg-white focus:ring-2 focus:ring-primary-500/10 dark:border-white/[0.1] dark:bg-white/[0.05] dark:text-white"
            @input="highlightedIndex = 0"
          />
        </div>

        <div :id="listboxId" role="listbox" :aria-label="t('imageStudio.modelSelect.listAria')" class="max-h-64 overflow-y-auto">
          <button
            v-for="(model, index) in visibleModels"
            :key="model.id"
            type="button"
            role="option"
            class="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition"
            :class="
              highlightedIndex === index
                ? 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-200'
                : 'text-gray-700 hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-white/[0.05]'
            "
            :aria-selected="model.id === selectedModelId"
            @mouseenter="highlightedIndex = index"
            @click="chooseModel(model)"
          >
            <span class="min-w-0 flex-1 truncate font-medium" :title="model.id">{{ model.id }}</span>
            <span
              v-if="model.id === selectedModelId"
              class="flex h-5 w-5 items-center justify-center rounded-full bg-primary-600 text-white"
            >
              <svg aria-hidden="true" viewBox="0 0 20 20" fill="none" class="h-3 w-3">
                <path
                  d="m5.5 10 3 3 6-6"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </span>
          </button>

          <p v-if="!visibleModels.length" class="px-3 py-6 text-center text-sm text-gray-400">
            {{ t('imageStudio.modelSelect.noMatch') }}
          </p>
        </div>
      </div>
    </div>
    <button
      type="button"
      data-testid="refresh-models"
      class="flex h-[46px] w-[46px] shrink-0 items-center justify-center rounded-xl border border-gray-200 bg-white text-gray-500 shadow-sm outline-none transition hover:border-gray-300 hover:text-primary-600 hover:shadow focus-visible:border-primary-500 focus-visible:ring-4 focus-visible:ring-primary-500/10 disabled:cursor-not-allowed disabled:bg-gray-50 disabled:text-gray-400 dark:border-white/[0.12] dark:bg-white/[0.04] dark:text-dark-300 dark:hover:border-white/[0.2] dark:hover:text-primary-400 dark:disabled:bg-white/[0.025] dark:disabled:text-dark-500"
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
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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

const open = ref(false)
const query = ref('')
const highlightedIndex = ref(0)
const rootRef = ref<HTMLDivElement | null>(null)
const searchRef = ref<HTMLInputElement | null>(null)
const listboxId = `model-listbox-${Math.random().toString(36).slice(2, 9)}`

const selectedModel = computed(() =>
  props.models.find((model) => model.id === props.selectedModelId),
)

const visibleModels = computed(() => {
  const normalized = query.value.trim().toLowerCase()
  return normalized
    ? props.models.filter((model) => model.id.toLowerCase().includes(normalized))
    : props.models
})

const refreshLabel = computed(() => t(
  props.refreshing ? 'imageStudio.modelSelect.refreshing' : 'imageStudio.modelSelect.refresh',
))

function refreshModels() {
  if (props.refreshing || props.refreshDisabled) return
  open.value = false
  query.value = ''
  emit('refresh')
}

function chooseModel(model: ImageModelDefinition) {
  emit('change', model.id)
  open.value = false
  query.value = ''
}

/** 点外部收起 */
function onPointerDown(event: PointerEvent) {
  if (!rootRef.value?.contains(event.target as Node)) open.value = false
}

watch(open, async (isOpen) => {
  if (!isOpen) {
    document.removeEventListener('pointerdown', onPointerDown)
    return
  }
  document.addEventListener('pointerdown', onPointerDown)

  const selectedIndex = visibleModels.value.findIndex((m) => m.id === props.selectedModelId)
  highlightedIndex.value = selectedIndex >= 0 ? selectedIndex : 0

  // 模型多时自动聚焦搜索框，便于直接输入筛选
  if (props.models.length > 8) {
    await nextTick()
    searchRef.value?.focus()
  }
})

watch(
  () => props.selectedModelId,
  () => {
    if (!open.value) return
    const idx = visibleModels.value.findIndex((m) => m.id === props.selectedModelId)
    if (idx >= 0) highlightedIndex.value = idx
  },
)

watch(
  () => props.disabled || props.refreshing,
  (disabled) => {
    if (disabled) open.value = false
  },
)

onUnmounted(() => {
  document.removeEventListener('pointerdown', onPointerDown)
})

function handleKeyDown(event: KeyboardEvent) {
  if (props.disabled || props.refreshing) return

  if (event.key === 'Escape') {
    open.value = false
    query.value = ''
    return
  }

  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!open.value) {
      open.value = true
      return
    }
    if (!visibleModels.value.length) return
    const direction = event.key === 'ArrowDown' ? 1 : -1
    highlightedIndex.value =
      (highlightedIndex.value + direction + visibleModels.value.length) %
      visibleModels.value.length
    return
  }

  if (open.value && event.key === 'Enter' && visibleModels.value[highlightedIndex.value]) {
    event.preventDefault()
    chooseModel(visibleModels.value[highlightedIndex.value])
  }
}
</script>
