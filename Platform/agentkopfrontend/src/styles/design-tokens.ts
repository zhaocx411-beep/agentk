/**
 * Design Tokens
 *
 * 本文件是 DESIGN.md 的代码实现，所有 UI 都应通过此文件引用设计 token。
 * 修改设计时，先改 DESIGN.md，再同步修改本文件。
 *
 * @see DESIGN.md
 */

export const colors = {
  // 品牌色
  primary: '#4f46e5',
  primaryHover: '#6366f1',
  primaryPressed: '#4338ca',
  primaryFocus: '#4f46e5',
  primarySoft: '#e0e7ff',
  primarySubdued: '#f0eeff',
  /** 品牌渐变的紫色端，仅用于渐变与氛围背景 */
  primaryGradientEnd: '#7c3aed',

  // 语义色
  success: '#10b981',
  warning: '#f59e0b',
  error: '#ef4444',
  info: '#4f46e5',

  // 冷蓝色阶：展示型深色视觉（官方精选轮播）专用，靠明度拉层次
  azureAbyss: '#081226',
  azureDeep: '#14306b',
  azureRoyal: '#1d4ed8',
  azureSky: '#60a5fa',

  // 文字色
  ink: 'rgba(0, 0, 0, 0.88)',
  inkSecondary: 'rgba(0, 0, 0, 0.65)',
  inkTertiary: 'rgba(0, 0, 0, 0.45)',
  inkDisabled: 'rgba(0, 0, 0, 0.25)',

  // 表面色
  canvas: '#ffffff',
  canvasSoft: '#f7f8fa',
  surface1: '#ffffff',
  surface2: '#fafafa',
  surface3: '#f5f5f5',

  // 边框/分割线
  hairline: '#e5e7eb',
  hairlineStrong: '#d9d9d9',
  divider: '#f0f0f0',

  // 暗色主题预留
  darkCanvas: '#0a0a0c',
  darkSurface1: '#141416',
  darkSurface2: '#1c1c1f',
  darkInk: '#e8e8ec',
  darkHairline: '#2a2a30',
} as const;

export const typography = {
  displayXl: { fontSize: 48, fontWeight: 700, lineHeight: 1.1, letterSpacing: -1.5 },
  displayLg: { fontSize: 36, fontWeight: 700, lineHeight: 1.15, letterSpacing: -1.2 },
  displayMd: { fontSize: 28, fontWeight: 600, lineHeight: 1.2, letterSpacing: -0.8 },
  heading1: { fontSize: 24, fontWeight: 600, lineHeight: 1.25, letterSpacing: -0.5 },
  heading2: { fontSize: 20, fontWeight: 600, lineHeight: 1.3, letterSpacing: -0.3 },
  heading3: { fontSize: 18, fontWeight: 600, lineHeight: 1.4, letterSpacing: -0.2 },
  bodyLg: { fontSize: 16, fontWeight: 400, lineHeight: 1.5, letterSpacing: 0 },
  body: { fontSize: 14, fontWeight: 400, lineHeight: 1.5715, letterSpacing: 0 },
  bodySm: { fontSize: 13, fontWeight: 400, lineHeight: 1.5, letterSpacing: 0 },
  caption: { fontSize: 12, fontWeight: 400, lineHeight: 1.5, letterSpacing: 0 },
  button: { fontSize: 14, fontWeight: 500, lineHeight: 1.2, letterSpacing: 0 },
  eyebrow: { fontSize: 12, fontWeight: 600, lineHeight: 1.2, letterSpacing: 0.5 },
  mono: {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    fontSize: 13,
    fontWeight: 400,
    lineHeight: 1.5,
  },
} as const;

export const spacing = {
  xxs: 4,
  xs: 8,
  sm: 12,
  md: 16,
  lg: 24,
  xl: 32,
  xxl: 48,
  section: 64,
} as const;

export const rounded = {
  xs: 4,
  sm: 6,
  md: 8,
  lg: 12,
  xl: 16,
  pill: 9999,
} as const;

export const shadows = {
  none: 'none',
  sm: '0 1px 2px rgba(0, 0, 0, 0.04), 0 1px 3px rgba(0, 0, 0, 0.06)',
  md: '0 2px 8px rgba(0, 0, 0, 0.08)',
  lg: '0 4px 16px rgba(0, 0, 0, 0.12)',
  focus: `0 0 0 2px ${colors.primary}30`,
} as const;

export const motion = {
  durationFast: '0.15s',
  durationBase: '0.2s',
  durationSlow: '0.3s',
  easing: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
} as const;

/** 页面氛围背景：列表 / 体验页统一纯白 */
export const gradients = {
  pageAmbient: colors.canvas,
} as const;

/** 控件高度：搜索、筛选、按钮共用同一套控件语言 */
export const controls = {
  heightSm: 32,
  heightBase: 40,
  heightLg: 48,
} as const;

/** 工作台与主框架尺寸 */
export const layout = {
  contentMaxWidth: 1360,
  headerHeight: 48,
  siderWidth: 220,
  conversationHistoryWidth: 360,
  conversationContextWidth: 400,
} as const;

/** 交互反馈：focus 光圈与卡片 hover 抬升 */
export const interaction = {
  focusRing: `0 0 0 3px ${colors.primarySoft}`,
  focusRingNeutral: `0 0 0 2px ${colors.inkTertiary}`,
  cardLift: -2,
} as const;

/** 设计令牌类型定义 */
export type ColorToken = keyof typeof colors;
export type TypographyToken = keyof typeof typography;
export type SpacingToken = keyof typeof spacing;
export type RoundedToken = keyof typeof rounded;
export type ShadowToken = keyof typeof shadows;
export type ControlToken = keyof typeof controls;
export type LayoutToken = keyof typeof layout;

/** 便捷的 px 转换 */
export const px = (n: number) => `${n}px`;
