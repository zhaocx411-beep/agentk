---
version: alpha
name: AgentKOpFrontend-design-system
description: 可灵 Agent 管理平台的设计系统。基于 indigo 主色（#4f46e5）构建的 B 端后台 SaaS 风格，强调清晰的信息层级、克制的色彩使用、Ant Design 组件深度定制。整体偏 Linear/Notion 风格的极简 SaaS 美学，但保留后台系统所需的密度与可读性。

colors:
  primary: '#4f46e5'
  primary-hover: '#6366f1'
  primary-pressed: '#4338ca'
  primary-focus: '#4f46e5'
  primary-soft: '#e0e7ff'
  primary-subdued: '#f0eeff'
  on-primary: '#ffffff'
  # 品牌渐变的紫色端，仅用于渐变与氛围背景，不作为独立强调色
  primary-gradient-end: '#7c3aed'

  success: '#10b981'
  warning: '#f59e0b'
  error: '#ef4444'
  info: '#4f46e5'

  # 冷蓝色阶：展示型深色视觉（官方精选轮播）专用，靠明度而非色相拉开层次。
  # 用于替代原先借 warning 橙做的渐变——高饱和暖色在大面积渐变上显廉价。
  azure-abyss: '#081226'
  azure-deep: '#14306b'
  azure-royal: '#1d4ed8'
  azure-sky: '#60a5fa'

  ink: 'rgba(0, 0, 0, 0.88)'
  ink-secondary: 'rgba(0, 0, 0, 0.65)'
  ink-tertiary: 'rgba(0, 0, 0, 0.45)'
  ink-disabled: 'rgba(0, 0, 0, 0.25)'

  canvas: '#ffffff'
  canvas-soft: '#f7f8fa'
  surface-1: '#ffffff'
  surface-2: '#fafafa'
  surface-3: '#f5f5f5'

  hairline: '#e5e7eb'
  hairline-strong: '#d9d9d9'
  divider: '#f0f0f0'

typography:
  display-xl:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 48px
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: -1.5px
  display-lg:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 36px
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: -1.2px
  display-md:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 28px
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: -0.8px
  heading-1:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 24px
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: -0.5px
  heading-2:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 20px
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: -0.3px
  heading-3:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 18px
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: -0.2px
  body-lg:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.5
  body:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.5715
  body-sm:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.5
  caption:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.5
  button:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.2
  eyebrow:
    fontFamily: 'Inter, system-ui, -apple-system, sans-serif'
    fontSize: 12px
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: 0.5px
  mono:
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.5

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  pill: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 48px
  section: 64px

gradients:
  # 列表 / 体验页氛围背景：统一纯白，不再叠加左右角晕染
  page-ambient: '{colors.canvas}'

layout:
  # 列表页内容最大宽度，超宽屏下保持可读行长与网格列宽
  content-max-width: 1360px
  header-height: 48px
  sider-width: 220px

controls:
  # 控件高度：同一套搜索/筛选/按钮语言，禁止各页自定高度
  height-sm: 32px
  height-base: 40px
  height-lg: 48px

interaction:
  # 可聚焦控件统一 focus 光圈
  focus-ring: '0 0 0 3px {colors.primary-soft}'
  # 卡片 hover 抬升位移，仅用于表达可点击
  card-lift: -2px

