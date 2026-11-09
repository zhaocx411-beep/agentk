/**
 * Agent Playground - 类型定义
 * 对齐 agent-k 后端 internal/run/events.go + internal/types/entities.go
 */
import { inferModelOptionsFrom } from '@/utils/modelField';

/** Tool 注册声明（capability.tool） */
export interface CapabilityToolDecl {
  name: string;
  description: string;
  /** background 表示提交后异步闭合，前端按后台任务展示 */
  mode: 'blocking' | 'background';
}

/** Pipeline 注册声明（capability.pipeline） */
export interface CapabilityPipelineDecl {
  name: string;
  description: string;
}

/** 一条 Worker 上报的能力契约（对齐 GET /agent/operation/backend/agentk/capabilities） */
export interface Capability {
  /** 全局能力标识，形如 <owner>.<snake_case_capability> */
  id: string;
  /** Temporal 实现种类 */
  kind: 'workflow' | 'activity';
  description: string;
  /** 入参 JSON Schema */
  inputSchema?: Record<string, unknown>;
  /** 输出 JSON Schema */
  outputSchema?: Record<string, unknown>;
  /** 单次执行超时上限 */
  timeoutSeconds?: number;
  /** 注册为 Tool 时的 Profile 引用串（tool:<id>） */
  toolRef?: string;
  /** 注册为 Pipeline 时的 Profile 引用串（pipeline:<id>） */
  pipelineRef?: string;
  tool?: CapabilityToolDecl;
  pipeline?: CapabilityPipelineDecl;
}

/** capabilities 接口原始响应 */
export interface CapabilitiesResponse {
  capabilities: Capability[];
}

/** 目录中可启用的工具（由 capability.tool 派生） */
export interface CatalogTool {
  /** 会话内 canonical name */
  name: string;
  /** Catalog Tool ref；内置工具没有 ref */
  ref?: string;
  description: string;
  mode?: 'blocking' | 'background';
}

/** 目录中可暴露的 Pipeline（由 capability.pipeline 派生） */
export interface CatalogPipeline {
  name: string;
  ref: string;
  description: string;
  timeoutSeconds?: number;
  /** Pipeline 入参 JSON Schema，运行页据此渲染表单 */
  inputSchema?: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
}

/**
 * 页面消费的能力目录（tools / pipelines）
 * 模型清单见独立的 ModelCatalog（GET /agentk/models）
 */
export interface CapabilityCatalog {
  capabilities: Capability[];
  tools: CatalogTool[];
  /** 工具名列表，用于勾选与展示 */
  toolNames: string[];
  pipelines: CatalogPipeline[];
}

/** 模型能力类别（对齐 GET /agentk/models → data.models[].kind） */
export type ModelKind =
  | 'chat'
  | 'video'
  | 'image'
  | 'image_edit'
  | 'media_understand'
  | 'search'
  | 'tts'
  | 'asr'
  | (string & Record<never, never>);

/** 对话模型接收消息附件的能力（GET /agentk/models → models[].attachments） */
export interface ModelAttachmentCapabilities {
  /** 后端汇总模式；具体发送能力以前三个布尔字段为准 */
  mode: 'none' | 'image' | 'all' | (string & Record<never, never>);
  image: boolean;
  video: boolean;
  audio: boolean;
}

/** 模型目录中的单条条目 */
export interface CatalogModel {
  /** 目录条目名；写入 profile.defaultModel / toolDefaults.<tool>.model */
  name: string;
  kind: ModelKind;
  /** 协议方言；video 缺省可按 kling 理解，chat 通常为空 */
  api?: string | null;
  /** 候选差异说明（偏 LLM 消费） */
  description?: string | null;
  /** 派生工具名；chat 为 null */
  tool?: string | null;
  /** 当前模型支持直接传入的消息附件类型；缺省时按不支持处理 */
  attachments?: ModelAttachmentCapabilities;
}

/**
 * 模型目录（对齐 GET /agent/operation/backend/agentk/models）
 * 全平台选模型（主对话 / 工具子模型 / Pipeline 入参）统一走这份目录
 */
