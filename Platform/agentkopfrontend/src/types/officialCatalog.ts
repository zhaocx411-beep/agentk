/**
 * 官方示范目录
 * GET /agent/operation/backend/platform/official/default
 *
 * default 只下发目录身份（id / name / quickPath 等），不含 fields/uiSchema。
 * 入参 schema 由 GET platform/agentk/capabilities?workflowKey= 提供。
 */

/** 后端 uiSchema / fields 声明的控件类型；未识别的 widget 会降级为 input / json-editor */
export type OfficialFieldWidget =
  | 'input'
  | 'textarea'
  | 'number'
  | 'image-url-list'
  | 'array'
  | 'json-editor'
  | (string & Record<never, never>);

/** 单个配置字段（与后端 fields / uiSchema.fields 对齐） */
export interface OfficialUiField {
  name: string;
  label: string;
  description?: string;
  type: string;
  widget: OfficialFieldWidget;
  required?: boolean;
  schema?: Record<string, unknown>;
  defaultValue?: unknown;
  /** array 元素的字段声明 */
  items?: OfficialUiField;
}

/** 官方自主规划 Agent（目录项） */
export interface OfficialAgentLoopItem {
  kind: 'agent_loop';
  agentLoopId: number;
  name: string;
  description: string;
  agentLoopVersionId: number;
  version: number;
  profileVersionLabel?: string;
  quickMode?: string;
  quickPath?: string;
  /** 绑定数据集 / TEMPLATE 查询用的 Pipeline 名（与 workflowAgents 同语义） */
  pipelineName?: string;
  /** 预留：若目录接口重新下发 schema 可透传 */
  fields?: OfficialUiField[];
  uiSchema?: { fields?: OfficialUiField[] };
}

/** 官方工作流 Agent（目录项） */
export interface OfficialWorkflowItem {
  kind: 'workflow';
  workflowId: number;
  name: string;
  description: string;
  workflowVersionId: number;
  version: number;
  workflowKey?: string;
  agentkId?: string;
  /** 如 pipeline:agentk.generate_brand_ad */
  pipelineRef?: string;
  /**
   * 绑定数据集 / TEMPLATE 查询用的 Pipeline 名
   * 如 generate_brand_ad、storyboard_film_example（≠ workflowKey）
   */
  pipelineName?: string;
  agentLoopVersionId?: number;
  quickMode?: string;
  quickPath?: string;
  /** 预留：若目录接口重新下发 schema 可透传 */
  fields?: OfficialUiField[];
  uiSchema?: { fields?: OfficialUiField[] };
}

/** 官方示范目录响应 */
export interface OfficialDefaultsData {
  agentLoops: OfficialAgentLoopItem[];
  workflowAgents: OfficialWorkflowItem[];
}

/**
 * 官配 pipeline 能力项
 * GET /agent/operation/backend/platform/agentk/capabilities?workflowKey=
 */
export interface PlatformCapabilityItem {
  id: string;
  kind?: string;
  name?: string;
  title?: string;
  description?: string;
  toolRef?: string;
  pipelineRef?: string;
  mode?: string;
  timeoutSeconds?: number;
  inputSchema?: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
  fields?: OfficialUiField[];
  uiSchema?: { fields?: OfficialUiField[] };
}

/** 官配 capabilities 响应 data */
export interface PlatformCapabilitiesData {
  capabilities: PlatformCapabilityItem[];
  cache?: {
    stale?: boolean;
    refreshing?: boolean;
    lastRefreshAt?: string;
    ttlSeconds?: number;
  };
}

/**
 * 官方工作流入参 schema（从 capabilities 中按 workflowKey 取出）
 * 供 WorkflowLaunchModal / CreationStudio 动态侧栏使用
 */
export interface OfficialWorkflowUiSchemaData {
  workflowKey: string;
  name?: string;
  title?: string;
  description?: string;
  fields?: OfficialUiField[];
  uiSchema?: { fields?: OfficialUiField[] };
  inputSchema?: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
}

/** 目录卡片视图模型 */
export interface OfficialCatalogCardData {
  key: string;
  name: string;
  description: string;
  /** 完整业务描述（轮播 pitch / 详情用） */
  detail?: string;
  subtitle: string;
  skills: string[];
  variant: 'planning' | 'workflow';
  agentLoopId?: number;
  agentLoopVersionId?: number;
  workflowId?: number;
  workflowVersionId?: number;
  workflowKey?: string;
  /**
   * 官方 defaults 下发的 pipelineName，用于 TEMPLATE 模版查询等
   * 如 generate_brand_ad（不是 workflowKey agentk.generate_brand_ad）
   */
  pipelineName?: string;
  /**
   * 可选透传的 schema 字段（当前 default 不含；正式来源见 getOfficialWorkflowUiSchema）
   */
  fields?: OfficialUiField[];
}

/** 官方快速对话会话 */
export interface OfficialQuickSession {
  id: string;
  tenantId?: string;
  title: string;
  createdAt?: string;
}
