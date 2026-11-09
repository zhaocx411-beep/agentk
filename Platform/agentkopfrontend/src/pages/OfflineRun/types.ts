/**
 * 离线跑数 - 类型定义
 */

// ==================== 数据集管理 ====================

/** 数据集类型 */
export type DatasetType = '文本' | '图片' | '视频';

/** 数据集类型选项（上传时选择） */
export const DATASET_TYPE_OPTIONS: { label: string; value: DatasetType }[] = [
  { label: '文本', value: '文本' },
  { label: '图片', value: '图片' },
  { label: '视频', value: '视频' },
];

/** 数据集用途：EVAL 可提交离线跑数；TEMPLATE 不可 */
export type DatasetUsage = 'EVAL' | 'TEMPLATE';

/** 数据集 */
export interface Dataset {
  /** 数据集 ID */
  id: number;
  /** 数据集名称 */
  name: string;
  /** 版本号 */
  version: string;
  /** 数据集类型 */
  datasetType: DatasetType;
  /** 用途（离线跑数仅允许 EVAL） */
  usage?: DatasetUsage;
  /** 关联 Pipeline 名（可选） */
  pipelineName?: string;
  /** 描述 */
  description: string;
  /** 创建人 */
  creator: string;
  /** 用例数量 */
  total: number;
  /** 引用该数据集的离线任务数 */
  jobCount: number;
  /** 是否被离线任务引用 */
  referencedByJobs: boolean;
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/** 数据集用例（task） */
export interface DatasetTask {
  /** task 主键 */
  id: number;
  /** 用例标识（对应数据集文件 tasks[].id） */
  taskKey: string;
  /** 用户输入 prompt */
  prompt: string;
  /** 外链地址（来自文件 linkUrl / link_url / url） */
  linkUrl?: string;
  /** 参考图 URL 列表（上传文件解析） */
  referImages?: string[];
  /** 扩展字段（上传文件解析） */
  extraFields?: Record<string, unknown>;
  /** 素材库分类（task 级；与 extraFields.category 等价） */
  category?: string;
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/** 数据集用途选项 */
export const DATASET_USAGE_OPTIONS: { label: string; value: DatasetUsage }[] = [
  { label: 'EVAL（可离线跑数）', value: 'EVAL' },
  { label: 'TEMPLATE（不可离线跑数）', value: 'TEMPLATE' },
];

/** 数据集上传参数（form-data） */
export interface DatasetUploadParams {
  file: File;
  /** 数据集类型 */
  datasetType: DatasetType;
  /** 名称（必填） */
  name: string;
  /** 版本号；YAML / JSON 非空时覆盖文件内 version，XLSX 缺省 v1 */
  version?: string;
  description?: string;
  /** EVAL / TEMPLATE，默认建议 EVAL */
  usage?: DatasetUsage;
  /** 关联 Pipeline 名 */
  pipelineName?: string;
  /** xlsx 上传必填：任务说明 */
  taskInstruction?: string;
}

/** 上传数据集素材/模版（form-data；usage 固定 TEMPLATE） */
export interface DatasetTemplateUploadParams {
  file: File;
  datasetType: DatasetType;
  /** 数据集名称，与 version 组成唯一键 */
  name: string;
  /** 归属 pipeline 名 */
  pipelineName: string;
  description?: string;
  /** 为空时后端从 JSON 内提取 */
  version?: string;
}

/**
 * 数据集用例查询参数。
 * datasetId / usage / pipelineName 至少传一个；
 * usage=TEMPLATE 时常与 pipelineName 联用，供创作工作台拉模版参数清单。
 */
export interface DatasetTaskQueryParams {
  /** 数据集 ID（与 usage / pipelineName 三选一必填） */
  datasetId?: number;
  /** EVAL / TEMPLATE（TEMPLATE = pipeline 入口模版参数清单） */
  usage?: DatasetUsage;
  /** 绑定的 Pipeline 名（多与 usage=TEMPLATE 联用） */
  pipelineName?: string;
  /** 当前页码，默认 1 */
  current?: number;
  /** 每页条数，默认 10，最大 100 */
  pageSize?: number;
  /** 素材库 pipelineName=asset 时按 extraFields.category 筛选 */
  category?: string;
}

/** 数据集用例列表响应（分页结构） */
export interface DatasetTaskQueryResponse {
  /** 数据列表 */
  records: DatasetTask[];
  /** 总条数 */
  total: number;
  /** 当前页码 */
  current: number;
  /** 每页条数 */
  pageSize: number;
  /** 总页数 */
  pages: number;
}

/** 数据集列表查询参数（usage 为接口必传） */
export interface DatasetQueryParams {
  /** 用途筛选：EVAL / TEMPLATE（必传；离线相关页固定 EVAL） */
  usage: DatasetUsage;
  /** 当前页码，默认 1 */
  current?: number;
  /** 每页条数，默认 10，最大 100 */
  pageSize?: number;
  /** 名称精确筛选（完全匹配） */
  name?: string;
  /** 类型精确筛选 */
  datasetType?: DatasetType;
  /** Pipeline 名筛选（多用于 TEMPLATE 列表） */
  pipelineName?: string;
  /** 创建人精确筛选 */
  creator?: string;
}

/** 数据集列表响应 */
export interface DatasetQueryResponse {
  /** 数据列表 */
  records: Dataset[];
  /** 总条数 */
  total: number;
  /** 当前页码 */
  current: number;
  /** 每页条数 */
  pageSize: number;
  /** 总页数 */
  pages: number;
}

/** 数据集上传响应（与 Dataset 结构一致） */
export type DatasetUploadResponse = Dataset;

/** 数据集元信息更新参数（description 与 datasetType 至少传一项） */
export interface DatasetUpdateParams {
  /** 描述 */
  description?: string;
  /** 数据集类型 */
  datasetType?: DatasetType;
}

/** 数据集更新响应（与 Dataset 结构一致） */
export type DatasetUpdateResponse = Dataset;

/** 新增单条用例参数 */
export interface DatasetTaskCreateParams {
  /** 用例标识（同数据集内唯一，创建后不可修改） */
  taskKey: string;
  /** 用户输入 */
  prompt: string;
  /** 外链地址（最大 1024） */
  linkUrl?: string;
}

/** 更新单条用例参数（prompt 与 linkUrl 至少传一项；linkUrl 传空字符串表示清空） */
export interface DatasetTaskUpdateParams {
  /** 用户输入 */
  prompt?: string;
  /** 外链地址 */
  linkUrl?: string;
}

/** 数据集导出格式 */
export type DatasetExportFormat = 'yaml' | 'json' | 'xlsx';

// ==================== 离线跑数 ====================

/** agent-config 返回的可选 Pipeline（含 inputSchema） */
export interface AgentConfigPipeline {
  /** 会话内规范名 */
  name: string;
  /** Profile 引用，如 pipeline:agentk.generate_brand_ad */
  ref: string;
  description?: string;
  capabilityId?: string;
  /** agentk / mock */
  source?: string;
  /** 入参 JSON Schema */
  inputSchema?: Record<string, unknown>;
}

/** agent-config 返回的可选 Agent（用于离线评测提交） */
export interface AgentConfigAgent {
  /** aop_agent.id */
  agentId: number;
  /** Agent 名称 */
  name: string;
  /** 已发布版本主键 */
  agentVersionId?: number;
  /** 来源：official / mine */
  source?: 'official' | 'mine' | string;
}

/** agent-config 返回的会话策略 */
export interface AgentConfigSessionMode {
  /** 提交离线跑数时使用的 sessionMode */
  code: string;
  /** 展示名称 */
  label: string;
  /** 策略说明 */
  description: string;
  /** 后端支持的别名，仅用于说明 */
  aliases?: string[];
}

/** Agent 配置（GET /agent-config） */
export interface AgentConfig {
  /** 可选 Agent 列表（离线评测提交用） */
  agents: AgentConfigAgent[];
  /** 离线评测会话策略选项 */
  sessionModes: AgentConfigSessionMode[];
  /** 默认会话策略 */
  defaultSessionMode: string;
  /** 配置版本（catalogVersion）；旧字段，可能缺省 */
  version?: string;
  /** 目录级默认 chat 模型；旧字段，可能缺省 */
  defaultModel?: string;
  /** 可选模型列表；旧字段，可能缺省 */
  models?: string[];
  /** 可选工具列表；旧字段，可能缺省 */
  tools?: string[];
  /** 可选 Pipeline 列表（含 inputSchema）；旧字段，可能缺省 */
  pipelines?: AgentConfigPipeline[];
  /** 工具候选模型映射；旧字段，可能缺省 */
  toolModels?: Record<string, string[]>;
}

/** 离线跑数任务提交参数 */
export interface OfflineJobSubmitParams {
  /** 数据集 ID（须为 usage=EVAL；来自上传接口返回的 data.id） */
  datasetId: number;
  /** aop_agent.id */
  agentId: number;
  /** 值来自 agent-config.sessionModes[].code */
  sessionMode: string;
}

/** 离线跑数任务（OfflineJobView） */
export interface OfflineJob {
  /** 业务 ID，形如 eval_xxx */
  bizId: string;
  /** 数据集 ID */
  datasetId: number;
  /** aop_agent.id */
  agentId: number;
  /** 会话策略；历史任务可能缺省 */
  sessionMode?: string;
  /** 已发布版本主键 */
  agentVersionId: number;
  /** Agent 名称 */
  agentName: string;
  /** profile.defaultModel 快照 */
  model: string;
  /** System Prompt 快照 */
  systemPrompt: string;
  /** 工具快照 */
  tools?: string[];
  /** profile.pipelines[0].name */
  pipeline?: string | null;
  /**
   * @deprecated 文档已划掉；P0 固定 2，前端不再展示
   */
  concurrency?: number;
  /** 状态：PENDING / RUNNING / SUCCEEDED / FAILED；初始 PENDING */
  status: string;
  /** 用例总数 */
  total: number;
  /** 失败用例数；初始 0 */
  errors: number;
  /** 创建人 */
  creator: string;
  /** 创建时间 */
  createdAt: string;
  /** 开始时间（进入 RUNNING 后填充） */
  startedAt?: string;
  /** 完成时间（SUCCEEDED / FAILED 后填充） */
  finishedAt?: string;
}

/** 离线跑数任务列表查询参数 */
export interface OfflineJobQueryParams {
  /** 当前页码，默认 1 */
  current?: number;
  /** 每页条数，默认 10，最大 100 */
  pageSize?: number;
}

/** 离线跑数任务列表响应 */
export interface OfflineJobQueryResponse {
  records: OfflineJob[];
  total: number;
  current: number;
  pageSize: number;
  pages: number;
}

/** 离线跑数用例执行结果（cell） */
export interface JobCell {
  /** 用例标识（对应数据集 tasks[].id） */
  taskKey: string;
  /** 原始 prompt（提交时从 dataset_task 复制） */
  prompt?: string;
  /** 执行状态：QUEUED / RUNNING / DONE / ERROR */
  status: string;
  /** AgentK 会话 ID（执行成功后填充） */
  agentkSessionId?: string;
  /** 最后一轮 roundId（agent-k 已无独立 run 实体） */
  agentkRunId?: string;
  /** Pipeline 执行 ID（勾选 pipeline 且发起过 pipeline-runs 后填充） */
  pipelineExecId?: string;
  /** 本 cell 产出媒体 URL（可直接渲染） */
  outputUris?: string[];
  /** 指标 JSON 字符串（结构见 CellMetrics） */
  metricsJson?: string;
  /** 失败原因；双通道失败时合并 loop / pipeline 两侧 */
  error?: string;
  /** 开始时间 */
  startedAt?: string;
  /** 完成时间 */
  finishedAt?: string;
}

/** 用例指标（解析 metricsJson 后的结构） */
export interface CellMetrics {
  steps: number;
  inputTokens: number;
  outputTokens: number;
  durationMillis: number;
  toolCalls: number;
  toolErrors: number;
}

/** Trace 中消息 part（由 events 派生，用于对话视图渲染） */
export interface TraceItemPart {
  /** part 类型：text / reasoning / tool-call / tool-result */
  type: string;
  /** 文本内容 */
  text?: string;
  /** 原始事件载荷，供提取图片、视频和音频产物 */
  mediaSource?: unknown;
  /** 工具名（tool-call 类型） */
  toolName?: string;
  /** 工具参数（tool-call 类型） */
  toolArgs?: string;
  /** 工具输出（tool-result 类型） */
  toolOutput?: string;
}

/** Trace 中消息条目（由 events 派生，用于对话视图渲染） */
export interface TraceItem {
  /** 序号 */
  seq: number;
  /** 角色：user / assistant */
  role: string;
  /** 消息片段列表 */
  parts: TraceItemPart[];
  /** 创建时间 */
  createdAt: string;
}

/** Trace 中事件 data（按 kind 不同结构不同） */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export interface TraceEventData extends Record<string, any> {}

/**
 * Trace 中事件（agent-k wire 信封）
 * 常见 kind：user.input / round.started / round.completed / assistant.text /
 * assistant.tool_call / tool.exec_succeeded / tool.exec_failed / error / interrupted
 */
export interface TraceEvent {
  /** 会话内单调序号，从 1 起 */
  seq: number;
  /** 事件类型 */
  kind: string;
  /** 事件载荷 */
  data: TraceEventData;
  /** 所属模型轮次 ID（会话级事件为空） */
  roundId?: string;
  /** 事件时间（ISO-8601） */
  createdAt: string;
}

/** Trace 中的 Pipeline 执行快照（勾选 pipeline 时出现） */
export interface TracePipelineSnapshot {
  /** pipeline-runs 执行 ID */
  execId: string;
  /** Pipeline 名 */
  pipeline: string;
  /** 执行状态：completed / failed / running */
  status: string;
  /** agent-k 返回的 Pipeline snapshot */
  snapshot?: Record<string, unknown>;
}

/** Trace 快照（由 agent-k 会话 events 拼成；勾选 pipeline 时合并 pipeline 节点） */
export interface TraceDetail {
  /** agent-k 会话 ID */
  sessionId: string;
  /** 执行结果：completed / failed */
  status: string;
  /** 失败原因（双通道失败时为两侧合并结果） */
  error?: string;
  /** Agent Loop 侧错误（仅双通道场景） */
  loopError?: string;
  /** Pipeline 侧错误（仅双通道场景） */
  pipelineError?: string;
  /** Pipeline 执行快照 */
  pipeline?: TracePipelineSnapshot;
  /** 首条事件时间（ISO-8601） */
  createdAt?: string;
  /** 末条事件时间（ISO-8601） */
  finishedAt?: string;
  /** 最后一轮 roundId */
  lastRoundId?: string;
  /** 会话事件列表（含可选 pipeline 节点） */
  events: TraceEvent[];
}

/** 导出接口 cell（字段为后端 snake_case） */
export interface OfflineJobExportCell {
  taskKey: string;
  prompt?: string;
  status: string;
  agentk_session_id?: string;
  agent_run_id?: string;
  metrics_json?: CellMetrics | Record<string, unknown>;
  trace_json?: TraceDetail | Record<string, unknown>;
  error_message?: string | null;
  started_at?: string;
  finished_at?: string;
  output_uris?: string[];
}

/** GET /offline-jobs/export 返回的任务产出 */
export interface OfflineJobExport {
  bizId: string;
  datasetId: number;
  status: string;
  model: string;
  pipeline?: string | null;
  cells: OfflineJobExportCell[];
}
