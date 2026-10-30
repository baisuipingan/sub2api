import { onUnmounted, ref } from 'vue'
import { onDismissTooltips } from '../lib/tooltipDismiss'

/**
 * 统一的 tooltip 控制。
 *
 * 行为要点（与原型保持一致）：
 * - 悬停 / 聚焦时显示
 * - 触摸设备长按 500ms 才显示，避免滑动时误触
 * - 任何弹窗打开时通过全局总线统一隐藏
 * - `dismiss()` 会置抑制位，防止待触发的定时器把气泡又弹出来
 *
 * 迁移说明：原实现基于 React 的 useState + useRef + useCallback 组合，
 * 这里改为 Vue 的 ref + 普通闭包。状态机语义不变。
 */
export function useTooltip(delay = 500) {
  const visible = ref(false)

  let timer: number | null = null
  /** 被 dismiss 后抑制，直到下一次鼠标/焦点周期才恢复 */
  let suppressed = false
  let longPressTriggered = false

  function clearTimer() {
    if (timer != null) {
      window.clearTimeout(timer)
      timer = null
    }
  }

  function dismiss() {
    clearTimer()
    suppressed = true
    longPressTriggered = false
    visible.value = false
  }

  function show() {
    suppressed = false
    visible.value = true
  }

  function hide() {
    visible.value = false
  }

  // 任一弹窗打开时自动收起
  const unsubscribe = onDismissTooltips(dismiss)

  onUnmounted(() => {
    clearTimer()
    unsubscribe()
  })

  /** 可直接 v-bind 到元素的监听对象 */
  const handlers = {
    onMouseenter: show,
    onMouseleave: hide,
    onFocus: show,
    onBlur: hide,
    onTouchstart: () => {
      clearTimer()
      longPressTriggered = false
      timer = window.setTimeout(() => {
        longPressTriggered = true
        if (!suppressed) visible.value = true
        timer = null
      }, delay)
    },
    onTouchmove: clearTimer,
    onClick: () => {
      clearTimer()
    },
    onTouchend: (e: TouchEvent) => {
      clearTimer()
      if (longPressTriggered && e.cancelable) e.preventDefault()
      longPressTriggered = false
    },
    onTouchcancel: () => {
      clearTimer()
      longPressTriggered = false
    },
  }

  return { visible, handlers, show, dismiss }
}
