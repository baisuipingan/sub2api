/**
 * 统一主题管理
 * ============================================================
 * 改造前的问题（已修复）：
 *   · 主题逻辑在 main.ts / AppSidebar.vue / KeyUsageView.vue / HomeView.vue
 *     各写了一遍，共 4 份
 *   · 切换主题只改 <html> 的 class 和 localStorage，**不通知任何消费者**
 *   · 图表组件靠 `document.documentElement.classList.contains('dark')`
 *     在 computed 里读一次 —— 这不是响应式依赖，切主题后图表不重绘
 *
 * 现在的做法：
 *   · 全局单例 reactive state，所有消费者共享
 *   · 用 matchMedia 监听系统主题变化（之前完全没处理）
 *   · 用 MutationObserver 兜底同步外部对 <html class> 的改动
 *   · 图表改成 watch(isDark) 重绘，不再读 DOM
 */
import { computed, readonly, ref, watch } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'theme'

/** 系统是否偏好深色 */
function systemPrefersDark(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

/** 读取持久化的用户选择。老版本只存 'light' | 'dark'，需兼容。 */
function readStoredMode(): ThemeMode {
  if (typeof localStorage === 'undefined') return 'system'
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw
    // 老数据可能是 'dark'/'light'，已覆盖；其余一律回退 system
    return 'system'
  } catch {
    // 隐私模式下 localStorage 可能抛错
    return 'system'
  }
}

// ── 模块级单例（所有 import 方共享同一状态）──────────────
const mode = ref<ThemeMode>(readStoredMode())
const systemDark = ref(systemPrefersDark())

/** 最终生效的深色与否 */
const isDark = computed(() => {
  if (mode.value === 'system') return systemDark.value
  return mode.value === 'dark'
})

let initialized = false

/** 把状态同步到 <html class> 和 color-scheme */
function applyToDom(dark: boolean) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.classList.toggle('dark', dark)
  // 让原生控件（日期选择器、滚动条）跟随
  root.style.colorScheme = dark ? 'dark' : 'light'
}

/**
 * 初始化主题。应在 app.mount 之前调用，避免首屏闪烁。
 * 重复调用是安全的。
 */
export function initTheme(): void {
  if (initialized) return
  initialized = true

  // 1. 立即应用，避免 FOUC
  applyToDom(isDark.value)

  // 2. 状态变化 → 同步 DOM
  watch(isDark, (dark) => applyToDom(dark), { immediate: false })

  // 3. 系统主题变化（之前完全没处理）
  if (typeof window !== 'undefined' && window.matchMedia) {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => {
      systemDark.value = e.matches
    }
    // Safari < 14 只有 addListener
    if (mq.addEventListener) mq.addEventListener('change', onChange)
    else if (mq.addListener) mq.addListener(onChange)
  }

  // 4. 兜底：外部（如某段遗留代码）直接改了 <html class>，
  //    把状态回同步过来，避免 state 与 DOM 不一致
  if (typeof MutationObserver !== 'undefined') {
    const observer = new MutationObserver(() => {
      const domDark = document.documentElement.classList.contains('dark')
      if (domDark !== isDark.value) {
        // DOM 被外部改了，以 DOM 为准（不写 localStorage，避免覆盖用户选择）
        mode.value = domDark ? 'dark' : 'light'
      }
    })
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
  }
}

/** 设置主题偏好并持久化 */
export function setTheme(next: ThemeMode): void {
  mode.value = next
  try {
    localStorage.setItem(STORAGE_KEY, next)
  } catch {
    // 隐私模式下静默失败，主题仍在本次会话生效
  }
}

/** 在 light ↔ dark 之间切换（从 system 出发时，取当前反色） */
export function toggleTheme(): void {
  setTheme(isDark.value ? 'light' : 'dark')
}

/**
 * 组合式入口。组件里用：
 *   const { isDark, toggleTheme } = useTheme()
 *   watch(isDark, () => redrawChart())
 */
export function useTheme() {
  return {
    mode: readonly(mode),
    isDark,
    systemDark: readonly(systemDark),
    setTheme,
    toggleTheme,
  }
}

/**
 * 图表配色跟随主题。
 * 图表组件以前各自写 `document.documentElement.classList.contains('dark')`，
 * 现在统一从这里取，且是响应式的 —— watch 会触发重绘。
 */
export const chartTheme = computed(() => {
  const dark = isDark.value
  return {
    dark,
    text: dark ? '#dcdee5' : '#3b3d44',
    textMuted: dark ? '#7d8089' : '#7f828a',
    grid: dark ? 'rgba(255,255,255,0.08)' : 'rgba(194,199,210,0.55)',
    border: dark ? 'rgba(255,255,255,0.1)' : 'rgba(194,199,210,0.7)',
    // 数据系列色：靛蓝为主，其余从语义色取，保证全站一致
    series: [
      '#5865d4', // 靛蓝（主）
      '#3f8f6a', // 青绿（成功）
      '#b8802f', // 琥珀（警告）
      '#bf5c5c', // 玫红（危险）
      '#7d8aec', // 浅靛蓝
      '#4a9d78', // 浅青绿
    ],
    tooltipBg: dark ? '#2c3138' : '#edeff3',
    tooltipText: dark ? '#dcdee5' : '#25262b',
  }
})

// 提供给多处想直接读布尔值的地方（兼容旧写法）
export { isDark }
