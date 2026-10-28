/**
 * sub2api 设计 token · 新拟态（Neumorphism）
 * ============================================================
 * 单一事实来源。tailwind.config.js 从这里展开，避免散落改色。
 *
 * ── 三条铁律 ────────────────────────────────────────────────
 * 1. 元素背景 === 页面背景 #edeff3。不同色，浮雕立即失效
 * 2. 凸起外阴影 / 凹陷内阴影，方向永远「左上亮 · 右下暗」
 * 3. 按下 = 外阴影归零 + 内阴影显现（4 层同写，用 0 0 0 隐藏）
 *
 * ── 绝对不要做 ──────────────────────────────────────────────
 * ✗ 给卡片/输入框设白色背景 → 浮雕失效，退化成普通投影
 * ✗ 用 1px 硬边框描边 → 变成 Windows 时代控件
 * ✗ 未选中控件（开关/复选/单选）用与背景同色 → 完全不可见
 * ✗ 模糊/偏移比值 < 2 → 变成硬阴影，失去柔和感
 * ✗ 用纯黑 #000 / 纯白 #fff 做大面积底色
 *
 * ── 颜色生成方式 ────────────────────────────────────────────
 * 中性灰与靛蓝均在 OKLCH 空间生成：固定色相 H=275°，
 * 明度线性爬坡，色度中段最高、两端收敛。
 * 这样 11 阶之间的视觉差异是均匀的，不是手调出来的。
 */

// ══════════════════════════════════════════════════════════
// 表面色
// ══════════════════════════════════════════════════════════
export const SURFACE = {
  bg: '#edeff3',      // 页面 + 元素统一底色（铁律 1）
  light: '#ffffff',   // 左上高光
  dark: '#c2c7d2',    // 右下暗影
  sunken: '#e2e5eb',  // 未选中控件底色（开关、复选框）
  // 细长元素（进度条槽、细分割槽）内阴影渲染不出来，需要更深的底
  track: '#d8dbe2',
}

/** 深色模式表面色 */
export const SURFACE_DARK = {
  bg: '#1f2329',
  light: '#2c3138',
  dark: '#121519',
  sunken: '#191d22',
}

// ══════════════════════════════════════════════════════════
// 色板
// ══════════════════════════════════════════════════════════

/** 中性灰：带靛蓝微染（chroma .004~.014），不是纯灰 */
const gray = {
  50: '#f7f8fb',
  100: '#f0f1f5',
  200: '#e2e4ea',
  300: '#cfd1d8',
  400: '#aeb1ba',
  500: '#8f929b',
  600: '#676970',   // 辅助文字下限 4.76:1
  700: '#565860',   // 次要文字 6.16:1
  800: '#3b3d44',
  900: '#25262b',   // 主文字 13.12:1
  950: '#101115',
}

/** 深色模式中性灰 */
const dark = {
  50: '#f0f1f6',
  100: '#dcdee5',
  200: '#bec0c9',
  300: '#9c9ea7',
  400: '#7d8089',
  500: '#61636b',
  600: '#45474f',
  700: '#2e3037',
  800: '#1f2025',
  900: '#16171b',
  950: '#0b0c0f',
}

/** 靛蓝：OKLCH H=275°，500 已校准到白字 4.97:1 */
const primary = {
  50: '#f4f7ff',
  100: '#e8edff',
  200: '#d1daff',
  300: '#acb9fb',
  400: '#7d8aec',
  500: '#5865d4',
  600: '#515bc3',
  700: '#40499d',
  800: '#333a7c',
  900: '#292f64',
  950: '#171c3b',
}

