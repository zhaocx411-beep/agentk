/**
 * AgentCenter - 前端 mock 文案
 * 这里集中放没有接口来源、由前端兜底的展示文案（营销文案、引导步骤、内置 Agent 描述）。
 * 后续接口能提供真实内容时，直接替换调用处即可，不需要在组件里翻找散落的字符串。
 */
import type { FeaturedStat, MarketCase, MarketStep, OfficialCatalogType } from './types';

/** 内置 Agent（后端 Base Profile）在列表与详情中的展示文案 */
export const BUILTIN_AGENT_TEXT = {
  name: 'agent-k 内置助手',
  /** 列表卡片描述 */
  cardDescription: '由后端 Base Profile 提供的默认能力配置，可直接调试或以此为基础创建 Agent',
  /** 详情页描述 */
  detailDescription: '后端 Base Profile 默认能力配置',
} as const;

/** 可灵Agent中后台 目录页头营销文案 */
export const OFFICIAL_GALLERY_TEXT = {
  heroTitleByType: {
    planning: '可灵Agent中后台 · 官方 Agent-Loop',
    workflow: '可灵Agent中后台 · 官方工作流',
  },
  heroDescByType: {
    planning: '适合开放式创作：通过对话完成创意策划、图文和视频任务，Agent 会根据目标自主选择工具。',
    workflow: '适合目标明确的任务：填写必要参数，按预设步骤稳定生成内容，执行过程与最终产物清晰可控。',
  },
  typeSwitchLabel: '官方能力类型',
  emptyTitleByType: {
    planning: '暂无上架 Agent-Loop',
    workflow: '暂无上架工作流',
  },
  emptyDesc:
    '官方 Agent 与工作流由平台统一发布，上架后会自动同步到此目录。若长时间为空，请稍后再试或联系平台管理员。',
  featuredLabel: '本周精选',
} as const;

/** 我的 Agent 面板页头文案 */
export const MINE_AGENT_PANEL_TEXT = {
  subtitle:
    '管理我的 Agent-Loop：在详情页编辑配置、保存草稿与发布版本；试用官方能力请前往「官方 Agent」目录。',
  createAction: '创建 Agent',
} as const;

/** 我的 Agent 列表空态：区分「一个都没有」与「筛选没命中」两种处境 */
export const MINE_AGENT_EMPTY = {
  title: '还没有 Agent',
  desc: '点击「创建 Agent」新建 Agent-Loop，也可前往「官方 Agent」目录体验后复制。',
  action: '创建 Agent',
  filteredTitle: '没有符合条件的 Agent',
  filteredDesc: '换个关键词，或把状态筛选切回「全部状态」再试试。',
  filteredAction: '清空筛选条件',
} as const;

/** 创建 Agent 弹窗文案 */
export const CREATE_AGENT_MODAL_TEXT = {
  title: '创建 Agent',
  hint: '新建 Agent-Loop Agent，自主规划并执行任务。',
  lockHint: '请先填写 Agent 名称。未填写前无法进入配置页，可返回上一页。',
  nameLabel: '名称',
  namePlaceholder: '请输入 Agent 名称',
  nameRequired: '请输入 Agent 名称',
  nameWhitespace: '名称不能为空',
  nameMax: '名称不超过 50 字',
  descLabel: '描述',
  descPlaceholder: '一句话描述这个 Agent 能做什么（可选）',
  descMax: '描述不超过 200 字',
  ok: '创建',
  cancel: '取消',
  back: '返回',
} as const;

/** 自定义 Agent 新建页（命名闸门背后的配置台占位） */
export const CUSTOM_AGENT_CREATE_TEXT = {
  placeholderName: '未命名 Agent',
  placeholderDesc: '填写名称后即可开始配置角色指令、能力与预览',
  rolePane: '角色指令',
  configPane: 'Agent 配置',
  previewPane: '预览与调试',
  versionPending: '版本尚未生成',
  saveDraft: '保存草稿',
  publish: '更新发布',
  promptPlaceholder: '定义 Agent 的角色与行为准则',
  modelTitle: '对话模型',
  capabilityTitle: '能力扩展',
  previewEmpty: '创建后即可在此预览与调试',
} as const;

