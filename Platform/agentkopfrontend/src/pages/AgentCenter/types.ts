/**
 * AgentCenter - 页面类型定义
 * 数据源：我的列表走 /platform/agent-loops；官方目录走 /official/default；
 * 新建 AgentLoop 默认 profile 走 session-profiles/templates/agent-loop-default
 */
import type {
  BaseProfile,
  CatalogPipeline,
  ProfilePipelineRef,
} from '@/pages/AgentPlayground/types';

/** Agent 类型：自主规划走 agent-loop；工作流走 platform workflows */
export type AgentKind = 'planning' | 'workflow' | 'builtin';

/** 列表页归属：我的 Agent / 官方 Agent，由二级菜单路由决定 */
export type AgentScope = 'mine' | 'official';

/** 官方目录类型筛选，对应自主规划 / 工作流 */
export type OfficialCatalogType = 'planning' | 'workflow';

/** 我的 Agent 列表状态筛选：all 为不限 */
export type MineStatusFilter = 'all' | 'published' | 'draft';

/** 我的 Agent 列表排布方式：卡片网格 / 紧凑列表 */
export type MineAgentLayout = 'grid' | 'list';

/** 精选轮播的一条指标（如「~30s / 首响速度」） */
export interface FeaturedStat {
  value: string;
  label: string;
}

/** 精选轮播单页展示文案 */
export interface FeaturedSlideMeta {
  /** 顶部小标签：类型 · 场景 */
  eyebrow: string;
  /** 一句话卖点 */
  hook: string;
  /** 详细介绍 */
  pitch: string;
  stats: FeaturedStat[];
}

/** Market 详情页「怎么用」的一步 */
export interface MarketStep {
  title: string;
  description: string;
}

/** Market 详情页案例卡 */
export interface MarketCase {
  title: string;
  description: string;
}

/** Market 详情页完整展示模型（接口无营销字段时由 mock 补齐） */
export interface MarketDetailMeta extends FeaturedSlideMeta {
  /** 三步说明区标题 */
  stepsTitle: string;
  steps: MarketStep[];
  /** 案例区标题 */
  casesTitle: string;
  cases: MarketCase[];
}

export type {
  OfficialAgentLoopItem,
  OfficialWorkflowItem,
  OfficialDefaultsData,
  OfficialCatalogCardData,
  OfficialQuickSession,
} from '@/types/officialCatalog';

/** 列表页卡片视图模型（真实 Agent 与内置 Agent 统一抽象） */
export interface AgentCardData {
  /** 路由参数：真实 Agent 为 id 字符串，工作流为 wf-{id}，内置为 builtin */
  key: string;
  /** Agent 类型，决定列表标签与详情路由 */
  kind: AgentKind;
  /** 真实 AgentLoop ID；内置 / 工作流为空 */
  agentId?: number;
  /** 工作流实体 ID（kind=workflow） */
  workflowId?: number;
  name: string;
  description: string;
  /** 类型标签：自主规划Agent / 工作流Agent / 内置Agent */
  typeLabel: string;
  /** 状态：published=已发布 draft=草稿 builtin=内置 */
  status: 'published' | 'draft' | 'builtin';
  /** 当前版本号 */
  versionLabel?: string;
  /** 更新时间展示文案 */
  updatedText?: string;
  /** 启用工具数量 */
  toolCount?: number;
}

/** HTTP 工具定义（baseProfile.httpTools 元素，后端透传） */
export interface HttpToolDef {
  name: string;
  description?: string;
  inputSchema?: {
    type?: string;
    required?: string[];
    properties?: Record<string, { type?: string; description?: string }>;
  };
  http?: {
    url?: string;
    method?: string;
    timeoutSeconds?: number;
  };
  async?: {
    taskIdField?: string;
    statusField?: string;
    doneValues?: string[];
    failedValues?: string[];
  };
}

/** 详情页统一视图模型（真实版本 / 内置 Profile 共用） */
export interface AgentDetailData {
  /** 是否内置 Base Profile（不可保存/发布） */
  isBuiltin: boolean;
  agentId?: number;
  /** 当前铺开的版本主键（真实 Agent） */
  versionId?: number;
  name: string;
  description: string;
  status: AgentCardData['status'];
  versionLabel?: string;
  updatedText?: string;
  /** 可编辑配置 */
  system: string;
  /** 主对话模型（来自 profile.defaultModel；选项来自 /agentk/models） */
  defaultModel: string;
  tools: string[];
  /** 只读铺开内容 */
  pipelines: ProfilePipelineRef[];
  httpTools: HttpToolDef[];
  /** 完整 BaseProfile（保存草稿时回写，保留未编辑字段） */
  rawProfile: BaseProfile;
  /** 可选工具目录（来自能力目录，与已启用工具取并集展示） */
  catalogTools: string[];
  /** 能力目录中的 Pipeline 声明，用于补齐 pipelines 的描述等只读信息 */
  catalogPipelines: CatalogPipeline[];
}

export type { BaseProfile, CatalogPipeline, ProfilePipelineRef };
