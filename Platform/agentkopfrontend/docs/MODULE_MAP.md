# 前端模块地图（Module Map）

> 本文档说明 `src/pages` 及各业务目录的**职责划分**与**文件归属**，便于定位代码。  
> 最后更新：2026-08-21（AgentCenter 目录重组；补充路由/菜单/共享库状态说明）

---

## 1. 总览

```
src/
├── pages/          # 按路由划分的页面模块（本文档重点）
├── components/     # 跨页面复用的通用 UI（表格、分页、素材选择器等）
├── layouts/        # 壳层布局（MainLayout、侧栏、顶栏）
├── services/       # 后端 API 封装（workflow、media、agentLoop…）
├── hooks/          # 全局 hooks
├── styles/         # 设计 token、主题、全局 Less
├── router/         # 路由表
└── utils/          # 通用工具函数
```

**页面 vs 组件 vs 逻辑**

| 层级 | 放什么 | 示例 |
|------|--------|------|
| `pages/X/index.tsx` | 路由入口、页面壳、布局拼装 | `OfficialExperience/index.tsx` |
| `pages/X/components/` 或子目录 | 仅该页面用的 UI | `AgentDetail/components/ConfigPanel` |
| `pages/X/hooks/` | 页面级状态与副作用 | `useCreationStudio` |
| `pages/X/lib/` | 纯函数、类型、常量（无 React） | `studio/lib/pipeline.ts` |
| `src/components/` | 多页面复用 | `MediaUpload`、`WorkbenchLayout` |
| `src/services/` | HTTP / 业务 API | `services/workflow.ts` |

---

## 2. 路由 ↔ 页面对照

**图例（入口状态）**

| 标记 | 含义 |
|------|------|
| 🟢 常开 | 菜单可见，正常使用 |
| 🟡 共享库 | **路由保留**、**菜单已隐藏**；`index.tsx` 独立页无产品入口，但目录内 **types / hooks / components 被多处 import**，不可当 dead code 删除 |
| ⚫ 已下线 | 路由与菜单均已关闭（代码可保留待恢复） |

| 路由 | 页面目录 | 用户看到什么 | 入口 |
|------|----------|--------------|------|
| `/` | `Home/` | 平台首页 | 🟢 |
| `/agents/official` | `AgentCenter/`（scope=official） | 官方 Agent 目录 | 🟢（首页「更多」等，顶栏菜单项暂隐藏） |
| `/agents/mine` | `AgentCenter/`（scope=mine） | 我的 Agent 列表 | 🟢 |
| `/agents/:agentId` | `AgentCenter/AgentDetail/` | 自建 Agent 配置 + 预览对话 | 🟢 |
| `/agents/wf/:workflowId` | `AgentCenter/WorkflowAgentDetail/` | 工作流 Agent 配置 + 运行预览 | 🟢 |
| `/agents/official/chat/:sessionId` | `AgentCenter/OfficialExperience/` | 官方 Agent **使用页**（对话 / 创作工作台） | 🟢 |
| `/agents/official/console/:sessionId` | `AgentCenter/OfficialWorkflowConsole/` | 工作流生成控制台 | 🟢 |
| `/agents/official/app/:cardKey` | `AgentCenter/OfficialMarketDetail/` | 官方 Agent 市场详情 | 🟢 |
| `/agent-playground` | `AgentPlayground/` | Agent 调试台（Sidebar + 全功能调试） | 🟡 见 §4 |
| `/workflow-runner` | `WorkflowRunner/` | Pipeline 独立运行页 | 🟡 见 §4.1 |
| `/tools`、`/tools/:toolName` | `ToolGallery/` | 工具广场 / 详情 | 🟢 |
| `/material-library` | `MaterialLibrary/` | 素材库 | 🟢 |
| `/works-gallery`、`/my-works` | `WorksGallery/` | 团队 / 个人作品集 | 🟢 |
| `/offline-run/datasets` | `OfflineRun/DatasetManage` | 数据集管理 | 🟢 |
| `/offline-run/jobs` | `OfflineRun/JobManage` | 离线跑数任务 | 🟢 |
| `/session-review` | `SessionReview/` | 数据回看 | 🟢 |
| `/model-arena` | `ModelArena/` | 多模型对战 | 🟢 |
| `/permission-control` | `PermissionControl/` | 权限控制 | 🟢 |
| `/oss-tools` | `OssToolLibrary/` | 开源能力灵感库 | 🟢 |
| `/workflow` | `WorkflowEditor/` | Pipeline 画布编辑器 | ⚫ 路由已注释，见 §5 |