/** 创建 Agent 弹窗的类型说明文案（图标在组件内组装） */
export const CREATE_AGENT_TYPE_TEXT: Record<OfficialCatalogType, { name: string; desc: string }> = {
  planning: {
    name: 'Agent-Loop Agent',
    desc: '自主思考并规划任务，适用于基础或日常业务流程',
  },
  workflow: {
    name: '工作流 Agent',
    desc: '按照人工编排的工作流执行任务，适用于流程严谨的业务场景',
  },
};

/* ========== 精选轮播文案池（无接口来源，按卡片 key 稳定分配） ========== */

/** 轮播默认展示顺序，未列出的卡片按接口原序排在后面 */
export const FEATURED_ORDER = ['loop-7', 'wf-2', 'wf-3'];

/** Agent 名称 / workflowKey → 场景标签 */
export const FEATURED_SCENE_BY_NAME: Record<string, string> = {
  全工具智能体: '工具集成',
  品牌广告视频生成: '品牌营销',
  分镜成片: '视频生产',
  generate_brand_ad: '品牌营销',
  storyboard_film: '视频生产',
};

/** 卡片 key → 场景标签 */
export const FEATURED_SCENE_BY_KEY: Record<string, string> = {
  'loop-7': '工具集成',
  'wf-2': '品牌营销',
  'wf-3': '视频生产',
};

/** 卡片 key → 定制卖点 */
export const FEATURED_HOOK_BY_KEY: Record<string, string> = {
  'loop-7': '工具一应俱全，对话即可调度平台能力',
  'wf-2': '输入参考与商品，分钟级产出品牌广告',
  'wf-3': '从分镜到成片，Pipeline 全自动推进',
};

/** 卡片 key → 定制介绍 */
export const FEATURED_PITCH_BY_KEY: Record<string, string> = {
  'loop-7': '官方示范全工具智能体，对齐 demoui「完整创作助手」能力面，适合快速验证工具链路与对话体验。',
  'wf-2': '品牌广告视频生成工作流，上传商品与参考素材，AI 自动完成人物、场景与商品替换。',
  'wf-3': '分镜成片 Pipeline，将脚本分镜一键推进为可交付视频，适合内容生产团队快速试跑。',
};

/** 自主规划类兜底卖点池 */
export const FEATURED_PLANNING_HOOKS = [
  '秒级唤醒，即刻进入工作状态',
  '让灵感与执行，只隔一次对话',
  '官方调优，稳定输出高质量结果',
  '开箱即用，零配置开启智能协作',
] as const;

/** 工作流类兜底卖点池 */
export const FEATURED_WORKFLOW_HOOKS = [
  '严谨流程，分钟级跑通全链路',
  '可视化编排，复杂任务一键交付',
  '官方 Pipeline，生产环境可直接复用',
  '从输入到成片，全程自动推进',
] as const;

/** 自主规划类指标池 */
export const FEATURED_PLANNING_STATS: FeaturedStat[][] = [
  [
    { value: '~30s', label: '首响速度' },
    { value: '多模态', label: '输入支持' },
    { value: '官方', label: '能力认证' },
  ],
  [
    { value: '24/7', label: '在线可用' },
    { value: 'Tool', label: '工具联动' },
    { value: 'vLatest', label: '持续迭代' },
  ],
];

/** 工作流类指标池 */
export const FEATURED_WORKFLOW_STATS: FeaturedStat[][] = [
  [
    { value: '全流程', label: '编排覆盖' },
    { value: '可观测', label: '运行追踪' },
    { value: '官方', label: '模版认证' },
  ],
  [
    { value: 'Pipeline', label: '节点编排' },
    { value: '参数化', label: '灵活配置' },
    { value: '稳定', label: '生产可用' },
  ],
];