/** 语义色：低饱和，压在软胶里 */
const emerald = {
  50: '#eff9f4', 100: '#d9f0e5', 200: '#b3e2cc', 300: '#84ceac',
  400: '#5fb891', 500: '#4a9d78', 600: '#3f8f6a', 700: '#2f7153',
  800: '#275c44', 900: '#224c39', 950: '#11291f',
}
const amber = {
  50: '#fdf8ef', 100: '#faedd6', 200: '#f4d9a9', 300: '#eabf74',
  400: '#d9a95e', 500: '#b8802f', 600: '#a2712a', 700: '#91631f',
  800: '#77511b', 900: '#624318', 950: '#35230b',
}
const red = {
  50: '#fdf3f3', 100: '#fbe2e2', 200: '#f6c7c7', 300: '#eda4a4',
  400: '#d78a8a', 500: '#c96b6b', 600: '#bf5c5c', 700: '#9c4646',
  800: '#833c3c', 900: '#6e3434', 950: '#3c1818',
}

export const colors = {
  gray,
  dark,
  primary,

  // 原皮肤的 teal 主色（全站 56 处残留）→ 映射到靛蓝，避免换肤后留青斑
  teal: primary,

  // 覆盖 Tailwind 内置语义色，全站跟着变
  emerald,
  green: emerald,
  amber,
  orange: amber,
  red,
  rose: red,

  // 原皮肤用过的 sky / indigo / purple 一并收进靛蓝系
  blue: primary,
  sky: primary,
  indigo: primary,
  purple: primary,
  violet: primary,
  cyan: primary,

  // accent 在原皮肤里与 dark 完全重复且几乎未使用，这里指向中性灰
  accent: gray,
}

// ══════════════════════════════════════════════════════════
// 阴影：21 组
// 偏移与模糊同步收紧，模糊/偏移 ≈ 2.6~3.0 保持柔和
// ══════════════════════════════════════════════════════════
export const boxShadow = {
  /*
   * ── 凸起：按元素尺寸分级 ──
   * 规则：阴影偏移 ≈ 元素尺寸的 8~12%，模糊 ≈ 偏移 × 2.6
   * 元素越小，相对偏移必须越大，否则新拟态完全看不出来。
   *   neu-xs  ≤ 24px 的方块/图标底
   *   neu-sm  ~38px  的小按钮、徽章容器
   *   neu     ~60px+ 的按钮、输入框、小卡片
   *   neu-lg  ~200px+ 的卡片、面板
   *   neu-xl  ~400px+ 的大面板
   */
  'neu-xs': `-2px -2px 5px ${SURFACE.light}, 2px 2px 6px ${SURFACE.dark}`,
  'neu-sm': `-4px -4px 9px ${SURFACE.light}, 4px 4px 10px ${SURFACE.dark}`,
  'neu': `-6px -6px 14px ${SURFACE.light}, 6px 6px 16px ${SURFACE.dark}`,
  'neu-lg': `-9px -9px 24px ${SURFACE.light}, 9px 9px 26px ${SURFACE.dark}`,
  'neu-xl': `-13px -13px 34px ${SURFACE.light}, 13px 13px 36px ${SURFACE.dark}`,

  /* ── 凹陷：同样按尺寸分级 ── */
  'neu-in-xs': `inset -1.5px -1.5px 4px ${SURFACE.light}, inset 1.5px 1.5px 5px ${SURFACE.dark}`,
  'neu-in-sm': `inset -3px -3px 8px ${SURFACE.light}, inset 3px 3px 9px ${SURFACE.dark}`,
  'neu-in': `inset -5px -5px 12px ${SURFACE.light}, inset 5px 5px 13px ${SURFACE.dark}`,
  'neu-in-lg': `inset -7px -7px 18px ${SURFACE.light}, inset 7px 7px 20px ${SURFACE.dark}`,

  /*
   * ── 4 层（凹凸同体，按pressable 用）──
   * 默认态：外层生效、内层 0 0 0 隐藏
   * 按下态：外层归零、内层生效
   * 两组同写才能靠 transition 平滑过渡
   */
  'neu-4': [
    `-4px -4px 10px ${SURFACE.light}`,
    `4px 4px 11px ${SURFACE.dark}`,
    `inset 0 0 0 ${SURFACE.light}`,
    `inset 0 0 0 ${SURFACE.dark}`,
  ].join(', '),
  'neu-4-in': [
    `0 0 0 ${SURFACE.light}`,
    `0 0 0 ${SURFACE.dark}`,
    `inset -3px -3px 9px ${SURFACE.light}`,
    `inset 3px 3px 10px ${SURFACE.dark}`,
  ].join(', '),

  /* ── 主色凸起 ── */
  'neu-pri': [
    '-4px -4px 10px rgba(255,255,255,0.72)',
    '4px 4px 11px rgba(160,167,182,0.9)',
    'inset 1px 1px 2px rgba(255,255,255,0.32)',
    'inset -1px -1px 3px rgba(50,62,150,0.32)',
  ].join(', '),
  'neu-pri-in': [
    'inset -2px -2px 7px rgba(255,255,255,0.25)',
    'inset 3px 3px 9px rgba(42,52,130,0.5)',
  ].join(', '),

  /* ── 浮层：比卡片更深的环境投影 ── */
  'neu-overlay': [
    '-9px -9px 24px rgba(255,255,255,0.7)',
    '11px 11px 30px rgba(178,184,196,0.85)',
  ].join(', '),

  /*
   * ── 兼容层：原皮肤已有的类名，值全部换成新拟态 ──
   * 保留是为了不破坏 319 个组件里已写好的 class
   */
  'glass': `-3px -3px 8px ${SURFACE.light}, 3px 3px 9px ${SURFACE.dark}`,
  'glass-sm': `-2px -2px 6px ${SURFACE.light}, 2px 2px 7px ${SURFACE.dark}`,
  'glow': '0 0 0 3px rgba(88,101,212,0.16)',
  'glow-lg': '0 0 0 4px rgba(88,101,212,0.2)',
  'card': `-5px -5px 13px ${SURFACE.light}, 5px 5px 14px ${SURFACE.dark}`,
  'card-hover': `-9px -9px 24px ${SURFACE.light}, 9px 9px 26px ${SURFACE.dark}`,
  'inner-glow': 'inset 0 1px 0 rgba(255,255,255,0.8)',
  'inset': `inset -4px -4px 11px ${SURFACE.light}, inset 4px 4px 12px ${SURFACE.dark}`,
  'overlay': `-9px -9px 24px ${SURFACE.light}, 9px 9px 26px ${SURFACE.dark}`,
  'pressed': `inset -3px -3px 9px ${SURFACE.light}, inset 3px 3px 10px ${SURFACE.dark}`,
  'gold': `-4px -4px 10px rgba(255,255,255,0.72), 4px 4px 11px rgba(160,167,182,0.9)`,
}

