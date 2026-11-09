# 移动端响应式布局适配方案

## 概述

基于项目现有技术栈（Less + CSS Modules + Antd Grid），用 **纯 CSS 媒体查询 + 布局组件改造** 实现响应式，零新增依赖。

---

## 断点规范

| Token        | 值    | 场景                       |
| ------------ | ----- | -------------------------- |
| `@screen-xs` | 480px | 小手机                     |
| `@screen-sm` | 576px | 大手机                     |
| `@screen-md` | 768px | 平板                       |
| `@screen-lg` | 992px | 小桌面（与 antd 断点对齐） |

---

## 改动清单

### 1. 全局变量 `variables.less` — 新增断点变量 & 移动间距

```less
// --- 响应式断点 ---
@screen-xs: 480px;
@screen-sm: 576px;
@screen-md: 768px;
@screen-lg: 992px;

// --- 移动端间距 ---
@spacing-mobile: 12px;
```

### 2. 新增 `useResponsive` hook

提供 `isMobile` 状态（`<768px`），布局组件据此切换模式。

```ts
// src/hooks/useResponsive.ts
export function useResponsive() {
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768);
  // 用 matchMedia 监听，返回 { isMobile }
}
```

### 3. MainLayout 改造 — 移动端侧边栏变 Drawer

- `isMobile` 时隐藏固定侧边栏，使用 antd `<Drawer>` 承载菜单
- Header 点击汉堡按钮打开/关闭 Drawer
- 点击菜单项后自动关闭 Drawer

### 4. Sider 样式适配

```less
// Sider.module.less 新增
@media (max-width: @screen-md) {
  .sider {
    display: none;
  } // 固定侧边栏隐藏，由 Drawer 接管
}
```

### 5. Header 样式适配

```less
@media (max-width: @screen-md) {
  .header {
    padding: 0 @spacing-mobile;
  }
}
```

### 6. Content 区域适配

```less
@media (max-width: @screen-md) {
  .content {
    margin: @spacing-sm;
    padding: @spacing-mobile;
  }
}
```

### 7. SearchForm 适配

移动端表单从 `inline` 布局改为垂直堆叠：

```less
@media (max-width: @screen-sm) {
  .searchForm {
    :global(.ant-form-inline .ant-form-item) {
      width: 100%;
      margin-right: 0;
      margin-bottom: @spacing-sm;
    }
  }
}
```

### 8. BaseTable 适配

表格容器加 `overflow-x: auto` 支持横向滚动：

```less
@media (max-width: @screen-md) {
  .tableWrapper {
    overflow-x: auto;
  }
}
```

### 9. PageContainer 适配

标题与操作按钮在小屏上换行：

```less
@media (max-width: @screen-sm) {
  .header {
    flex-direction: column;
    align-items: flex-start;
    gap: @spacing-sm;
  }
}
```

### 10. Home 首页 — 已有 antd Grid 断点（`xs={12} sm={6}`），无需额外改动

---

## 文件变动汇总

| 文件                                                          | 操作             |
| ------------------------------------------------------------- | ---------------- |
| `src/styles/variables.less`                                   | 新增断点变量     |
| `src/hooks/useResponsive.ts`                                  | 新建             |
| `src/hooks/index.ts`                                          | 导出新 hook      |
| `src/layouts/MainLayout/index.tsx`                            | Drawer 逻辑      |
| `src/layouts/MainLayout/MainLayout.module.less`               | 移动端样式       |
| `src/layouts/MainLayout/components/Sider/index.tsx`           | 支持 Drawer 模式 |
| `src/layouts/MainLayout/components/Sider/Sider.module.less`   | 隐藏规则         |
| `src/layouts/MainLayout/components/Header/index.tsx`          | 汉堡按钮         |
| `src/layouts/MainLayout/components/Header/Header.module.less` | 移动端间距       |
| `src/components/SearchForm/SearchForm.module.less`            | 表单堆叠         |
| `src/components/BaseTable/BaseTable.module.less`              | 横向滚动         |
| `src/components/PageContainer/PageContainer.module.less`      | 标题换行         |

---

## 不引入新依赖

完全利用项目已有的 Less + CSS Modules + antd 组件，无需 postcss 插件或新包。
