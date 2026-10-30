<template>
  <div
    class="pointer-events-none absolute bottom-4 left-1/2 z-20 flex w-full -translate-x-1/2 items-center justify-center px-2 sm:bottom-8 sm:px-4"
  >
    <div
      class="pointer-events-auto flex items-center gap-2 rounded-2xl border border-gray-200/80 bg-white/95 px-2 py-1.5 shadow-2xl backdrop-blur-md dark:border-white/5 dark:bg-[#0f0f0f]/95 sm:gap-4 sm:rounded-[1.25rem] sm:px-3 sm:py-2"
    >
      <div class="flex items-center gap-1.5 sm:gap-3">
        <!-- 笔刷 / 橡皮 -->
        <div class="flex items-center rounded-xl bg-gray-100/80 p-1 dark:bg-[#232325]/80 sm:rounded-[14px]">
          <button
            type="button"
            class="rounded-lg p-2 transition-all sm:rounded-xl sm:p-2.5"
            :class="
              tool === 'brush'
                ? 'bg-white text-primary-500 shadow-sm dark:bg-[#323338] dark:text-primary-400 dark:shadow-none'
                : 'text-gray-500 hover:text-gray-700 dark:text-[#8a8a8e] dark:hover:text-dark-200'
            "
            :disabled="!ready || busy"
            :aria-label="t('imageStudio.mask.brush')"
            :title="t('imageStudio.mask.brush')"
            @click="emit('toolChange', 'brush')"
          >
            <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="m15.232 5.232 3.536 3.536m-2.036-5.036a2.5 2.5 0 1 1 3.536 3.536L6.5 21.036H3v-3.572L16.732 3.732Z"
              />
            </svg>
          </button>
          <button
            type="button"
            class="rounded-lg p-2 transition-all sm:rounded-xl sm:p-2.5"
            :class="
              tool === 'eraser'
                ? 'bg-white text-primary-500 shadow-sm dark:bg-[#323338] dark:text-primary-400 dark:shadow-none'
                : 'text-gray-500 hover:text-gray-700 dark:text-[#8a8a8e] dark:hover:text-dark-200'
            "
            :disabled="!ready || busy"
            :aria-label="t('imageStudio.mask.eraser')"
            :title="t('imageStudio.mask.eraser')"
            @click="emit('toolChange', 'eraser')"
          >
            <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <g transform="translate(0, 1) rotate(-45 12 12)">
                <path fill="currentColor" d="M4 10a2 2 0 0 1 2-2h7v8H6a2 2 0 0 1-2-2Z" />
                <rect x="4" y="8" width="16" height="8" rx="2" />
              </g>
              <path d="M8 21h12" />
            </svg>
          </button>
        </div>

        <div ref="brushSizeControlRef" class="relative flex items-center justify-center">
          <button
            ref="brushSizeButtonRef"
            type="button"
            class="flex h-10 w-10 items-center justify-center rounded-xl border transition-all sm:h-[46px] sm:w-[46px] sm:rounded-[14px]"
            :class="
              showBrushControls
                ? 'border-primary-200 bg-primary-50 text-primary-600 dark:border-[#323338] dark:bg-[#323338] dark:text-primary-400'
                : 'border-gray-200/80 bg-white text-gray-700 hover:bg-gray-50 dark:border-[#323338] dark:bg-transparent dark:text-[#e0e0e0]'
            "
            :disabled="!ready || busy"
            :aria-label="t('imageStudio.mask.brushSizeN', { size: brushSize })"
            :title="t('imageStudio.mask.adjustBrush')"
            @click="emit('toggleBrushControls')"
          >
            <span class="text-sm font-semibold tracking-tight sm:text-[15px]">{{ brushSize }}</span>
          </button>
        </div>
      </div>

      <!-- 撤销 / 重做 / 重置视图 / 清空 -->
      <div class="flex items-center gap-0.5 sm:ml-1 sm:gap-2">
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition hover:bg-gray-100 disabled:opacity-30 dark:text-[#8a8a8e] dark:hover:bg-white/10 dark:hover:text-dark-200 sm:rounded-xl sm:p-2.5"
          :disabled="!canUndo"
          :aria-label="t('imageStudio.mask.undo')"
          :title="t('imageStudio.mask.undo')"
          @click="emit('undo')"
        >
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 7v6h6" /><path d="M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6 2.3L3 13" />
          </svg>
        </button>
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition hover:bg-gray-100 disabled:opacity-30 dark:text-[#8a8a8e] dark:hover:bg-white/10 dark:hover:text-dark-200 sm:rounded-xl sm:p-2.5"
          :disabled="!canRedo"
          :aria-label="t('imageStudio.mask.redo')"
          :title="t('imageStudio.mask.redo')"
          @click="emit('redo')"
        >
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 7v6h-6" /><path d="M3 17a9 9 0 0 1 9-9 9 9 0 0 1 6 2.3l3 2.7" />
          </svg>
        </button>
        <div class="mx-1 h-4 w-px bg-gray-300 dark:bg-[#323338] sm:h-5" />
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition hover:bg-gray-100 disabled:opacity-30 dark:text-[#8a8a8e] dark:hover:bg-white/10 dark:hover:text-dark-200 sm:rounded-xl sm:p-2.5"
          :disabled="!canResetView"
          :aria-label="t('imageStudio.mask.resetView')"
          :title="t('imageStudio.mask.resetView')"
          @click="emit('resetView')"
        >
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 14h6v6" /><path d="M20 10h-6V4" /><path d="m14 10 7-7" /><path d="m3 21 7-7" />
          </svg>
        </button>
        <button
          type="button"
          class="rounded-lg p-2 text-gray-500 transition hover:bg-gray-100 disabled:opacity-30 dark:text-[#8a8a8e] dark:hover:bg-white/10 dark:hover:text-dark-200 sm:rounded-xl sm:p-2.5"
          :disabled="!ready || busy"
          :aria-label="t('imageStudio.mask.clear')"
          :title="t('imageStudio.mask.clear')"
          @click="emit('clear')"
        >
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 6h18" /><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6" />
            <path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" />
          </svg>
        </button>
      </div>
    </div>
  </div>

  <!-- 笔刷大小滑块：竖直浮层 -->
  <Teleport to="body">
    <div
      v-if="showBrushControls && sliderAnchor"
      ref="brushSizePanelRef"
      class="fixed z-[100] flex h-44 w-12 -translate-x-1/2 touch-none select-none items-center justify-center rounded-full border border-gray-200/80 bg-white/95 shadow-[0_8px_30px_rgb(0,0,0,0.12)] backdrop-blur-md dark:border-white/[0.08] dark:bg-[#28292d]/95"
      :style="{
        left: `${sliderAnchor.left}px`,
        bottom: `${sliderAnchor.bottom + 8}px`,
        touchAction: 'none',
      }"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
    >
      <input
        type="range"
        min="8"
        max="220"
        :value="brushSize"
        :disabled="!ready || busy"
        class="pointer-events-none h-1.5 w-32 -rotate-90 cursor-ns-resize appearance-none rounded-full bg-gray-200 outline-none dark:bg-black/30 [&::-webkit-slider-thumb]:h-4 [&::-webkit-slider-thumb]:w-4 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-primary-500 [&::-webkit-slider-thumb]:shadow-md"
        @input="emit('brushSizeChange', Number(($event.target as HTMLInputElement).value))"
      />
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

export type MaskTool = 'brush' | 'eraser'
export interface MaskSliderAnchor {
  left: number
  bottom: number
}

defineProps<{
  tool: MaskTool
  brushSize: number
  showBrushControls: boolean
  sliderAnchor: MaskSliderAnchor | null
  ready: boolean
  busy: boolean
  canUndo: boolean
  canRedo: boolean
  canResetView: boolean
}>()

const emit = defineEmits<{
  toolChange: [tool: MaskTool]
  toggleBrushControls: []
  brushSizeChange: [size: number]
  undo: []
  redo: []
  resetView: []
  clear: []
}>()

const { t } = useI18n()

const brushSizeControlRef = ref<HTMLDivElement | null>(null)
const brushSizeButtonRef = ref<HTMLButtonElement | null>(null)
const brushSizePanelRef = ref<HTMLDivElement | null>(null)

/** 按手指/鼠标在竖直滑块上的位置换算笔刷大小（8–220） */
function updateBrushFromPointer(event: PointerEvent) {
  const target = event.currentTarget as HTMLDivElement
  const rect = target.getBoundingClientRect()
  const padding = 16
  const ratio = Math.max(
    0,
    Math.min(1, 1 - (event.clientY - rect.top - padding) / (rect.height - padding * 2)),
  )
  emit('brushSizeChange', Math.round(8 + ratio * 212))
}

function onPointerDown(event: PointerEvent) {
  ;(event.currentTarget as HTMLDivElement).setPointerCapture(event.pointerId)
  updateBrushFromPointer(event)
}

function onPointerMove(event: PointerEvent) {
  const target = event.currentTarget as HTMLDivElement
  if (target.hasPointerCapture(event.pointerId)) updateBrushFromPointer(event)
}

defineExpose({ brushSizeControlRef, brushSizeButtonRef, brushSizePanelRef })
</script>