---

## 3. AgentCenter（Agent 资产中心）

**路由入口**：`/agents/official`、`/agents/mine` 及下属详情/体验路由  
**职责**：官方目录、我的 Agent 列表、Agent 配置、官方体验、工作流控制台

```
AgentCenter/
├── index.tsx                 # 列表页入口（官方 / 我的 由 scope 区分）
├── api.ts                    # AgentCenter 专用接口
├── types.ts / constants.ts   # 卡片、分页、目录类型
├── utils.ts                  # 卡片转换、官方路径构建、体验类型解析
├── mock.ts                   # 列表 mock（开发用）
│
├── hooks/
│   └── useOfficialLaunch.tsx # 「立即使用」跳转官方体验
│
├── lib/                      # 列表页展示用静态资源与 meta
│   ├── featuredVisuals.ts    # 精选 Agent 视觉
│   ├── officialCardCovers.ts # 卡片封面
│   ├── officialCardTheme.ts  # 卡片主题色
│   ├── officialFeaturedMeta.ts
│   └── marketGuide.ts        # 市场引导文案
│
├── components/               # 列表页 UI（非详情页）
│   ├── MineAgentPanel.tsx    # 「我的 Agent」列表面板
│   ├── AgentCard.tsx         # 通用 Agent 卡片
│   ├── CreateAgentModal.tsx  # 创建 Agent 弹窗
│   └── official/             # 官方目录专用
│       ├── OfficialAgentGallery.tsx
│       ├── OfficialAgentCard.tsx
│       ├── OfficialCatalogSection.tsx
│       └── OfficialFeaturedCarousel.tsx
│
├── AgentDetail/              # 路由：/agents/:agentId
├── WorkflowAgentDetail/      # 路由：/agents/wf/:workflowId
├── OfficialExperience/       # 路由：/agents/official/chat/:sessionId
├── OfficialMarketDetail/     # 路由：/agents/official/app/:cardKey
├── OfficialWorkflowConsole/  # 路由：/agents/official/console/:sessionId
└── WorkflowLaunchModal/      # 工作流快速启动弹窗（被多处引用）
```

---

### 3.1 AgentDetail — 自建 Agent 配置页

**页面形态**：左配置 + 右 Playground 式预览对话

```
AgentDetail/
├── index.tsx                      # 页面壳：WorkbenchLayout + 配置/预览分栏
├── AgentDetail.module.less        # 整页样式（子组件共用）
│
├── components/                    # UI 组件
│   ├── DetailHeader.tsx           # 顶栏：名称、状态、保存
│   ├── ConfigPanel.tsx            # 左栏：角色 Prompt + 能力配置 Tab 内容
│   ├── PreviewPanel.tsx           # 右栏：嵌入 ChatArea + 会话历史
│   ├── CapabilityCards.tsx        # HTTP 工具 / Pipeline 能力卡片
│   ├── ModelSelectSection.tsx     # 模型选择区
│   ├── ModelSelectCard.tsx        # 单个模型卡片
│   ├── ToolSelectSection.tsx      # 工具选择与已选列表
│   ├── SelectedToolCard.tsx       # 已选工具卡片
│   ├── AddToolModal.tsx           # 添加工具弹窗
│   ├── EditToolDefaultsModal.tsx  # 编辑工具默认参数
│   ├── ToolDefaultsForm.tsx       # 工具默认参数表单
│   ├── ParamField.tsx             # 单个参数字段（含媒体）
│   ├── ToolParamMediaPicker.tsx   # 工具参数里的素材选择
│   ├── SessionHistoryList.tsx     # 预览区会话历史列表
│   └── SaveOfficialAgentNameModal.tsx  # 保存为官方 Agent 时命名
│
├── hooks/
│   ├── useAgentDetailModel.ts     # 详情加载、编辑态、保存、dirty 检测
│   ├── usePreviewChat.tsx         # 预览区 SSE 对话、会话切换
│   ├── useAgentDetailPromptShell.tsx  # 官方模版快捷提问条（复用 FreeChat 模版）
│   └── useSessionListPagination.ts    # 会话列表分页（被 Official 体验复用）
│
└── lib/
    ├── toolMeta.ts                # 工具图标、accent、展示名（ToolGallery 也引用）
    ├── modelMeta.ts               # 模型展示 meta
    ├── accentStyles.ts            # accent 样式 class 映射
    ├── toolDefaultsUtils.ts       # 工具默认参数结构
    ├── configCompare.ts           # 配置与运行态是否一致
    └── pendingOfficialAgent.ts    # 待保存官方 Agent 草稿
```