/** 描述过短时补充的兜底话术 */
export const FEATURED_PITCH_SUFFIX =
  '。由可灵官方团队预置与调优，适合作为业务落地参考与快速使用入口。';

/** 描述长度达到该值时直接使用接口描述，不再拼接兜底话术 */
export const FEATURED_PITCH_MIN_LENGTH = 48;

/** Market 详情：通用三步（按 variant 区分文案） */
export const MARKET_STEPS_PLANNING: MarketStep[] = [
  {
    title: '描述你的目标',
    description: '用一句话说明想完成的任务，Agent 会自主规划工具调用与执行步骤。',
  },
  {
    title: '确认关键参数',
    description: '按需补充素材、约束或偏好；不清楚时 Agent 会通过追问补齐。',
  },
  {
    title: '对话中完成交付',
    description: '边聊边看进度与产物，可随时追问修改，结果沉淀在会话历史里。',
  },
];

export const MARKET_STEPS_WORKFLOW: MarketStep[] = [
  {
    title: '准备输入素材',
    description: '上传参考视频、商品图或填写业务参数，Pipeline 按契约校验入参。',
  },
  {
    title: '选择风格与模版',
    description: '从官方模版或历史记录里选一个风格起点，减少从零试错。',
  },
  {
    title: '一键生成并查看产物',
    description: '启动 Pipeline，跟踪节点进度，完成后在结果区预览与下载。',
  },
];

/** 卡片 key → 定制三步 / 案例；未命中则用通用池 */
export const MARKET_DETAIL_BY_KEY: Record<
  string,
  Partial<{
    stepsTitle: string;
    steps: MarketStep[];
    casesTitle: string;
    cases: MarketCase[];
  }>
> = {
  'wf-2': {
    stepsTitle: '3 步生成品牌级营销视频',
    steps: [
      {
        title: '上传爆款视频',
        description: '选择一支想复刻的参考视频，作为人物动作与镜头节奏的风格底。',
      },
      {
        title: '上传你的商品图',
        description: '提供商品主图或多角度素材，AI 自动完成人物与商品替换。',
      },
      {
        title: '一键复刻输出',
        description: '融合 VLLM 与生成能力，分钟级产出可投放的营销短片。',
      },
    ],
    casesTitle: '精选案例',
    cases: [
      { title: '美妆产品短片', description: '参考爆款口播节奏，替换为品牌主推 SKU。' },
      { title: '3C 产品短片', description: '保留镜头语言，商品与卖点文案一键替换。' },
      { title: '服饰上身展示', description: '人物换装 + 场景统一，适合电商主图视频。' },
    ],
  },
  'wf-3': {
    stepsTitle: '3 步完成分镜成片',
    steps: MARKET_STEPS_WORKFLOW,
    casesTitle: '适用场景',
    cases: [
      { title: '短片分镜', description: '主题拆镜后并行出片，适合内容团队试跑。' },
      { title: '脚本直出', description: '从文案到镜头编排，一条 Pipeline 跑通。' },
    ],
  },
  'loop-7': {
    stepsTitle: '3 步用上全工具智能体',
    steps: MARKET_STEPS_PLANNING,
    casesTitle: '你可以用它做什么',
    cases: [
      { title: '多工具联调', description: '在对话里串联生图、生视频与剪辑能力。' },
      { title: '能力探索', description: '快速验证平台工具链路与交互体验。' },
    ],
  },
};

/** 工作流预览未运行时的引导文案 */
export const WORKFLOW_RUN_GUIDE = {
  title: '准备运行 Pipeline',
  desc: '在下方配置参数并点击运行，执行进度与结果将按 Pipeline 运行页样式展示。',
  /** 需要先选择 Pipeline 时额外插入的第一步 */
  selectPipelineStep: '选择要运行的 Pipeline',
  steps: ['填写运行参数（表单或 JSON）', '点击「运行 Pipeline」开始执行'],
} as const;