export interface ModelCatalog {
  kconfKey: string;
  version?: string | null;
  /** 全部条目名（保序） */
  names: string[];
  /** 对话模型名；对应 profile.defaultModel */
  chatModels: string[];
  /** 工具 → 候选模型列表；对应 toolDefaults.<tool>.model */
  toolModels: Record<string, string[]>;
  models: CatalogModel[];
}

/**
 * 工具名单明细项（对齐 GET /agent/operation/backend/agentk/tools → data.tools[]）
 */
export interface AgentkToolItem {
  name: string;
  description?: string;
  /** tool:agentk.xxx */
  ref?: string;
  mode?: string;
  capabilityId?: string;
  /** agentk / mock */
  source?: string;
}

/** 工具名单明细（对齐 GET /agentk/tools） */
export interface AgentkToolCatalog {
  source?: string;
  tools: AgentkToolItem[];
}

/** Profile.tools 的一项：本地工具选择或 Catalog Tool ref */
export interface ProfileToolRef {
  /** Catalog Tool ref（tool:<owner>.<name>）；省略时按 name 选本地内置工具 */
  ref?: string;
  /** 本会话提供给 LLM 的 canonical name */
  name: string;
  /** 追加到工具说明里的会话级使用提示 */
  instructions?: string;
}

/** Profile.pipelines 的一项：ref-only 的 Pipeline 暴露声明 */
export interface ProfilePipelineRef {
  /** Catalog Pipeline ref（pipeline:<owner>.<name>） */
  ref: string;
  /** 本会话对外 canonical name */
  name: string;
  displayName?: string;
}

/**
 * 会话行为定义文档（对齐 agent-k Profile schema）
 * 服务端 additionalProperties: false —— 未知字段不是忽略而是整份 400，
 * 因此这里不能再出现 toolModels / workflows 等历史字段
 */
export interface BaseProfile {
  /** 主 agent 模型；实例模型目录有缺省时省略，由服务端回填 */
  defaultModel?: string;
  /** 主 agent 的 system prompt */
  system?: string;
  /** 启用的工具列表（保序，即喂给模型的顺序） */
  tools?: ProfileToolRef[];
  /** 按工具名配置的顶层默认入参，工具须已在 tools 中启用 */
  toolDefaults?: Record<string, Record<string, unknown>>;
  /** 低代码 HTTP 工具声明 */
  httpTools?: unknown[];
  /** 向本会话暴露的 Pipeline */
  pipelines?: ProfilePipelineRef[];
}

/** 会话信息（由平台会话记录映射而来，id 取 publicId，用于后续 agentk 接口） */
export interface Session {
  id: string;
  tenantId?: string;
  title: string;
  spec?: SessionSpec;
  createdAt?: string;
  /** 会话状态（active / failed / closed 等） */
  status?: string;
  /** 当前活跃 Run 状态：idle / running / waiting_job / completed / failed */
  activeRunStatus?: string;
  /** 最近一次 execution（进行中时可用来续订 stream） */
  lastExecutionId?: string;
  /** 同 lastExecutionId，部分接口字段名 */
  lastAgentkRunId?: string;
  /* —— 以下为创建时的配置快照，切换历史会话时反向回显到配置项 —— */
  /** 归属的 AgentLoop 版本 ID（configSource=agent_version 的会话） */
  agentVersionId?: number;
  /** 配置来源（agent_version / raw_profile 等） */
  configSource?: string;
  /** 会话创建时的 Profile 快照（新版为 Agent-k Profile JSON object，旧数据可能是 YAML 字符串） */
  profileYaml?: string | Record<string, unknown>;
  /** 会话生效模型 */
  effectiveModel?: string;
  /** 会话生效工具列表（JSON 数组字符串） */
  effectiveToolsJson?: string;
  /** 会话规格快照（JSON 字符串，含 tools 等） */
  specJson?: string;
}

/** 历史会话过滤条件（对齐平台会话查询接口，WorkflowRunner 按 workflowId 隔离用） */
export interface SessionFilter {
  agentLoopId?: number;
  agentLoopVersionId?: number;
  workflowId?: number;
  workflowVersionId?: number;
}