---

### 3.2 WorkflowAgentDetail — 工作流 Agent 配置页

**路由**：`/agents/wf/:workflowId`  
**职责**：工作流型 Agent 的参数配置、运行预览、会话历史（结构与 AgentDetail 类似但面向 Pipeline）

| 文件 | 职责 |
|------|------|
| `index.tsx` | 页面壳 |
| `WorkflowDetailHeader.tsx` | 顶栏 |
| `WorkflowConfigPanel.tsx` | 左侧工作流参数配置 |
| `WorkflowPreviewPanel.tsx` | 右侧运行预览 + 历史 |
| `WorkflowRunEmptyState.tsx` | 无运行记录空态 |
| `WorkflowRunParams.tsx` | 运行参数展示 |
| `useWorkflowAgentModel.ts` | 详情数据与编辑态 |
| `useWorkflowAgentPreview.ts` | 预览运行、SSE |
| `useWorkflowRuns.ts` | 运行记录列表 |
| `pipelineRuntime.ts` | Pipeline 运行态辅助 |

---

### 3.3 OfficialExperience — 官方 Agent 使用页（核心）

**路由**：`/agents/official/chat/:sessionId?type=…&agentLoopId=…`  
**分流逻辑**（见 `OfficialExperience/index.tsx`）：

```
                    OfficialExperience/index.tsx
                              │
          ┌───────────────────┼───────────────────┐
          │                   │                   │
   type=planning        type=workflow      type=workflow + keffects
   (自主规划)            (通用工作流)         (专用 Studio)
          │                   │                   │
   OfficialFreeChat    CreationStudio     ClothingDupeStudio
                              │            GoodsScraperStudio
                              │            GoodsStudioStudio
                              └─ studio/ 子模块
```

**页面级文件**（`OfficialExperience/` 根目录）

| 文件 | 职责 |
|------|------|
| `index.tsx` | 路由入口：解析 catalog / workflow / keffects，挂载对应子页面 |
| `ExperienceHeader.tsx` | 顶栏：Agent 名、运行态、模版按钮 |
| `useOfficialExperienceChat.ts` | 会话列表、SSE、输入框、TTS、历史分页（FreeChat + Studio 共用） |
| `useOfficialTemplate.ts` | 「使用官方模版」：复制配置到当前会话 |
| `officialSessions.ts` | 官方会话列表查询封装 |
| `pendingFirstMessage.ts` | 从列表「带首条消息」进页的 pending 逻辑 |
| `runStatus.ts` | 运行态 label / className 配置表（消除嵌套三元） |

---

#### 3.3.1 OfficialFreeChat — 自主规划 Agent 自由对话

**适用**：官方 agent-loop（自主规划），含漫剧等特殊 loop