components:
  button-primary:
    backgroundColor: '{colors.primary}'
    textColor: '{colors.on-primary}'
    typography: '{typography.button}'
    rounded: '{rounded.md}'
    padding: 8px 16px
  button-primary-hover:
    backgroundColor: '{colors.primary-hover}'
    textColor: '{colors.on-primary}'
    typography: '{typography.button}'
    rounded: '{rounded.md}'
  button-primary-pressed:
    backgroundColor: '{colors.primary-pressed}'
    textColor: '{colors.on-primary}'
    typography: '{typography.button}'
    rounded: '{rounded.md}'
  button-secondary:
    backgroundColor: '{colors.canvas}'
    textColor: '{colors.primary}'
    typography: '{typography.button}'
    rounded: '{rounded.md}'
    padding: 8px 16px
  button-ghost:
    backgroundColor: 'transparent'
    textColor: '{colors.ink}'
    typography: '{typography.button}'
    rounded: '{rounded.md}'
    padding: 8px 16px
  card-default:
    backgroundColor: '{colors.surface-1}'
    textColor: '{colors.ink}'
    typography: '{typography.body}'
    rounded: '{rounded.lg}'
    padding: 24px
    border: 1px solid {colors.hairline}
  card-elevated:
    backgroundColor: '{colors.surface-1}'
    textColor: '{colors.ink}'
    typography: '{typography.body}'
    rounded: '{rounded.lg}'
    padding: 24px
    shadow: { shadows.sm }
  text-input:
    backgroundColor: '{colors.canvas}'
    textColor: '{colors.ink}'
    typography: '{typography.body}'
    rounded: '{rounded.md}'
    padding: 8px 12px
    border: 1px solid {colors.hairline}
  text-input-focused:
    backgroundColor: '{colors.canvas}'
    textColor: '{colors.ink}'
    typography: '{typography.body}'
    rounded: '{rounded.md}'
    padding: 8px 12px
    border: 1px solid {colors.primary}
  tag-default:
    backgroundColor: '{colors.surface-3}'
    textColor: '{colors.ink-secondary}'
    typography: '{typography.caption}'
    rounded: '{rounded.xs}'
    padding: 2px 8px
  tag-primary:
    backgroundColor: '{colors.primary-subdued}'
    textColor: '{colors.primary}'
    typography: '{typography.caption}'
    rounded: '{rounded.xs}'
    padding: 2px 8px
  table-row:
    backgroundColor: '{colors.canvas}'
    textColor: '{colors.ink}'
    typography: '{typography.body}'
    padding: 12px 16px
    border-bottom: 1px solid {colors.hairline}
  table-row-hover:
    backgroundColor: '{colors.surface-3}'
    textColor: '{colors.ink}'
  table-header:
    backgroundColor: '{colors.canvas}'
    textColor: '{colors.ink-secondary}'
    typography: '{typography.body-sm}'
    padding: 12px 16px
    border-bottom: 1px solid {colors.hairline}
  sidebar-nav:
    backgroundColor: '{colors.canvas}'
    textColor: '{colors.ink-secondary}'
    typography: '{typography.body}'
    padding: 8px 12px
    rounded: '{rounded.md}'
  sidebar-nav-active:
    backgroundColor: '{colors.primary-subdued}'
    textColor: '{colors.primary}'
    typography: '{typography.body}'
    padding: 8px 12px
    rounded: '{rounded.md}'
  page-container:
    backgroundColor: '{colors.canvas}'
    padding: 24px 32px
  scrollbar-thin:
    width: 4px
    thumbColor: '{colors.hairline-strong}'
    thumbHoverColor: '{colors.ink-tertiary}'
    thumbRounded: '{rounded.pill}'
    trackColor: 'transparent'
---

# AgentKOpFrontend Design System

> 本设计系统定义了可灵 Agent 管理平台的视觉规范。所有 UI 生成、组件开发、样式调整都以此为唯一权威。

## Overview

可灵 Agent 管理平台是 B 端后台系统，设计目标：**清晰、高效、克制**。整体采用亮色 SaaS 风格，参考 Linear 的极简密度 + Notion 的温暖可读性，主色使用 indigo `#4f46e5` 作为唯一品牌强调色，其余中性色阶梯式分布。

## Key Characteristics

- **单一品牌色**：indigo `#4f46e5` 只用于 CTA、链接、选中态、focus ring，不做装饰
- **亮色优先**：canvas 用纯白 `#ffffff`，layout 背景用 `#f7f8fa` 形成层次
- **三档表面**：canvas → surface-1（白）→ surface-2（#fafafa）→ surface-3（#f5f5f5）
- **Ant Design 深度定制**：通过 ConfigProvider 统一所有组件 token，不用 `!important` 覆盖
- **CSS Modules + Less**：样式隔离 + 全局变量双轨制