/** 语音合成发消息参数（agent-loop 27 等 TTS 场景必填） */
export interface SendMessageTtsOptions {
  engine: string;
  voiceId: string;
}

/** 虚拟试穿发消息参数（agent-loop 32） */
export interface VirtualTryOnUserInput {
  productImageUrls: string[];
  modelImageUrl?: string;
}

/** V3 发消息附件类型 */
export type SendMessageAttachmentType = 'image' | 'video' | 'audio';

/** POST /sessions/{id}/messages V3 附件结构 */
export interface SendMessageAttachment {
  type: SendMessageAttachmentType;
  uri: string;
  /** 图片可省略；视频、音频必须提供 */
  mimeType?: string;
}

export interface SendMessageOptions {
  tts?: SendMessageTtsOptions;
  attachments?: SendMessageAttachment[];
  /** 官方虚拟试穿的兼容扩展参数 */
  userInput?: VirtualTryOnUserInput;
}

/** TTS 音色 */
export interface TtsVoiceOption {
  voiceId: string;
  displayName: string;
}

/** TTS 引擎（GET /agentk/tts/voices） */
export interface TtsEngineOption {
  engine: string;
  displayName: string;
  tool: string;
  model: string;
  defaultVoiceId: string;
  voices: TtsVoiceOption[];
}

/** 发送消息返回（对齐 /sessions/{id}/messages；demoui 会带回落库 events） */
export interface RunMessageResponse {
  id?: string;
  sessionId?: string;
  /** 本次执行 ID，SSE 订流 query 必填 */
  executionId?: string;
  status?: string;
  profileVersion?: string;
  createdAt?: string;
  /** 落库后的账本事件（含 user.input），有则优先用其渲染用户气泡 */
  events?: SessionLedgerEvent[];
}

/** 会话类型：agent_loop = Agent 调试会话；pipeline = Pipeline 会话 */
export type SessionKind = 'agent_loop' | 'pipeline';

/** 获取会话列表请求参数（对齐 POST /agent/operation/backend/platform/chat/sessions/query） */
export interface GetSessionsParams {
  /** 当前页码（必填） */
  page: number;
  /** 每页条数（必填） */
  pageSize: number;
  /** 会话类型（区分 agent 与 Pipeline 会话） */
  sessionKind?: SessionKind;
  /** AgentLoop ID（agent-loop 类会话过滤） */
  agentLoopId?: number;
  /** AgentLoop 版本 ID */
  agentLoopVersionId?: number;
  /** Workflow ID（workflow 类会话过滤） */
  workflowId?: number;
  /** Workflow 版本 ID */
  workflowVersionId?: number;
}

/** 历史会话列表（getSessions 已把平台记录映射为 Session） */
export interface SessionListResult {
  sessions: Session[];
  /** 是否还有下一页（数组形态响应按 pageSize 估算） */
  hasMore: boolean;
  page: number;
  pageSize: number;
}

/**
 * 获取会话账本请求参数
 * 对齐 GET /platform/admin/chat/sessions/by-agentk/{agentkSessionId}/events
 */
export interface GetSessionEventsParams {
  /** 页码（从 1 起） */
  page: number;
  /** 每页条数 */
  pageSize: number;
}

/**
 * 平台账本原始记录（snake/camel 混用字段以接口为准）
 * 按 seq 倒序分页返回
 */
export interface PlatformChatSessionEventRecord {
  id: number;
  sessionId: number;
  runId?: number;
  agentkRunId?: string;
  agentkSessionId: string;
  agentkEventId?: string;
  roundId?: string;
  seq: number;
  kind: string;
  /** JSON 字符串，需前端解析为对象 */
  payloadJson?: string;
  createdAt: string;
  agentkCreatedAt?: string;
}

/** 会话账本事件（结构与 session stream 的持久化事件一致） */
export interface SessionLedgerEvent {
  seq: number;
  roundId?: string;
  /** 所属 execution（平台字段 agentkRunId） */
  executionId?: string;
  /** agent-k 全局事件 ID；与 stream 帧 data.eventId 一致，用于历史回放与续流去重 */
  eventId?: string;
  kind: string;
  /** agent-k / 平台常见字段；部分代理层会改成 payload */
  data?: unknown;
  payload?: unknown;
  /** 平台接口原始字段，映射后通常已写入 data/payload */
  payloadJson?: string;
  createdAt: string;
}