```
OfficialFreeChat/
├── index.tsx                    # 页壳：左会话历史 + 右对话/产出（或漫剧 Workspace）
├── OfficialFreeChat.module.less # 布局样式（workbench / output / manga 共用）
│
├── workbench/
│   └── OfficialLoopWorkbench.tsx   # 输入区：ChatArea + 附件 + TTS 工具条
│
├── output/                         # 右侧产出区
│   ├── OfficialOutputStage.tsx     # 产出主舞台：流式 / 产物 / 配置 Tab
│   ├── OfficialLoopStreamPanel.tsx # SSE 流式消息面板
│   ├── OfficialConfigPanel.tsx     # Agent 配置只读展示
│   ├── OfficialGenerationProcess.tsx # Pipeline 步骤只读（agent-loop 内嵌 pipeline 时）
│   └── useOfficialSessionArtifacts.ts # 从 ledger 汇总产物
│
├── templates/                      # 快捷提问 / 灵感模版（AgentDetail 预览也复用）
│   ├── OfficialLoopPromptStrip.tsx      # 输入框上方横条模版
│   ├── OfficialLoopTemplateLauncher.tsx # 模版下拉 launcher
│   ├── useOfficialLoopPromptTemplates.ts # 拉取 TEMPLATE 数据集
│   ├── promptTemplates.ts              # 本地 fallback 引导文案
│   └── applyOfficialLoopTemplatePick.ts  # 选中模版写入输入框/附件
│
├── manga/                          # 漫剧专用
│   ├── MangaWorkspace.tsx          # 画布 / 产出 Tab 切换
│   ├── MangaCreationCanvas.tsx     # 画布容器
│   ├── MangaCanvasBoard.tsx        # 画布渲染
│   ├── foldMangaCanvas.ts          # 折叠算法
│   ├── drawMangaCanvasLinks.ts     # 连线绘制
│   ├── buildMangaFoldSliceFromLedger.ts
│   ├── useMangaCanvasModel.ts
│   └── useOfficialSessionLedger.ts # 全量会话 ledger（跨页产物/画布）
│
└── tts/
    ├── TtsComposerControls.tsx     # TTS 合成控件
    └── useTtsComposer.ts           # TTS 状态与提交
```

---

#### 3.3.2 studio — 官方工作流创作工作台

**适用**：官方 workflow 卡片（非 keffects 专用壳时使用 `CreationStudio`）

```
studio/
├── CreationStudio.tsx           # 工作台页壳：上屏表单+Inspector，下屏历史与结果
├── CreationStudio.module.less   # 工作台共享样式
│
├── form/                        # 左侧创作表单
│   ├── CreationForm.tsx         # 主表单：参数、参考图、生成按钮
│   ├── ArgField.tsx             # 单个动态参数字段（WorkflowLaunchModal 也引用）
│   ├── TemplatePicker.tsx       # 模版卡片选择（CreationStudio 内）
│   ├── StudioTemplatePicker.tsx # 模版下拉（keffects 表单复用）
│   ├── IntentTrace.tsx          # 意图追踪展示
│   └── useStudioTemplates.ts    # 模版分页加载
│
├── pipeline/                    # 流水线 / 产出 UI
│   ├── PipelinePanel.tsx        # 节点步骤条
│   ├── PipelineRunPanel.tsx     # 当前 run 状态面板
│   ├── StreamPanel.tsx          # SSE 流式输出（含 AskUserCard）
│   ├── InspectorPanel.tsx       # 右侧 Inspector 容器
│   ├── GenerationHistory.tsx    # 「我的生成」历史列表
│   ├── ArtifactDock.tsx         # 产物网格 Dock
│   └── pipelineNodeMeta.ts      # 节点状态文案 / 样式 class
│
├── hooks/
│   └── useCreationStudio.ts     # 核心状态机：模版、参数、生成、会话切换、pipeline-runs
│
├── lib/                         # 纯逻辑（无 UI）
│   ├── types.ts                 # ArtifactItem、PipelineNode、GenerationRecord…
│   ├── constants.ts             # 工作室常量（项目选项等）
│   ├── templates.ts             # 模版占位、prompt 填充
│   ├── templateTasks.ts         # datasets TEMPLATE 分页 API（FreeChat 模版共用）
│   ├── templatePickerMeta.ts    # 模版列表展示 meta
│   ├── schemaFields.ts          # 官方 UI Schema → 动态表单 spec
│   ├── executionSpecs.ts        # 执行方式、字段 spec、preview payload
│   ├── pipeline.ts              # 节点构建、产物收集、进度计算
│   ├── artifactTier.ts          # 产物分级排序
│   ├── sessionHistory.ts        # sessions → GenerationRecord
│   ├── pendingGenerate.ts       # 跨页 pending 普通生成
│   ├── pendingPipelineGenerate.ts
│   ├── restoreRecords.ts        # 历史记录恢复
│   └── studioFoldHint.ts        # 折叠提示文案
│
├── keffects/                    # Keffects 专用工作流 Studio（商品/爬虫/换装）
│   ├── KeffectsStudioShell.tsx  # 共用外壳：历史 + 折叠
│   ├── GoodsStudioStudio.tsx    # 商品视频 Studio 入口
│   ├── GoodsScraperStudio.tsx   # 商品爬虫 Studio 入口
│   ├── *Form.tsx / *Results.tsx # 各业务表单与结果区
│   ├── KeffectsTaskPanel.tsx    # 任务进度面板
│   ├── KeffectsPipelineHistory.tsx
│   ├── useGoodsStudioTask.ts    # 各任务 hook + 轮询
│   ├── keffectsTaskRuntime.ts   # 轮询、进度百分比
│   ├── applyKeffectsTemplate.ts # 模版解析写入表单
│   └── pipelineExecHistory.ts   # pipeline-execs → 历史记录
│
└── clothingDupe/                # 换装复刻专用 Studio
    ├── ClothingDupeStudio.tsx
    ├── ClothingDupeForm.tsx
    ├── ClothingDupeTaskPanel.tsx
    ├── MediaUploadField.tsx
    ├── mediaFields.ts
    └── useClothingDupeTask.ts
```

