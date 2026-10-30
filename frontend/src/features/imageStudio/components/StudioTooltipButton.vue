<template>
  <span :class="wrapperClassName" v-on="tooltip.handlers">
    <button
      type="button"
      :class="className"
      :aria-label="tooltipText"
      :aria-disabled="disabled"
      :disabled="disabled && !showOnClick"
      @click="handleClick"
      @mousedown="handleMouseDown"
    >
      <slot />
    </button>
    <StudioViewportTooltip :visible="tooltip.visible.value" class="whitespace-nowrap">
      {{ tooltipText }}
    </StudioViewportTooltip>
  </span>
</template>

<script setup lang="ts">
import StudioViewportTooltip from './StudioViewportTooltip.vue'
import { useTooltip } from '../composables/useTooltip'

/**
 * 带 tooltip 的按钮。所有需要提示的图标按钮统一走这里，
 * 避免每个按钮各写一套 hover 逻辑。
 */
const props = withDefaults(
  defineProps<{
    tooltipText: string
    className: string
    wrapperClassName?: string
    disabled?: boolean
    showOnClick?: boolean
    stopPropagation?: boolean
  }>(),
  {
    wrapperClassName: 'relative inline-flex',
    disabled: false,
    showOnClick: false,
    stopPropagation: false,
  },
)

const emit = defineEmits<{
  click: [e: MouseEvent]
  mousedown: [e: MouseEvent]
}>()

const tooltip = useTooltip()

function handleClick(e: MouseEvent) {
  if (props.stopPropagation) e.stopPropagation()
  if (props.showOnClick) tooltip.show()
  else tooltip.dismiss()
  if (props.disabled) return
  emit('click', e)
}

function handleMouseDown(e: MouseEvent) {
  tooltip.dismiss()
  if (props.disabled) return
  emit('mousedown', e)
}
</script>