/** 会话账本分页响应（已映射为 SessionLedgerEvent） */
export interface SessionEventsResponse {
  events: SessionLedgerEvent[];
  hasMore: boolean;
  page: number;
  pageSize: number;
  total: number;
}

/** 创建会话请求参数（对齐 POST /agent/operation/backend/agentk/sessions） */
export interface CreateSessionRequest {
  /** 会话标题；不传时由后端根据首条消息自动生成 */
  title?: string;
  /** 会话类型（必填）：agent_loop / pipeline */
  sessionKind: SessionKind;
  /** 会话级 Profile JSON 对象（新版后端不再接受 YAML 字符串） */
  profile?: BaseProfile;
  /** 平台已发布 AgentLoop 版本 ID；不传 profile 时使用该版本配置 */
  agentLoopVersionId?: number;
  /** 平台 Workflow 版本 ID；平台将 definitionJson 绑定到本会话 */
  workflowVersionId?: number;
}

/* ========== Run / Stream 事件 ========== */

/** 运行事件（会话 SSE / 账本回放共用） */
export interface RunEvent {
  seq: number;
  kind: string;
  payload: unknown;
  createdAt: string;
  /** 所属 execution；seq 仅在该 execution 内递增 */
  executionId?: string;
  /** agent-k 全局事件 ID；stream 与 /events 账本对齐时可据此去重 */
  eventId?: string;
}

/** 会话规格（窄选择：选模型、收窄工具） */
export interface SessionSpec {
  /** 须为 agent-k 模型目录中的 chat 模型 */
  model?: string;
  /** 须为 profile 已启用工具的子集 */
  tools?: string[];
  /** 各工具选用的模型（key=工具名, value=模型名），全量带上便于观察后端实际使用 */
  toolModels?: Record<string, string>;
}

/* ========== ask_user 问卷 ========== */

/** ask_user 问卷题目（ask_user.requested 事件 payload） */
export interface AskUserQuestion {
  /** 题目 ID，提交答案时作为 questionId */
  id: string;
  /** 题目标签（如「画幅」） */
  label?: string;
  /** 问题文案 */
  question: string;
  /** 选项文案列表 */
  options: string[];
  /** 是否多选 */
  multiSelect?: boolean;
}

/** ask_user.requested payload */
export interface AskUserRequestedPayload {
  toolCallId?: string;
  questions?: AskUserQuestion[];
}

/** 待回答的问卷（前端交互状态） */
export interface PendingAskUser {
  toolCallId: string;
  questions: AskUserQuestion[];
  /** 用于切回会话时识别并撤下已被新 execution 接管的旧问卷 */
  executionId?: string;
}

/** 单题答案（selected 填选项文案本身，不是下标） */
export interface AskUserAnswer {
  questionId: string;
  selected: string[];
}

/** 对话流中的问卷状态；已回答/被文字接管后仍保留为只读历史卡片 */
export type AskUserCardStatus = 'pending' | 'submitted' | 'dismissed';

/** 提交问卷答案请求体（对齐 POST /sessions/{id}/answers） */
export interface SubmitAnswersRequest {
  toolCallId: string;
  answers: AskUserAnswer[];
}

/** 提交问卷答案返回；回答会启动新的 execution，后续 SSE 必须改绑该 ID */
export interface SubmitAnswersResponse {
  executionId?: string;
}

/** 后台任务结果数据（对齐后端 jobpoller 注入的 data-job-result 载荷） */
export interface JobResultData {
  /** 任务类型（如 generate_video@kling-v3） */
  kind?: string;
  jobId?: string;
  toolCallId?: string;
  /** 任务产物（含 videoUrl/imageUrl 等媒体链接） */
  result?: unknown;
  error?: string;
}

