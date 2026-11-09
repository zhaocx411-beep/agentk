# 可灵Agent管理平台

基于 React 18 + Vite 5 + TypeScript 的后台管理前端脚手架。

## 🚀 快速开始

### 环境要求

- Node.js >= 18
- npm >= 9

### 安装依赖

```bash
npm install
```

### 启动开发环境

```bash
npm run dev
```

访问 http://localhost:3000

### 构建

```bash
# 测试环境
npm run build:test

# 生产环境
npm run build:prod
```

### 代码检查

```bash
# ESLint 检查
npm run lint

# Stylelint 检查
npm run lint:style

# 格式化代码
npm run format

# 类型检查
npm run type-check
```

---

## 📁 目录结构

```
src/
├── api/                  # 接口请求模块（按业务拆分）
├── assets/               # 静态资源
│   └── icons/            # SVG 图标目录
├── components/           # 通用公共组件
│   ├── BaseTable/        # 基础表格
│   ├── PageContainer/    # 页面容器
│   ├── SearchForm/       # 搜索表单
│   └── SvgIcon/          # SVG 图标组件
├── hooks/                # 通用 Hooks
│   ├── useRequest.ts     # 请求封装
│   ├── useModal.ts       # 弹窗控制
│   └── useTable.ts       # 表格状态
├── layouts/              # 布局组件
│   └── MainLayout/       # 后台主布局
├── pages/                # 页面组件
│   ├── Home/             # 首页
│   ├── Login/            # 登录页
│   └── NotFound/         # 404 页面
├── router/               # 路由配置
│   ├── index.tsx         # 路由表
│   └── AuthGuard.tsx     # 路由守卫
├── store/                # Zustand 状态管理
│   ├── user.ts           # 用户状态
│   └── app.ts            # 全局应用状态
├── styles/               # 全局样式
│   ├── global.less       # 全局重置样式
│   ├── variables.less    # Less 变量
│   └── theme.ts          # Ant Design 主题配置
├── types/                # TypeScript 类型定义
│   ├── api.ts            # 接口类型
│   └── global.ts         # 全局类型
├── utils/                # 工具函数
│   └── request.ts        # Axios 请求封装
├── App.tsx               # 根组件
└── main.tsx              # 入口文件
```

---

## 🌍 环境变量说明

| 变量                | 说明         | 开发环境              | 测试环境                          | 生产环境                     |
| ------------------- | ------------ | --------------------- | --------------------------------- | ---------------------------- |
| `VITE_APP_TITLE`    | 应用标题     | 可灵Agent管理平台     | 可灵Agent管理平台                 | 可灵Agent管理平台            |
| `VITE_APP_ENV`      | 环境标识     | development           | test                              | production                   |
| `VITE_API_BASE_URL` | API 基础地址 | http://localhost:8080 | https://test-api.keling-agent.com | https://api.keling-agent.com |
| `VITE_PORT`         | 开发端口     | 3000                  | 3000                              | 3000                         |

- 环境变量文件：`.env.development`、`.env.test`、`.env.production`
- 本地覆盖：创建 `.env.*.local` 文件（不会被 Git 跟踪）

---

## 📝 代码提交规范

### Git Hooks

- **pre-commit**: 自动执行 lint-staged（ESLint + Prettier + Stylelint）

### Commit Message 格式

```
<type>(<scope>): <subject>
```

**type 类型：**

| Type       | 说明                   |
| ---------- | ---------------------- |
| `feat`     | 新功能                 |
| `fix`      | Bug 修复               |
| `docs`     | 文档变更               |
| `style`    | 代码格式（不影响逻辑） |
| `refactor` | 重构                   |
| `perf`     | 性能优化               |
| `test`     | 测试相关               |
| `chore`    | 构建/工具链变更        |

**示例：**

```bash
git commit -m "feat(agent): 新增 Agent 列表页面"
git commit -m "fix(request): 修复 401 重复跳转问题"
```

---

## 🛠️ 技术栈

| 技术         | 版本 | 说明           |
| ------------ | ---- | -------------- |
| React        | 18.x | UI 框架        |
| Vite         | 5.x  | 构建工具       |
| TypeScript   | 5.x  | 类型系统       |
| Ant Design   | 5.x  | UI 组件库      |
| React Router | 6.x  | 路由管理       |
| Zustand      | 4.x  | 状态管理       |
| React Query  | 5.x  | 服务端状态管理 |
| Axios        | 1.x  | HTTP 请求      |
| Less         | 4.x  | CSS 预处理     |
| ESLint       | 8.x  | 代码检查       |
| Prettier     | 3.x  | 代码格式化     |
| Stylelint    | 16.x | 样式检查       |
| Husky        | 9.x  | Git Hooks      |

---

## 📋 新增页面指南

1. 在 `src/pages/` 下创建页面目录（含 `index.tsx` 和样式文件）
2. 在 `src/router/index.tsx` 中添加路由配置
3. 在布局侧边菜单中添加对应菜单项
4. 如需接口请求，在 `src/api/` 下新建对应模块

---

## 📄 License

MIT
