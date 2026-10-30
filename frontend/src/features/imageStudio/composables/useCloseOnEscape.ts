import { onUnmounted, watch } from 'vue'

/**
 * 全局 ESC 栈：每个模态注册时入栈，只有栈顶的 handler 会被调用。
 *
 * 为什么需要栈而不是简单的 addEventListener：工作台会同时叠多个弹窗
 * （详情 → 遮罩编辑 → 确认框），按 ESC 只能关最上面那个。
 * 各自独立监听会导致一次 ESC 关掉所有层。
 *
 * 迁移说明：原实现是 React 的 useEffect + useRef，这里改为 Vue 的
 * watch + 显式清理，语义保持一致。
 */
const escStack: Array<{ id: number; handler: () => void }> = []
let nextId = 0
let listenerAttached = false

function globalKeyDown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  if (escStack.length === 0) return
  e.preventDefault()
  escStack[escStack.length - 1].handler()
}

function ensureListener() {
  if (listenerAttached) return
  listenerAttached = true
  window.addEventListener('keydown', globalKeyDown)
}

export function useCloseOnEscape(
  enabled: () => boolean,
  onClose: () => void,
) {
  let currentId: number | null = null

  const unregister = () => {
    if (currentId === null) return
    const idx = escStack.findIndex((e) => e.id === currentId)
    if (idx !== -1) escStack.splice(idx, 1)
    currentId = null
  }

  const register = () => {
    unregister()
    ensureListener()
    const id = nextId++
    currentId = id
    escStack.push({ id, handler: onClose })
  }

  watch(
    enabled,
    (on) => {
      if (on) register()
      else unregister()
    },
    { immediate: true },
  )

  onUnmounted(unregister)
}