/** 对话附件（上传结果）；Agent 支持消息 V3 时转换为 attachments，否则拼入 text。 */
export interface MessageAttachment {
  /** 优先选用 blobUrl，其余 URL 仅用于兼容兜底 */
  url: string;
  mediaType?: string;
  name?: string;
  fileId?: string;
  fileUrl?: string;
  cdnUrl?: string;
  blobUrl?: string;
  /** 兼容文档解析等旧链路的发送地址；预览仍走 url / cdnUrl */
  sendUrl?: string;
  contentType?: string;
  /** 已知字节数（本地上传结果）；素材库勾选通常没有，发送前再探测 */
  size?: number;
}

/** 消息 Part */
export type MessagePart =
  | { type: 'text'; text: string }
  | { type: 'reasoning'; text: string }
  | { type: 'image'; url: string; name?: string }
  | { type: 'file'; url: string; name?: string; mediaType?: string; contentType?: string }
  | {
      type: 'tool-call';
      toolCallId: string;
      toolName: string;
      input: unknown;
      /** assistant.tool_call 原始 payload，供调试回看完整展示 */
      payload?: unknown;
    }
  | { type: 'tool-result'; toolCallId: string; output: unknown; isError?: boolean }
  | { type: 'data-job-result'; data: JobResultData }
  | { type: 'data-job'; jobId: string }
  | {
      type: 'ask-user';
      toolCallId: string;
      questions: AskUserQuestion[];
      answers?: AskUserAnswer[];
      status: AskUserCardStatus;
    };

/** 消息项 */
export interface MessageItem {
  role: 'user' | 'assistant' | 'tool';
  parts: MessagePart[];
  /** 所属 execution；用于迟到回放事件在同一 execution 内按 id 归位 */
  executionId?: string;
  /** 关联的 runId（历史消息回显用） */
  runId?: string;
  /** 创建时间，用于时间线排序 */
  createdAt?: string;
  /** 后端事件 id（seq），同一批 delta 的 ts 相同，用 id 区分先后顺序 */
  id?: number;
}

/** 运行统计 */
export interface RunStats {
  steps: number;
  inTok: number;
  outTok: number;
  modelMs: number;
  tools: number;
}

/** 账本事件 */
export interface LedgerEvent {
  seq: number;
  kind: string;
  summary: string;
}

/** 工具卡片状态 */
export interface ToolCardState {
  toolCallId: string;
  toolName: string;
  status: 'running' | 'ok' | 'err';
  duration?: number;
  input?: unknown;
  output?: unknown;
}

/** Job 唤醒策略 */
export type WakePolicy = 'notify' | 'resume' | 'workflow';

/** 后台任务（对齐 JobSubmittedPayload / JobCompletedPayload / JobFailedPayload） */
export interface JobEvent {
  jobId: string;
  toolCallId?: string;
  kind: string;
  remoteTaskId?: string;
  wakePolicy?: WakePolicy;
  /** cancelled：用户主动停止会话后，前端把未完成的 job 收口，不再显示「进行中」 */
  status: 'submitted' | 'completed' | 'failed' | 'cancelled';
  result?: unknown;
  error?: string;
  wakeRunId?: string;
  /** 远端任务状态（/jobs 轮询刷新，仅展示用，如 processing） */
  remoteStatus?: string;
  /** 已等待时长毫秒（/jobs 轮询刷新，仅展示用） */
  elapsedMs?: number;
}

/* ========== 事件 Payload 类型 ========== */
/** 对齐后端 internal/run/events.go */

/** run.started payload */
export interface RunStartedPayload {
  workerId?: string;
  profileVersion?: string;
  model?: string;
  promptSha256?: string;
  tools?: string[];
  /** 后端事件 id */
  id?: number;
  /** 后端毫秒时间戳 */
  ts?: number;
  /** 各工具实际使用的模型（key=工具名, value=模型名） */
  toolModels?: Record<string, string>;
}

/** step.completed payload */
export interface StepCompletedPayload {
  step?: number;
  usage?: {
    inputTokens?: number;
    outputTokens?: number;
    cacheReadTokens?: number;
  };
  modelMillis?: number;
  id?: number;
  ts?: number;
}

/** item.completed payload */
export interface ItemCompletedPayload {
  seq?: number;
  item?: {
    role?: 'assistant' | 'tool' | 'user';
    parts?: StreamItemPart[];
  };
  id?: number;
  ts?: number;
}

