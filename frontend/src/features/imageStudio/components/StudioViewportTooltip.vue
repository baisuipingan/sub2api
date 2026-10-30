<template>
  <!-- 隐藏锚点：用于测量触发元素位置 -->
  <span ref="anchorRef" class="hidden" aria-hidden="true" />
  <Teleport to="body">
    <div
      v-if="effectiveVisible"
      ref="tooltipRef"
      class="pointer-events-none fixed rounded-lg bg-gray-800 px-3 py-2 text-xs font-normal text-white shadow-lg"
      :class="className"
      :style="{
        left: `${position?.left ?? 0}px`,
        top: `${position?.top ?? 0}px`,
        visibility: position ? 'visible' : 'hidden',
        zIndex: 120,
      }"
    >
      <slot />
      <div
        class="absolute left-0 border-4 border-transparent"
        :class="
          position?.placement === 'bottom'
            ? 'bottom-full border-b-gray-800'
            : 'top-full border-t-gray-800'
        "
        :style="{
          left: `${position?.arrowLeft ?? 0}px`,
          transform: 'translateX(-50%)',
        }"
      />
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { onDismissTooltips } from '../lib/tooltipDismiss'

/**
 * 视口感知的 tooltip 气泡。
 *
 * 关键行为：
 * 1. 用 Teleport 挂到 body，避免被父级 overflow 裁切
 * 2. 自动判断上下方空间，空间不足翻转到下方
 * 3. 水平方向夹在视口边界内（margin 8px）
 * 4. 锚点被弹窗遮挡时（elementFromPoint 检测）不显示
 * 5. 全局 dismiss 总线可强制收起
 *
 * 迁移说明：原实现用 createPortal + useState 存位置，
 * 这里改用 Teleport + ref。定位算法逐行保留。
 */
const props = withDefaults(
  defineProps<{
    visible: boolean
    className?: string
  }>(),
  { className: '' },
)

const anchorRef = ref<HTMLSpanElement | null>(null)
const tooltipRef = ref<HTMLDivElement | null>(null)

interface Position {
  left: number
  top: number
  arrowLeft: number
  placement: 'top' | 'bottom'
}

const position = ref<Position | null>(null)
/** 被全局 dismiss 或点击外部时抑制，visible 回到 false 后复位 */
const suppressed = ref(false)

const effectiveVisible = computed(() => props.visible && !suppressed.value)

let unsubscribeDismiss: (() => void) | null = null

function bindDismiss() {
  unsubscribeDismiss?.()
  unsubscribeDismiss = onDismissTooltips(() => {
    suppressed.value = true
  })
}

watch(
  () => props.visible,
  (v) => {
    if (!v) {
      suppressed.value = false
      unsubscribeDismiss?.()
      unsubscribeDismiss = null
    } else {
      bindDismiss()
    }
  },
  { immediate: true },
)

/** 锚点中心是否仍可命中（未被弹窗等覆盖） */
function isAnchorExposed(anchor: HTMLElement, rect: DOMRect): boolean {
  const x = rect.left + rect.width / 2
  const y = rect.top + rect.height / 2
  if (x < 0 || x > window.innerWidth || y < 0 || y > window.innerHeight) return false
  const el = document.elementFromPoint(x, y)
  return Boolean(el && anchor.contains(el))
}

function updatePosition() {
  const anchor = anchorRef.value?.parentElement
  const el = tooltipRef.value
  if (!anchor || !el) return

  const margin = 8
  const gap = 8
  const anchorRect = anchor.getBoundingClientRect()
  if (!anchor.getClientRects().length || (anchorRect.width === 0 && anchorRect.height === 0)) {
    position.value = null
    return
  }
  if (!isAnchorExposed(anchor, anchorRect)) {
    position.value = null
    return
  }

  const tooltipRect = el.getBoundingClientRect()
  const anchorCenter = anchorRect.left + anchorRect.width / 2
  const maxLeft = Math.max(margin, window.innerWidth - tooltipRect.width - margin)
  const left = Math.min(Math.max(anchorCenter - tooltipRect.width / 2, margin), maxLeft)
  const aboveTop = anchorRect.top - tooltipRect.height - gap
  const placement: 'top' | 'bottom' = aboveTop >= margin ? 'top' : 'bottom'
  const top = placement === 'top' ? aboveTop : anchorRect.bottom + gap

  position.value = {
    left,
    top,
    arrowLeft: anchorCenter - left,
    placement,
  }
}

/** 点击/触摸外部时收起 */
function hideIfOutside(event: PointerEvent | TouchEvent) {
  const target = event.target
  const anchorParent = anchorRef.value?.parentElement
  if (!(target instanceof Node)) return
  if (anchorParent?.contains(target) || tooltipRef.value?.contains(target)) return
  suppressed.value = true
}

watch(
  effectiveVisible,
  async (on) => {
    if (!on) {
      position.value = null
      document.removeEventListener('pointerdown', hideIfOutside, true)
      document.removeEventListener('touchstart', hideIfOutside, true)
      return
    }

    document.addEventListener('pointerdown', hideIfOutside, true)
    document.addEventListener('touchstart', hideIfOutside, true)

    // 等下一帧再量，确保气泡已经渲染出来
    await nextTick()
    updatePosition()
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
  },
  { immediate: true },
)

onUnmounted(() => {
  unsubscribeDismiss?.()
  document.removeEventListener('pointerdown', hideIfOutside, true)
  document.removeEventListener('touchstart', hideIfOutside, true)
  window.removeEventListener('resize', updatePosition)
  window.removeEventListener('scroll', updatePosition, true)
})
</script>
