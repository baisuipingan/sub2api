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
 * 开关：OFF 用比画布深一档的轨道，ON 用品牌实色。
 * 不再依赖内阴影 —— 在密集表格里，纯阴影的开关几乎看不见状态。
 */
.neu-switch {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
  width: 40px;
  height: 22px;
  padding: 0;
  border: 1px solid var(--border-strong);
  border-radius: 999px;
  cursor: pointer;
  background-color: var(--neu-track);
  transition:
    background-color 0.2s var(--neu-ease),
    border-color 0.2s var(--neu-ease);
}

.neu-switch:focus-visible {
  outline: none;
  border-color: var(--brand);
  box-shadow: var(--shadow-focus);
}

.neu-switch:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.neu-switch-on {
  background-color: var(--brand);
  border-color: var(--brand);
}

.neu-switch-thumb {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background-color: #ffffff;
  box-shadow: 0 1px 3px rgba(16, 24, 40, 0.24);
  transition: transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}

.neu-switch-on .neu-switch-thumb {
  transform: translateX(18px);
}

/* 系统开启减少动效时关闭过渡 */
@media (prefers-reduced-motion: reduce) {
  .neu-switch,
  .neu-switch-thumb {
    transition-duration: 1ms;
  }
}
</style>
