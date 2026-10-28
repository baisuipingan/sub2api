<template>
  <button
    type="button"
    @click="toggle"
    class="neu-switch"
    :class="{ 'neu-switch-on': modelValue }"
    role="switch"
    :aria-checked="modelValue"
    :disabled="disabled"
  >
    <span class="neu-switch-thumb" />
  </button>
</template>

<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    modelValue: boolean
    disabled?: boolean
  }>(),
  { disabled: false }
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

function toggle() {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}
</script>

<style scoped>
/*
 * 新拟态开关。
 * 关键：OFF 态轨道必须比页面背景深（--neu-sunk），
 * 否则纯靠内阴影的控件在浅色底上完全看不见。
 */
.neu-switch {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
  width: 44px;
  height: 24px;
  padding: 0;
  border: 0;
  border-radius: 999px;
  cursor: pointer;
  background-color: var(--neu-sunk, #e2e5eb);
  box-shadow:
    inset -2px -2px 6px var(--neu-light, #ffffff),
    inset 2px 2px 7px var(--neu-dark, #c2c7d2);
  transition:
    box-shadow 0.22s cubic-bezier(0.4, 0, 0.2, 1),
    background-color 0.22s cubic-bezier(0.4, 0, 0.2, 1);
}

.neu-switch:focus-visible {
  outline: 2px solid #5865d4;
  outline-offset: 3px;
}

.neu-switch:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.neu-switch-on {
  background-color: transparent;
  background-image: linear-gradient(145deg, #5f6cd8, #4a56c0);
  box-shadow:
    inset 1px 1px 2px rgba(255, 255, 255, 0.3),
    inset -1px -1px 3px rgba(50, 62, 150, 0.3);
}

/* 深色模式下减弱亮侧阴影，避免刺眼外发光 */
:global(.dark) .neu-switch {
  box-shadow:
    inset -1px -1px 4px rgba(255, 255, 255, 0.06),
    inset 2px 2px 6px rgba(0, 0, 0, 0.5);
}

:global(.dark) .neu-switch-thumb {
  box-shadow:
    -1px -1px 3px rgba(255, 255, 255, 0.08),
    1.5px 1.5px 5px rgba(0, 0, 0, 0.55);
}

.neu-switch-thumb {
  position: absolute;
  top: 2.5px;
  left: 2.5px;
  width: 19px;
  height: 19px;
  border-radius: 50%;
  background-color: var(--neu-bg, #edeff3);
  box-shadow:
    -1.5px -1.5px 4px var(--neu-light, #ffffff),
    1.5px 1.5px 5px var(--neu-dark, #c2c7d2);
  transition: transform 0.26s cubic-bezier(0.16, 1, 0.3, 1);
}

.neu-switch-on .neu-switch-thumb {
  transform: translateX(20px);
}

/* 系统开启减少动效时关闭过渡 */
@media (prefers-reduced-motion: reduce) {
  .neu-switch,
  .neu-switch-thumb {
    transition-duration: 1ms;
  }
}
</style>