/** tool.executed payload */
export interface ToolExecutedPayload {
  toolCallId?: string;
  toolName?: string;
  isError?: boolean;
  durationMillis?: number;
  id?: number;
  ts?: number;
}

/** run.completed payload */
export interface RunCompletedPayload {
  steps?: number;
  id?: number;
  ts?: number;
}

/** run.failed payload */
export interface RunFailedPayload {
  error?: string;
  steps?: number;
  id?: number;
  ts?: number;
}

/** job.submitted payload（对齐 types.JobSubmittedPayload） */
export interface JobSubmittedPayload {
  jobId?: string;
  toolCallId?: string;
  kind?: string;
  remoteTaskId?: string;
  wakePolicy?: WakePolicy;
}

/** job.completed payload（对齐 types.JobCompletedPayload） */
export interface JobCompletedPayload {
  jobId?: string;
  toolCallId?: string;
  kind?: string;
  wakePolicy?: WakePolicy;
  result?: unknown;
  wakeRunId?: string;
}

/** job.failed payload（对齐 types.JobFailedPayload） */
export interface JobFailedPayload {
  jobId?: string;
  toolCallId?: string;
  kind?: string;
  wakePolicy?: WakePolicy;
  error?: string;
}

/** 会话流的瞬态 delta payload（SSE event 固定为 delta） */
export interface DeltaPayload {
  roundId: string;
  block: number;
  kind: 'text' | 'reasoning';
  text: string;
  /** delta 自身的瞬态 id，不是账本 seq，不能用于断线续传 */
  id?: number;
}

/** 流式 item part（SSE 推送的 item 结构） */
export interface StreamItemPart {
  type?: string;
  text?: string;
  toolCallId?: string;
  toolName?: string;
  input?: unknown;
  output?: unknown;
  isError?: boolean;
  payload?: unknown;
}

/** 编译后的 Agent Version */
export interface CompiledAgentVersion {
  id: string;
  version: number;
  profileYaml: string;
  spec: {
    model: string;
    tools?: string[];
  };
  contentHash: string;
}

/** Agent 配置编辑状态 */
export interface AgentEditState {
  systemPrompt: string;
  selectedModel: string;
  selectedTools: string[];
}

/* ========== Pipeline 类型 ========== */

/** Pipeline 参数字段定义（后端提供 schema，前端动态渲染表单） */
export interface WorkflowParamDef {
  /** 字段名 */
  name: string;
  /** 展示标签 */
  label: string;
  /** 输入类型（array/object 以 JSON 文本输入，提交时解析为对应类型） */
  type: 'text' | 'number' | 'select' | 'array' | 'object';
  /** 是否必填 */
  required?: boolean;
  /** 默认值 */
  defaultValue?: string | number;
  /** select 类型的可选项（schema enum；模型字段仅作目录为空时的回退） */
  options?: { label: string; value: string | number }[];
  /**
   * 动态模型选项：来自 GET /agentk/models
   * 有值时 UI 优先用模型目录，忽略/回退 schema.enum
   */
  optionsFrom?: 'chat' | 'image' | 'video';
  /** 占位提示 */
  placeholder?: string;
  /** 数值最小值 */
  min?: number;
  /** 数值最大值 */
  max?: number;
}

/** Pipeline 定义 */
export interface WorkflowDefinition {
  /** 唯一标识 */
  id: string;
  /** 展示名称（如 storyboard_film） */
  name: string;
  /** 描述 */
  description: string;
  /** 节点列表（如 board、clip_gen、compose） */
  nodes: WorkflowNodeDef[];
  /** 参数定义 */
  params: WorkflowParamDef[];
}

/** Pipeline 节点定义 */
export interface WorkflowNodeDef {
  /** 节点唯一名 */
  name: string;
  /** 展示标签 */
  label: string;
  /** 节点描述 */
  description?: string;
}

/** Pipeline 节点运行状态 */
export type WorkflowNodeStatus = 'pending' | 'running' | 'completed' | 'failed' | 'skipped';

/** Pipeline 节点运行快照 */
export interface WorkflowNodeState {
  name: string;
  label: string;
  status: WorkflowNodeStatus;
  /** 运行耗时 ms */
  duration?: number;
  /** 错误信息 */
  error?: string;
  /** 节点输出（从详情接口获取） */
  output?: unknown;
}