---

### 3.4 其他 AgentCenter 子页面

| 目录 | 路由 | 职责 |
|------|------|------|
| `OfficialMarketDetail/` | `/agents/official/app/:cardKey` | 官方 Agent 市场详情页（介绍、能力、进入体验） |
| `OfficialWorkflowConsole/` | `/agents/official/console/:sessionId` | 工作流运行控制台：批量 runs、产物预览 |
| `WorkflowLaunchModal/` | （弹窗，无独立路由） | 从列表快速填参并启动工作流 |

---

## 4. AgentPlayground — Agent 调试台（🟡 共享库）

| 维度 | 状态 |
|------|------|
| **路由** | ✅ 保留：`router/index.tsx` 仍注册 `/agent-playground` |
| **菜单** | ❌ 已隐藏：`menuConfig.tsx` 中「调试与运行」整组已注释 |
| **产品入口** | ❌ 无：应用内无 `Link` / `navigate` 指向该路径（仅手动输 URL） |
| **代码定位** | ⚠️ **共享库**：目录不可删；`types`、`api`、`hooks/useSSE`、`components/chat/*` 等被 Agent 详情、官方体验等广泛引用 |

**独立页职责**（仅访问 `/agent-playground` 时）：会话管理、SSE 对话、版本/配置、Run Inspector。

**被外部复用的典型 export**（改这里会影响多页）：

| 路径 | 谁在用 |
|------|--------|
| `components/chat/ChatArea` | `AgentDetail/PreviewPanel`、`OfficialFreeChat/workbench` |
| `components/chat/ChatAttachmentBar` | 官方自由对话、Agent 详情预览 |
| `components/chat/AskUserCard` | `OfficialFreeChat/output`、`studio/pipeline/StreamPanel` |
| `types.ts` | AgentCenter、WorkflowRunner、services、utils 等 |
| `hooks/useSSE.ts` | Agent 详情预览、官方体验、Studio |
| `hooks/sse/helpers.ts` | 流式面板、漫剧 canvas、SessionReview |
| `api.ts` | 会话 CRUD、`SessionTitleRename` 组件 |

