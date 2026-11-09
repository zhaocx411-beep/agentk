/**
 * 数据回看（管理员全量 Session）- 类型定义
 */

/** Session 回看查询参数（对齐 POST /platform/admin/chat/sessions/query） */
export interface SessionReviewQueryParams {
  /** 当前页码（必填） */
  page: number;
  /** 每页条数（必填） */
  pageSize: number;
  /** AgentLoop ID */
  agentLoopId?: number;
  /** AgentLoop 版本 ID */
  agentLoopVersionId?: number;
  /** Workflow ID */
  workflowId?: number;
  /** Workflow 版本 ID */
  workflowVersionId?: number;
  /** 用户 ID */
  userId?: string;
  /** 会话状态（如 active / failed / closed） */
  status?: string;
  /** 创建时间起，格式 YYYY-MM-DD HH:mm:ss */
  createdFrom?: string;
  /** 创建时间止，格式 YYYY-MM-DD HH:mm:ss */
  createdTo?: string;
}

/** Session 回看记录 */
export interface SessionReviewRecord {
  /** 主键 ID */
  id: number;
  /** 对外公开 ID（pcs_ 前缀） */
  publicId: string;
  /** agentk 侧会话 ID（ses_ 前缀），agentk 系列接口使用它；raw_profile 异常会话可能缺失 */
  agentkSessionId?: string;
  /** 归属的 Agent 版本 ID（agent_version 类会话） */
  agentVersionId?: number;
  /** 归属的 Workflow 版本 ID */
  workflowVersionId?: number;
  /** 用户 ID */
  userId: string;
  /** 租户 ID */
  tenantId: string;
  /** 会话标题 */
  title: string;
  /** 会话使用的 Profile 快照（历史字段名沿用 profileYaml；新版接口返回 Agent-k Profile JSON object，旧数据可能是 YAML 字符串） */
  profileYaml: string | Record<string, unknown>;
  /** Profile 版本 */
  profileVersion: string;
  /** 生效模型 */
  effectiveModel: string;
  /** 会话规格快照（JSON 字符串，含 tools 等） */
  specJson?: string;
  /** 生效工具列表（JSON 数组字符串） */
  effectiveToolsJson?: string;
  /** 配置来源（如 raw_profile） */
  configSource: string;
  /** 会话被拒绝时的原因 */
  rejectReason: string;
  /** 会话类型（agent_loop / pipeline） */
  sessionKind?: string;
  /** 会话状态 */
  status: string;
  /** 最新事件序号 */
  lastEventSeq: number;
  /** 当前活跃 Run 状态（idle / running 等） */
  activeRunStatus: string;
  /** 最近一次 execution，切换会话时可用于续订 stream */
  lastExecutionId?: string;
  lastAgentkRunId?: string;
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/** Session 回看分页响应 */
export interface SessionReviewResponse {
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  pageSize: number;
  /** 总条数 */
  total: number;
  /** 记录列表 */
  records: SessionReviewRecord[];
}

/** 单会话案例聚合查询参数 */
export interface SessionCaseQueryParams {
  messageLimit?: number;
  runLimit?: number;
  callLogLimit?: number;
}

/** 案例聚合 - Run 摘要 */
export interface SessionCaseRun {
  id: number;
  sessionId: number;
  agentkRunId: string;
  triggerKind: string;
  status: string;
  lastEventSeq?: number;
  model?: string;
  profileVersion?: string;
  createdAt?: string;
  startedAt?: string;
  finishedAt?: string;
}

/** 案例聚合 - 消息 */
export interface SessionCaseMessage {
  id: number;
  sessionId: number;
  runId?: number;
  agentkItemSeq?: number;
  role: string;
  /** JSON 字符串，常见形态 {"text":"..."} */
  contentJson: string;
  status?: string;
  createdAt?: string;
}

/** 案例聚合 - 调用日志 */
export interface SessionCaseCallLog {
  id: number;
  bizType: string;
  bizId: number;
  api: string;
  requestJson?: string;
  status: string;
  httpStatus?: number;
  agentkSessionId?: string;
  agentkRunId?: string;
  createdAt?: string;
  finishedAt?: string;
}

/** 案例聚合里的 session 摘要（字段比列表记录更松） */
export interface SessionCaseSession {
  id: number;
  publicId: string;
  agentkSessionId?: string;
  userId?: string;
  title?: string;
  status?: string;
  activeRunStatus?: string;
  profileVersion?: string;
  effectiveModel?: string;
  effectiveToolsJson?: string;
  configSource?: string;
  sessionKind?: string;
  createdAt?: string;
  updatedAt?: string;
}

/**
 * GET /platform/admin/chat/sessions/by-agentk/{agentkSessionId}/case
 * 运营回看复现：session + runs + messages + callLogs
 */
export interface SessionCaseData {
  session: SessionCaseSession;
  runs: SessionCaseRun[];
  messages: SessionCaseMessage[];
  callLogs: SessionCaseCallLog[];
}

/** Pipeline 执行产物 */
export interface PipelineExecArtifact {
  uri: string;
  type?: string;
  model?: string;
  description?: string;
}

/** GET /platform/chat/pipeline-execs/by-agentk/{agentkSessionId} 单条记录 */
export interface PipelineExecRecord {
  id: number;
  publicId: string;
  tenantId?: string;
  userId?: string;
  sessionId: number;
  workflowVersionId?: number;
  agentkSessionId?: string;
  agentkExecId?: string;
  pipelineName: string;
  argsJson?: Record<string, unknown>;
  outputJson?: {
    artifacts?: PipelineExecArtifact[];
    clipCount?: number;
    [key: string]: unknown;
  };
  profileVersion?: string;
  status: string;
  error?: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt?: string;
  updatedAt?: string;
}