## Colors

### Brand & Accent

- **Primary** `#4f46e5`：CTA 按钮、链接、选中态、focus ring
- **Primary Hover** `#6366f1`：按钮 hover
- **Primary Pressed** `#4338ca`：按钮按下
- **Primary Subdued** `#f0eeff`：选中项背景、tag 背景

### Text

- **Ink** `rgba(0,0,0,0.88)`：正文与标题
- **Ink Secondary** `rgba(0,0,0,0.65)`：辅助文字
- **Ink Tertiary** `rgba(0,0,0,0.45)`：placeholder、meta
- **Ink Disabled** `rgba(0,0,0,0.25)`：禁用态

### Surface

- **Canvas** `#ffffff`：卡片、输入框、侧边栏背景
- **Canvas Soft** `#f7f8fa`：页面 layout 背景
- **Surface 3** `#f5f5f5`：hover 态、tag 默认背景

## Typography

- **Font Family**：Inter（首选），fallback `system-ui, -apple-system`
- **Body Size**：14px（与 Ant Design 对齐）
- **Display**：24-48px，负字间距 -0.5px ~ -1.5px
- **Mono**：用于代码、ID、调试信息

## Components

### Buttons

- **Primary**：indigo 背景 + 白字，圆角 8px，padding `8px 16px`
- **Secondary**：白底 + indigo 字 + indigo 边框
- **Ghost**：透明背景 + ink 字
- 圆角统一 8px，不使用 pill（除非状态标签）

### Cards

- 白底 + 1px hairline 边框 + 12px 圆角 + 24px padding
- 不使用阴影除非需要明确层级
- `ask_user` 问卷作为对话内容追加到消息流末尾，不设独立高度上限，随对话区域整体滚动
- `ask_user` 提交后保留在原对话位置，以只读状态展示已选答案与提交结果

### Tables

- Header：浅灰背景 + 次级文字色
- Row：白底 + 1px 底边线
- Hover：surface-3 背景

### Sidebar Nav

- 默认：白底 + 次级文字
- Active：primary-subdued 背景 + primary 文字
- Hover：surface-3 背景

### Scrollbar

全项目统一使用**细滚动条**，由 `src/styles/global.less` 全局内置，覆盖所有可滚动容器（含 antd Table / Modal / Drawer / 代码块）：

- 宽度：4px（WebKit），Firefox 用 `scrollbar-width: thin`
- 滑块：`hairline-strong` `#d9d9d9`，pill 圆角，hover 加深为 `ink-tertiary`
- 轨道：透明，不占额外背景
- 组件内**不需要**再写任何 `::-webkit-scrollbar` / `scrollbar-width` / `scrollbar-color` 样式

## Do's and Don'ts

### Do

- 使用 `design-tokens.ts` 的 token 引用颜色/间距/圆角
- 优先复用 `src/components/` 现有组件
- 新页面参考 `src/pages/_template/`
- 滚动条直接使用 global.less 内置的全局细滚动条，无需额外处理
- 提交前跑 `pnpm lint && pnpm lint:style && pnpm type-check`

### Don't

- 不要硬编码 hex 颜色值
- 不要引入新的 UI 库（只用 Ant Design）
- 不要使用 Tailwind 或原子化 CSS
- 不要在 .tsx 中写超过 3 行内联样式
- 不要用 `!important` 覆盖 Ant Design 样式
- 不要在组件样式里重复定义滚动条（宽度、颜色、圆角）

## Agent Prompt Guide

当你（AI Agent）需要在本项目生成 UI 时，使用以下 prompt 模板：

```
参照 DESIGN.md 的设计规范，为 [功能名] 生成 UI。
要求：
1. 颜色使用 design-tokens.ts 中的 colors token
2. 间距使用 spacing token
3. 组件优先用 Ant Design
4. 样式用 CSS Modules + Less，文件名 [Name].module.less
5. 页面结构参考 src/pages/_template/
6. 不要硬编码任何 hex 值
```