// ══════════════════════════════════════════════════════════
// 圆角：收敛版（比初版更硬朗）
// ══════════════════════════════════════════════════════════
export const borderRadius = {
  sm: '6px',
  DEFAULT: '7px',
  md: '8px',
  lg: '10px',
  xl: '13px',
  '2xl': '16px',
  '3xl': '20px',
  '4xl': '24px',
}

// ══════════════════════════════════════════════════════════
// 渐变：新拟态几乎不用渐变，只保留主色按钮与进度条
// ══════════════════════════════════════════════════════════
export const backgroundImage = {
  'neu-pri': 'linear-gradient(145deg, #5f6cd8, #4a56c0)',
  'neu-pri-hover': 'linear-gradient(145deg, #6b78e0, #4a56c0)',
  'neu-pri-active': 'linear-gradient(145deg, #4a56c0, #4149b0)',
  'neu-bar': 'linear-gradient(90deg, #acb9fb, #5865d4)',
  'neu-bar-ok': 'linear-gradient(90deg, #6dbb95, #3f8f6a)',
  'neu-bar-warn': 'linear-gradient(90deg, #d9a95e, #b8802f)',
  'neu-bar-bad': 'linear-gradient(90deg, #d78a8a, #bf5c5c)',
  'gradient-primary': 'linear-gradient(145deg, #5f6cd8, #4a56c0)',

  // 原皮肤的渐变在新拟态下会显脏，全部置空
  'gradient-radial': 'none',
  'gradient-dark': 'none',
  'gradient-glass': 'none',
  'mesh-gradient': 'none',
}