/** Pipeline 运行实例 */
export interface WorkflowRun {
  /** 运行 ID */
  runId: string;
  /** 关联的 Pipeline 定义 ID */
  workflowId: string;
  /** Pipeline 名称 */
  workflowName: string;
  /** 触发参数 */
  params: Record<string, unknown>;
  /** 节点运行状态 */
  nodes: WorkflowNodeState[];
  /** 整体状态 */
  status: 'running' | 'completed' | 'failed';
  /** 错误信息 */
  error?: string;
  /** Pipeline 顶层输出（如 film URL、clipCount），轮询时写入 */
  output?: unknown;
  /** 创建时间，用于时间线排序 */
  createdAt?: string;
}

/* ========== Pipeline SSE 事件 Payload ========== */

/** pipeline.started payload */
export interface WorkflowStartedPayload {
  runId?: string;
  workflowId?: string;
  workflowName?: string;
  params?: Record<string, unknown>;
  nodes?: string[];
}

/** wf.node payload（节点状态变更） */
export interface WfNodePayload {
  runId?: string;
  node?: string;
  status?: WorkflowNodeStatus;
  duration?: number;
  error?: string;
}

/** pipeline.completed payload */
export interface WorkflowCompletedPayload {
  runId?: string;
  status?: 'completed' | 'failed';
  error?: string;
}

/* ========== Pipeline 真实接口类型 ========== */

/** 后端返回的 Pipeline 摘要（对齐 GET /sessions/{sessionId}/pipelines） */
export interface WorkflowInfo {
  /** Pipeline 名称 */
  name: string;
  /** 人类可读说明 */
  description?: string;
  /** 输入 JSON Schema（对象结构），用于校验 / 表单生成 */
  inputs?: {
    type: 'object';
    properties: Record<string, WorkflowInputSchema>;
    required?: string[];
    /** 是否允许额外字段（后端新版 Schema 会显式返回 false） */
    additionalProperties?: boolean;
  };
}

/** Pipeline 输入字段的 JSON Schema 子项 */
export interface WorkflowInputSchema {
  description?: string;
  type: 'string' | 'number' | 'integer' | 'boolean' | 'array' | 'object';
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  enum?: (string | number)[];
  default?: unknown;
  /** array 类型的元素 Schema（如 images 的 items: { type: string }） */
  items?: WorkflowInputSchema;
  minItems?: number;
}

/** 获取 Pipeline 列表响应 */
export interface WorkflowsResponse {
  pipelines: WorkflowInfo[];
}

/** 启动 Pipeline 请求体（对齐 POST /sessions/{sessionId}/pipeline-runs） */
export interface RunWorkflowRequest {
  /** Workflow 名称，须存在于该 Session 的 Profile（文档必填字段） */
  workflow: string;
  /** 兼容文档示例 / 旧后端字段名 */
  pipeline?: string;
  /** 输入参数（JSON 对象，缺必填字段时上游返回 4xx） */
  inputs: Record<string, unknown>;
}

/** 启动 Pipeline 响应 */
export interface RunWorkflowResponse {
  /** 执行 ID（execId），后续查询 / 轮询用它 */
  id: string;
  /** 关联的 session ID */
  sessionId: string;
  /** Pipeline 名称 */
  name: string;
  /** 启动后多为 running */
  status: string;
  /** 入参快照 */
  inputs?: Record<string, unknown>;
  /** 官方体验接口常用 args */
  args?: Record<string, unknown>;
  /** 创建时 pin 住的 Profile 版本 */
  profileVersion: string;
  /** 创建时间 */
  createdAt: string;
}

/* ========== Pipeline 执行列表 & 详情 ========== */

