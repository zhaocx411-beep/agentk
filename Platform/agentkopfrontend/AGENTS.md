# AGENTS.md — AgentKOpFrontend AI Agent 行为指南

> 本文件用于约束所有 AI 编码 Agent（Claude Code / Cursor / CodeFlicker 等）在本项目中的行为。
> **任何 UI 生成、组件开发、样式调整都必须遵循此指南。**

---

## 📐 项目概述

- **项目**：可灵 Agent 管理平台（AgentKOpFrontend）
- **技术栈**：React 18 + TypeScript + Vite 5 + Ant Design 5 + Less + Zustand + React Query
- **类型**：B 端后台管理系统
- **包管理器**：pnpm

---

## 🎨 设计规范遵循（强制）

### 必读文件

在生成任何 UI 代码前，**必须**先读取以下文件：

1. **`DESIGN.md`**（项目根目录）—— 完整的设计规范：颜色、排版、组件、间距、护栏
2. **`src/styles/design-tokens.ts`** —— 设计 token 的 TypeScript 实现
3. **`src/styles/theme.ts`** —— Ant Design 主题配置
4. **`src/styles/variables.less`** —— Less 全局变量

### 强制规则

1. **颜色**：只能使用 `DESIGN.md` 中 `colors:` 段定义的颜色，通过 `design-tokens.ts` 引用，禁止硬编码 hex 值
2. **排版**：使用 `DESIGN.md` 中 `typography:` 段定义的字号、字重、行高、字间距
3. **组件**：按钮、卡片、输入框等必须遵循 `DESIGN.md` 中 `components:` 段的定义
4. **间距**：使用 `DESIGN.md` 中 `spacing:` 段的 token，不使用任意像素值
5. **圆角**：使用 `DESIGN.md` 中 `rounded:` 段的 token
6. **Do's & Don'ts**：`DESIGN.md` 末尾的禁止项绝对不能违反

### 引用方式

```tsx
// ✅ 正确：通过 token 引用
import { colors, spacing, typography } from '@/styles/design-tokens';

<Button style={{ background: colors.primary, borderRadius: spacing.md }}>
  按钮
</Button>

// ❌ 错误：硬编码
<Button style={{ background: '#4f46e5', borderRadius: '8px' }}>按钮</Button>
```

```less
// ✅ 正确：使用 Less 变量
.card {
  background: @surface-1;
  border-radius: @border-radius-lg;
  padding: @spacing-lg;
}

// ❌ 错误：硬编码
.card {
  background: #ffffff;
  border-radius: 12px;
  padding: 24px;
}
```

---

## 📁 项目结构约定

### 新增页面

```
src/pages/YourPage/
├── index.tsx              # 页面主组件
├── YourPage.module.less   # 页面样式（CSS Modules + Less）
├── api.ts                 # 接口请求
├── types.ts               # 类型定义
└── components/            # 页面级子组件
```

### 新增通用组件

```
src/components/YourComponent/
├── index.tsx
└── YourComponent.module.less
```

然后在 `src/components/index.ts` 中导出。

### 样式规范

- **使用 CSS Modules + Less**：文件名 `[ComponentName].module.less`
- **禁止内联样式**：除动态值外，所有样式写入 `.module.less`
- **禁止全局样式**：除 `src/styles/global.less` 外，不写全局选择器
- **响应式**：使用 `src/hooks/useResponsive.ts` 处理断点

---

## 🔧 代码规范

### TypeScript

- **必须启用严格类型**：禁止 `any`，必须给出明确类型
- **接口定义**：放在 `types.ts` 中，业务类型用 `interface`，联合类型用 `type`
- **API 请求**：使用 `src/utils/request.ts` 封装的 axios 实例
- **状态管理**：全局状态用 Zustand（`src/store/`），服务端状态用 React Query

### React

- **函数组件 + Hooks**：禁止 class 组件
- **Props 类型**：必须定义 `Props` interface
- **副作用**：`useEffect` 必须写明依赖项
- **组件导出**：使用命名导出，不用默认导出

### Ant Design

- **优先使用 Ant Design 组件**：除非有定制需求，不要重写基础组件
- **主题定制**：通过 `src/styles/theme.ts` 的 `ConfigProvider`，不通过 `!important` 覆盖
- **图标**：使用 `@ant-design/icons`，SVG 自定义图标放在 `src/assets/icons/`

---

## 🚫 禁止行为

1. ❌ 引入新的 UI 组件库（如 MUI、Chakra、Element Plus 等）—— 只用 Ant Design
2. ❌ 引入 Tailwind CSS 或其他原子化 CSS 框架
3. ❌ 在 `.tsx` 中写超过 3 行的内联 style
4. ❌ 硬编码颜色 hex 值（必须走 token）
5. ❌ 在组件文件中写超过 200 行代码（必须拆分）
6. ❌ 修改 `src/styles/global.less` 除非真的需要全局调整

---

## ✅ 推荐做法

1. ✅ 写新页面前，先参考 `src/pages/_template/` 的模板
2. ✅ 复用 `src/components/` 中的 `BaseTable`、`SearchForm`、`PageContainer`
3. ✅ 复用 `src/hooks/` 中的 `useTable`、`useModal`、`useRequest`
4. ✅ 接口请求必须通过 React Query 包装
5. ✅ 提交前运行 `pnpm lint && pnpm lint:style && pnpm type-check`

---

## 🤝 协作流程

### 当你（AI Agent）接到 UI 任务时

1. **先读** `DESIGN.md` → 了解设计规范
2. **再读** `src/styles/design-tokens.ts` → 了解代码中的 token
3. **参考** `src/pages/_template/` → 了解项目代码风格
4. **生成代码** → 严格使用 token，不硬编码
5. **自查** → 对照 `DESIGN.md` 的 Do's & Don'ts 检查
6. **交付** → 说明使用了哪些 token，符合哪些规范点

### 当需要调整设计时

1. **先修改** `DESIGN.md`（设计规范源头）
2. **同步修改** `src/styles/design-tokens.ts`（代码 token）
3. **同步修改** `src/styles/variables.less`（Less 变量）
4. **同步修改** `src/styles/theme.ts`（Ant Design 主题）
5. **测试** → 运行 `pnpm dev` 检查全局效果

---

## 📝 Git 提交规范

```
<type>(<scope>): <subject>
```

- type：`feat` / `fix` / `docs` / `style` / `refactor` / `perf` / `chore`
- scope：业务模块名（如 `agent` / `workflow` / `playground`）
- subject：简洁描述

**示例**：

```
feat(agent): 新增 Agent 列表页面，遵循 DESIGN.md 的 Notion 风格
fix(request): 修复 401 重复跳转问题
docs(design): 更新 DESIGN.md 的颜色 token
```

---

## 🎯 核心原则

> **DESIGN.md 是设计源头，代码是设计实现。**
>
> 任何视觉相关的改动，先改 DESIGN.md，再改代码。AI Agent 在每次生成 UI 前必须先读 DESIGN.md，否则生成的代码视为不符合规范。