```
AgentPlayground/
├── index.tsx              # 页面壳：Sidebar + ChatArea + RunInspector + 各类 Modal
├── api.ts / types.ts      # 调试台 API 与类型
├── constants.ts / utils.ts / mock.ts
│
├── components/
│   ├── chat/              # 对话核心区
│   │   ├── ChatArea.tsx           # 消息列表 + 输入
│   │   ├── ChatAttachmentBar.tsx  # 附件栏（Official 体验复用）
│   │   ├── ChatEmptyState.tsx     # 空态 + 快捷建议
│   │   └── AskUserCard.tsx        # AskUser 交互卡片
│   ├── sidebar/
│   │   └── Sidebar.tsx            # 会话列表、新建、配置入口
│   ├── inspector/
│   │   ├── RunInspector.tsx       # 运行 Inspector（工具调用、事件）
│   │   └── FreeConfigPanel.tsx    # 会话级自由配置
│   ├── version/                   # Agent 版本管理
│   │   ├── VersionManagerDrawer.tsx
│   │   ├── VersionDetailModal.tsx
│   │   └── VersionConfigOverview.tsx
│   ├── modals/
│   │   ├── NewAgentModal.tsx
│   │   ├── SaveAgentModal.tsx
│   │   ├── OverrideModal.tsx
│   │   └── SessionConfigModal.tsx
│   └── workflow/
│       └── WorkflowDetailCard.tsx # 工作流结构只读卡片
│
└── hooks/
    ├── useSSE.ts          # SSE 总入口
    ├── useSessions.ts     # 会话 CRUD
    ├── useCatalog.ts      # 模型/能力 catalog
    ├── useChatHistoryPaging.ts
    └── sse/               # SSE 内部实现拆分
        ├── useSSEState.ts
        ├── useStream.ts
        ├── helpers.ts
        └── types.ts
```

---

### 4.1 WorkflowRunner — Pipeline 运行页（🟡 共享库）

| 维度 | 状态 |
|------|------|
| **路由** | ✅ 保留：`router/index.tsx` 仍注册 `/workflow-runner` |
| **菜单** | ❌ 已隐藏：与 Agent 调试台同在「调试与运行」注释块内 |
| **产品入口** | ❌ 无：无应用内跳转 |
| **代码定位** | ⚠️ **共享库**：`WorkflowRunCard` 等被 `WorkflowAgentDetail` 嵌入；hooks / utils 与 `AgentPlayground/types` 共用 |

**独立页职责**（仅访问 `/workflow-runner` 时）：选择 Pipeline、填参、查看运行结果与历史。

**被外部复用的典型 export**：

| 路径 | 谁在用 |
|------|--------|
| `components/WorkflowRunCard.tsx` | `WorkflowAgentDetail/WorkflowPreviewPanel` |
| `components/workflow/WorkflowDetailCard`（在 AgentPlayground） | `WorkflowVersionDetailModal` |
| `hooks/useWorkflowRunner.ts` | 独立运行页；逻辑与 Playground `api` 共用 |

```
WorkflowRunner/
├── index.tsx              # 独立运行页壳（菜单无入口）
├── hooks/useWorkflowRunner.ts
├── components/WorkflowRunCard.tsx   # ← WorkflowAgentDetail 预览区复用
├── components/ConfigPanel.tsx
├── components/NewWorkflowModal.tsx
├── components/WorkflowVersionDrawer.tsx
└── utils.ts
```

恢复产品入口：取消 `menuConfig.tsx` 中「调试与运行」注释即可，**无需**移动目录。

---

## 5. 其他 pages 模块（简要）

| 目录 | 职责 | 主要文件 |
|------|------|----------|
| `Home/` | 首页：官方 Agent 入口、模型展示、作品区块 | `index.tsx`、`mapOfficialLoops.ts`、`mapOfficialWorkflows.ts`、`components/ModelShowcase` |
| `ToolGallery/` | 工具广场与详情 | `index.tsx`、`components/ToolDetail/`、`constants.ts` |
| `MaterialLibrary/` | 素材库浏览与管理 | `index.tsx`、`components/MaterialDetailModal` |
| `WorksGallery/` | 团队/个人作品集 | `index.tsx`（scope 区分） |
| `WorkflowRunner/` | 🟡 Pipeline 独立运行页 + **共享组件** | 见 §4.1；勿与「仅路由页」混淆 |
| `WorkflowEditor/` | ⚫ Pipeline 画布编辑器（**路由已下线**） | 保留代码，恢复 `router` 注释即可 |
| `OfflineRun/` | 离线跑数：数据集 + 任务 | `DatasetManage.tsx`、`JobManage.tsx` |
| `SessionReview/` | 会话数据回看 | `index.tsx`、`columns.tsx` |
| `ModelArena/` | 多模型对战与管理 | `index.tsx`、`Manage/` |
| `PermissionControl/` | 权限控制后台 | `index.tsx` |
| `OssToolLibrary/` | 开源工具灵感库 | `index.tsx`、`api.ts` |