/** Pipeline 执行摘要（对齐 GET /sessions/{sessionId}/pipeline-runs） */
export interface WorkflowRunInfo {
  /** 执行 ID */
  id: string;
  /** Pipeline 名称 */
  name: string;
  /** 状态：running / completed / failed */
  status: string;
  /** 入参快照（官方体验 / 多数 pipeline-runs 接口） */
  args?: Record<string, unknown>;
  /** 入参快照（部分环境字段名） */
  inputs?: Record<string, unknown>;
  /** 顶层输出（列表接口也可能直接返回） */
  output?: unknown;
  /** 媒体产物 URL（列表接口可能直出） */
  videoUrls?: string[];
  imageUrls?: string[];
  mediaArtifacts?: { type?: string; url?: string; source?: string }[];
  /** 创建时间 */
  createdAt?: string;
  /** 完成时间 */
  finishedAt?: string;
}

/** 获取 Pipeline 执行列表响应 */
export interface WorkflowRunsResponse {
  pipelineRuns: WorkflowRunInfo[];
}

/** 获取 Pipeline 执行列表参数 */
export interface GetWorkflowRunsParams {
  /** 过滤状态 */
  status?: 'running' | 'completed' | 'failed' | 'all';
}

/** Pipeline 节点执行详情 */
export interface WorkflowRunNodeDetail {
  /** 节点 ID */
  nodeId: string;
  /** 节点序号 */
  idx: number;
  /** 节点状态 */
  status: string;
  /** 节点输出 */
  output?: unknown;
  /** 重试次数 */
  attempts: number;
}

/** Pipeline 执行详情（对齐 GET /sessions/{sessionId}/pipeline-runs/{execId}） */
export interface WorkflowRunDetail {
  /** 执行 ID */
  id: string;
  /** Pipeline 名称 */
  name: string;
  /** 状态 */
  status: string;
  /** 入参快照（归一化：args 优先） */
  inputs: Record<string, unknown>;
  /** 原始 args（若接口返回） */
  args?: Record<string, unknown>;
  /** 最终输出 */
  output?: unknown;
  /** 执行失败原因 */
  error?: string;
  /** Profile 版本 */
  profileVersion: string;
  /** 创建时间 */
  createdAt: string;
  /** 完成时间 */
  finishedAt?: string;
  /** 节点执行详情 */
  nodes?: WorkflowRunNodeDetail[];
}

/* ========== 前端内部使用的 Workflow 定义（兼容旧 UI，由 WorkflowInfo 转换而来） ========== */

/** 将后端 WorkflowInfo 转换为前端 WorkflowDefinition */
export function adaptWorkflowInfo(wf: WorkflowInfo): WorkflowDefinition {
  const params: WorkflowParamDef[] = [];
  const { properties = {}, required = [] } = wf.inputs ?? {};

  for (const [key, schema] of Object.entries(properties)) {
    // 模型字段名 → 走 /agentk/models（见 utils/modelField）
    const optionsFrom = inferModelOptionsFrom(key);
    let type: WorkflowParamDef['type'] =
      schema.type === 'number' || schema.type === 'integer'
        ? 'number'
        : schema.type === 'array'
          ? 'array'
          : schema.type === 'object'
            ? 'object'
            : schema.enum || optionsFrom
              ? 'select'
              : 'text';

    if (optionsFrom) type = 'select';

    const param: WorkflowParamDef = {
      name: key,
      label: schema.description || key,
      type,
      required: required.includes(key),
      placeholder: schema.description,
    };

    if (optionsFrom) param.optionsFrom = optionsFrom;

    if (type === 'number') {
      if (schema.minimum !== undefined) param.min = schema.minimum;
      if (schema.maximum !== undefined) param.max = schema.maximum;
      if (schema.default !== undefined) param.defaultValue = schema.default as number;
    }

    if (type === 'select' && schema.enum) {
      param.options = schema.enum.map((v) => ({ label: String(v), value: v as string | number }));
      if (schema.default !== undefined) param.defaultValue = schema.default as string | number;
    }

    if (type === 'text' && schema.default !== undefined) {
      param.defaultValue = schema.default as string;
    }

    if (optionsFrom && schema.default !== undefined && param.defaultValue === undefined) {
      param.defaultValue = schema.default as string;
    }

    params.push(param);
  }

  return {
    id: wf.name,
    name: wf.name,
    description: wf.description || wf.name,
    nodes: [], // TODO: 等后端接口补上 nodes 字段后透传
    params,
  };
}