---

## 6. 全局共享层（常被 pages 引用）

### 6.1 `src/components/`

| 组件 | 用途 |
|------|------|
| `WorkbenchLayout` / `WorkbenchPane` | Agent 详情、官方体验等左右分栏布局 |
| `MediaUpload/` | 素材选择器（上传、资产库、分页 Tab） |
| `BaseTable` / `DataTable` / `BasePagination` | 列表页表格与分页 |
| `PageContainer` / `SearchForm` | B 端页面容器与搜索表单 |
| `SessionTitleRename` | 会话标题重命名 |
| `RunStatusTag` | 运行状态 Tag |
| `WorkflowParamSelect` | 工作流参数选择 |
| `AgentBrandIcons` / `SvgIcon` | 品牌图标 |

### 6.2 `src/services/`（与 pages 的边界）

- **放 services**：HTTP 请求、与后端 resource 一一对应的 CRUD  
- **放 pages/X/lib**：仅某一页面域内的聚合、映射、UI 衍生逻辑  

常用：`agentLoop.ts`、`workflow.ts`、`media.ts`、`keffectsTasks.ts`

### 6.3 `src/layouts/MainLayout/`

- `menuConfig.tsx`：侧栏菜单与路由对应  
- `Header` / `Sider`：顶栏与导航  

---

## 7. 跨模块引用关系（常查）

```
ToolGallery ──imports──► AgentDetail/lib/toolMeta.ts

AgentDetail/hooks/useAgentDetailPromptShell
    └── OfficialFreeChat/templates/*   （官方模版快捷提问）

AgentDetail/components/PreviewPanel
    └── AgentPlayground/components/chat/ChatArea

WorkflowAgentDetail/WorkflowPreviewPanel
    └── WorkflowRunner/components/WorkflowRunCard

OfficialFreeChat/*
    └── AgentPlayground/components/chat/*   （对话 UI 复用）

OfficialExperience/studio/form/ArgField
    └── WorkflowLaunchModal                 （工作流弹窗复用动态字段）

Home/mapOfficialLoops.ts / mapOfficialWorkflows.ts
    └── AgentCenter/utils.ts                （首页卡片跳转官方体验）
```

---

## 8. 目录拆分原则（后续新增代码参考）

1. **按业务域拆**，不按文件类型全局拆（✅ `manga/`、`keffects/`；❌ 全项目统一 `components/` 堆 page 逻辑）  
2. **页面壳留根目录**：`index.tsx` + 主 `.module.less`  
3. **约 15+ 同层文件**时考虑加子目录  
4. **hooks / lib 命名**：  
   - `hooks/`：React 状态、副作用  
   - `lib/`：纯函数、types、constants  
5. **已有子域不拆碎**：`studio/keffects/`、`studio/clothingDupe/` 保持独立  

---

## 9. 快速定位指南

| 我想改… | 去这里 |
|---------|--------|
| 官方 Agent 列表 / 卡片 | `AgentCenter/components/official/` |
| 我的 Agent 列表 | `AgentCenter/components/MineAgentPanel.tsx` |
| 自建 Agent 配置表单 | `AgentDetail/components/ConfigPanel.tsx` |
| 官方自由对话 UI | `OfficialExperience/OfficialFreeChat/` |
| 官方工作流创作表单 | `OfficialExperience/studio/form/` |
| 流水线步骤 / 产物展示 | `OfficialExperience/studio/pipeline/` + `lib/pipeline.ts` |
| 商品视频 / 爬虫 / 换装 Studio | `OfficialExperience/studio/keffects/` 或 `clothingDupe/` |
| 素材选择器分页 | `src/components/MediaUpload/` |
| 对话 UI / SSE 类型（非独立调试页） | `AgentPlayground/components/chat/`、`hooks/useSSE.ts`、`types.ts` |
| Pipeline 运行卡片（嵌入工作流 Agent 详情） | `WorkflowRunner/components/WorkflowRunCard.tsx` |
| 路由新增页面 | `src/router/index.tsx` |

---

如有新页面或目录变更，请同步更新本文档。
