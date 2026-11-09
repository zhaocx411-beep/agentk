// 与 cut-apiserver 的通信层接口定义（设计文档第 3 节的接口清单）。
// 阶段 A 只定义函数签名与 DTO；阶段 B 起由 ./http 提供真实 REST 实现，
// ./mock 保留作演示/无后端场景的内存实现。

import type {
  ChangeSummary,
  Media,
  MediaKind,
  Operation,
  TimelineDoc,
} from "@/domain/timeline/types";

// ---- DTO ----

/** 对齐后端 entity.ProjectRef；后端不区分 summary/detail，只有这一种形状。 */
export interface ProjectRef {
  projectId: string;
  title: string;
  /** Unix 毫秒时间戳（不是 RFC3339 字符串）。 */
  createdAtUnixMs: number;
  /** 未配置 agent-k 时项目没有绑定会话，此字段缺省。 */
  sessionId?: string;
  /** 创建项目会话时选中的 Agent/Profile。 */
  profileId?: string;
}

export interface TimelineSnapshot {
  doc: TimelineDoc;
  revision: number;
  durationInFrames: number;
  /** Optional only for compatibility with pre-sequence servers. */
  activeTimelineId?: string;
  /** Optional only for compatibility with pre-sequence servers. */
  timelines?: TimelineSummary[];
}

export interface TimelineSummary {
  timelineId: string;
  name: string;
  revision: number;
  durationInFrames: number;
  currentFrame: number;
  zoom: number;
  createdAtUnixMs: number;
  updatedAtUnixMs: number;
}

export interface UpdateTimelineViewRequest {
  requestId: string;
  currentFrame: number;
  zoom: number;
}

export interface TimelineMutationRequest {
  requestId: string;
  name?: string;
}

export interface ApplyOpsRequest {
  timelineId?: string;
  /**
   * 兼容旧后端的幂等键。仅可在完全相同请求的网络重试中复用；
   * 409/422 后修正并重提必须生成新 ID。
   */
  requestId: string;
  /**
   * 用户操作幂等标识，后端据此派生项目隔离的 Temporal UpdateID。
   * 语义与 requestId 相同：只用于完全相同 payload 的网络重试。
   */
  clientMutationId?: string;
  /** 提交时认为的服务端版本；不匹配返回 REVISION_CONFLICT。 */
  baseRevision: number;
  operations: Operation[];
}

export interface ApplyOpsResponse {
  revision: number;
  summary: ChangeSummary;
  /** 新版服务端可直接返回权威 Timeline；旧版客户端会在成功后 GET 校准。 */
  doc?: TimelineDoc;
}

export interface RestoreTimelineRequest {
  timelineId?: string;
  clientMutationId: string;
  baseRevision: number;
  doc: TimelineDoc;
}

export interface RestoreTimelineResponse {
  revision: number;
  doc: TimelineDoc;
}

export type TranscriptGranularity = "word" | "segment";

export interface TranscriptEntry {
  occurrenceId: string;
  itemId: string;
  trackId: string;
  mediaId: string;
  mediaGeneration: number;
  segmentId: string;
  wordId?: string;
  text: string;
  sourceStartMs: number;
  sourceEndMs: number;
  fromFrame: number;
  toFrame: number;
  granularity: TranscriptGranularity;
}

export interface TranscriptView {
  timelineRevision: number;
  entries: TranscriptEntry[];
  warnings: string[];
}

export interface TranscriptDeleteRange {
  fromOccurrenceId: string;
  toOccurrenceId: string;
}

export interface DeleteTranscriptRequest {
  timelineId?: string;
  clientMutationId: string;
  baseRevision: number;
  ranges: TranscriptDeleteRange[];
  mode: "ripple";
}

export interface DeletedFrameRange {
  fromFrame: number;
  toFrame: number;
}

export interface DeleteTranscriptResponse {
  revision: number;
  doc: TimelineDoc;
  summary: ChangeSummary;
  deletedRanges: DeletedFrameRange[];
  deletedDurationInFrames: number;
}

/**
 * 服务端结构化错误（HTTP 非 2xx 时 body 的形状）。对齐 platform/api 的
 * writeError/applyOps 错误映射：通用错误只有 error 字段；REVISION_CONFLICT
 * 带 latestRevision；单个 op 校验失败（422）额外带 opIndex/op。
 */
export interface ApiErrorBody {
  error: string;
  code?: string; // 如 REVISION_CONFLICT / OVERLAP / NOT_FOUND …（不是所有错误都有）
  /** REVISION_CONFLICT（409）时携带服务端当前 revision。 */
  latestRevision?: number;
  /** op 校验失败（422）时定位具体是哪一条 op。 */
  opIndex?: number;
  op?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly body: ApiErrorBody;

  constructor(status: number, body: ApiErrorBody) {
    super(`${body.code ?? "ERROR"}: ${body.error}`);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

/**
 * 会话事件信封。契约来源：cut-apiserver 的 agent-k 桥接层
 * （platform/api/agentk.go）——agent-k 已迁移到 Connect RPC，cut-apiserver
 * 把 SDK 的 Event（apiv1.Event）翻译成这个 JSON 信封：kind 是 payload
 * oneof 的点分名，data 是 payload 的 protojson（camelCase）。
 * 事件不再有整数 seq，去重/游标改用 eventId（字符串，无序含义）。
 */
export interface SessionEvent {
  /** 事件唯一 ID（evt_ 开头），用于去重；不可比大小。 */
  eventId: string;
  /** 事件所属的一次完整 Agent/Pipeline 执行。 */
  executionId?: string;
  /** 只在事件属于某次模型调用时出现。 */
  roundId?: string;
  /** 事件类别，决定 data 结构：user.input / round.* / assistant.* / tool.* / ask_user.* / execution.* / invocation.progress。 */
  kind: string;
  data: unknown;
  /** 事实写入时间（RFC3339）。 */
  createdAt: string;
}

/** GET /events 的分页响应。事件按新到旧排列；nextPageToken 空串表示没有更多。 */
export interface EventsPage {
  events: SessionEvent[];
  nextPageToken: string;
}

/**
 * Run Stream 的模型增量帧（SSE `event: delta`，protojson of apiv1.Delta）。
 * 只用于实时预览，不是定稿；模型 Activity 重试会出现新 generation，
 * 客户端应以新一代替换同 round 的旧预览。
 */
export interface DeltaFrame {
  executionId?: string;
  roundId?: string;
  generation?: string;
  index?: number;
  block?: number;
  /** protojson 的枚举名：DELTA_BLOCK_KIND_TEXT / DELTA_BLOCK_KIND_REASONING。 */
  kind?: string;
  text?: string;
}

/** streamEvents 的回调集合。 */
export interface StreamHandlers {
  /** 当前 run 内已持久化的事件（data 与 /events 信封完全相同）。 */
  onEvent: (ev: SessionEvent) => void;
  /** 模型生成中的增量预览帧。 */
  onDelta?: (frame: DeltaFrame) => void;
  /**
   * 流结束：kind = "end"（收到 stream.end 或连接自然结束）/
   * "no_active_run"（连接时 204，当前没有 active run）/ "error"。
   * 按契约收到 end 后应回退 /events?after=<lastSeq> 做最终校准（TODO 阶段 D）。
   */
  onClose?: (reason: "end" | "no_active_run" | "error") => void;
}

export interface EditorContext {
  timelineId?: string;
  /** Timeline revision observed when the message was submitted. */
  timelineRevision: number;
  /** Timeline item IDs selected in the editor, in selection order. */
  selectedItemIds: string[];
  /** Current timeline playhead in integer frames. */
  playheadFrame: number;
  selectedMediaId?: string;
  selectedCaptionItemId?: string;
}

export interface SendMessageRequest {
  text: string;
  /** 预留的 Agent/Profile 标识；旧后端会忽略，支持多 Profile 后据此选会话配置。 */
  profileId?: string;
  /**
   * UI 选中的素材 ID。当前 agentk-sdk 的 AgentMessageExecutionInput 只有 text，
   * cut-apiserver 无法结构化转发；前端同时把引用编码为 [media:ID] 文本令牌，
   * 此字段保留给未来 SDK 升级及本地展示。
   */
  mediaIds?: string[];
  /** Immutable editor state captured at send time. */
  editorContext?: EditorContext;
}

const EDITOR_CONTEXT_START = "[AGENTCUT_SYSTEM_CONTEXT_V1]";
const EDITOR_CONTEXT_END = "[/AGENTCUT_SYSTEM_CONTEXT_V1]";
const USER_MESSAGE_START = "[AGENTCUT_USER_MESSAGE]";
const USER_MESSAGE_END = "[/AGENTCUT_USER_MESSAGE]";

/**
 * Agent-K persists the SDK message text. Hide the bridge's explicit context
 * envelope when rendering user.input events, while leaving legacy messages intact.
 */
export function unwrapAgentUserMessage(text: string): string {
  if (!text.startsWith(`${EDITOR_CONTEXT_START}\n`)) return text;
  const userStart = `\n${EDITOR_CONTEXT_END}\n${USER_MESSAGE_START}\n`;
  const userStartIndex = text.indexOf(userStart);
  const userEnd = `\n${USER_MESSAGE_END}`;
  if (userStartIndex < 0 || !text.endsWith(userEnd)) return text;
  return text.slice(userStartIndex + userStart.length, -userEnd.length);
}

/** POST /messages、/answers 的响应：本次启动的 Execution ID。 */
export interface StartExecutionResponse {
  executionId: string;
}

/** ask_user 答复（对齐 SDK 的 AgentAnswersExecutionInput）。 */
export interface AnswerRequest {
  toolCallId: string;
  answers: { questionId: string; selected: string[] }[];
}

/**
 * 服务端中转上传（实际后端实现，取代设计文档第 9 节原先设想的预签名
 * 直传两段式）：一次 multipart/form-data POST 直接完成上传 + 登记素材。
 */
export interface UploadRequest {
  file: File | Blob;
  kind: MediaKind;
  /** 浏览器能探测时提供；失败可省略，由服务端 ffprobe 决定权威时长。 */
  durationMs?: number;
}

export interface UploadResponse {
  media: Media;
  revision: number;
}

export interface PrepareMediaResponse {
  workflowId: string;
  status: "started" | "already_started";
}

export interface StartAudioSeparationRequest {
  clientMutationId: string;
  timelineId?: string;
  baseRevision: number;
}

export interface AudioSeparationResult {
  revision: number;
  doc: TimelineDoc;
  audioMediaId: string;
  audioItemId: string;
  audioTrackId: string;
  reused?: boolean;
}

export type AudioSeparationStatus =
  | { workflowId: string; status: "running" }
  | { workflowId: string; status: "completed"; result: AudioSeparationResult }
  | {
      workflowId: string;
      status: "failed";
      code?: string;
      error: string;
      workflowStatus?: string;
    };

export interface StartRenderResponse {
  renderKey: string;
  revision: number;
  status: "accepted";
}

export type RenderState = "running" | "completed" | "failed";

/** running 时的分步进度：done/total 是编译出的调用步数，不是 0~1 比例。 */
export interface RenderProgress {
  done: number;
  total: number;
  current?: string;
  warnings?: string[];
}

export interface RenderStatus {
  renderKey: string;
  status: RenderState;
  progress?: RenderProgress;
  /** completed 时的产物地址（注意字段名是 Uri 不是 Url）。 */
  outputUri?: string;
  revision?: number;
  warnings?: string[] | null;
  /** failed 时由渲染 Workflow 返回的稳定错误详情。 */
  error?: string;
  /** failed 时的 Temporal Workflow 状态（如 FAILED / TIMED_OUT）。 */
  workflowStatus?: string;
}

// ---- 接口清单（与后端路由一一对应） ----

export interface CreateProjectRequest {
  title: string;
  profileId?: string;
  width?: number;
  height?: number;
  fps?: number;
}

export interface RenameProjectRequest {
  title: string;
  requestId: string;
}

/** 可供聊天选择的 Agent/Profile。当前后端未实现时 UI 使用内置默认项。 */
export interface AgentProfile {
  profileId: string;
  name: string;
  description?: string;
  isDefault?: boolean;
}

/** 生成只读分享链接的返回。分享页面与访问策略由后端负责。 */
export interface ProjectShare {
  shareId: string;
  url: string;
  expiresAt?: string;
}

export interface CutApiClient {
  // Agent/Profile（预留契约：GET /v1/profiles → {profiles:[...]}）
  listProfiles(): Promise<AgentProfile[]>;

  // 项目
  listProjects(): Promise<ProjectRef[]>; // GET    /v1/projects（响应体是 {projects:[...]}，这里返回展开后的数组）
  createProject(req: CreateProjectRequest): Promise<ProjectRef>; // POST   /v1/projects
  renameProject(projectId: string, req: RenameProjectRequest): Promise<ProjectRef>; // PATCH /v1/projects/{id}
  /** 没有独立的 GET /v1/projects/{id} 路由，后端本身也是拉列表再按 id 过滤（见 requireProject）。 */
  getProject(projectId: string): Promise<ProjectRef>;
  deleteProject(projectId: string): Promise<void>; // DELETE /v1/projects/{id}
  createProjectShare(projectId: string): Promise<ProjectShare>; // POST /v1/projects/{id}/shares

  // Timeline
  getTimeline(projectId: string, opts?: { signal?: AbortSignal }): Promise<TimelineSnapshot>; // GET /v1/projects/{id}/timeline
  createTimeline(projectId: string, req: TimelineMutationRequest): Promise<TimelineSnapshot>;
  switchTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot>;
  renameTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot>;
  deleteTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot>;
  updateTimelineView(
    projectId: string,
    timelineId: string,
    req: UpdateTimelineViewRequest,
  ): Promise<TimelineSummary>;
  applyOps(
    projectId: string,
    req: ApplyOpsRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<ApplyOpsResponse>; // POST /v1/projects/{id}/timeline/ops
  restoreTimeline(
    projectId: string,
    req: RestoreTimelineRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<RestoreTimelineResponse>; // POST /v1/projects/{id}/timeline:restore
  getTranscriptView(
    projectId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TranscriptView>; // GET /v1/projects/{id}/transcript-view
  deleteTranscriptEdits(
    projectId: string,
    req: DeleteTranscriptRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<DeleteTranscriptResponse>; // POST /v1/projects/{id}/transcript-edits:delete

  // 会话（agent-k 桥接，见 platform/api/agentk.go）
  /** POST /v1/projects/{id}/messages → 202 {executionId}，不等模型完成。 */
  sendMessage(projectId: string, req: SendMessageRequest): Promise<StartExecutionResponse>;
  /**
   * GET /v1/projects/{id}/events?pageToken={t}&limit={n}。新到旧分页：
   * 第一页不带 pageToken；nextPageToken 空串表示到底。limit 缺省 20、上限 100。
   */
  listEvents(projectId: string, opts?: { pageToken?: string; limit?: number }): Promise<EventsPage>;
  /**
   * SSE：一次 Execution 的实时旁路（GET /v1/projects/{id}/stream[?executionId=]）。
   * 不带 executionId 时后端取最新 Execution；没有进行中的 Execution 时返回
   * 204。帧：`event:` 是事件 kind（或 delta），data 与 /events 信封相同；
   * 结束发 stream.end。事件流(listEvents)才是可靠通道，SSE 只是旁路。
   * 返回取消订阅函数。
   */
  streamEvents(projectId: string, handlers: StreamHandlers, opts?: { executionId?: string }): () => void;
  /** POST /v1/projects/{id}/answers → 202 {executionId}（ask_user 表单是后续任务）。 */
  answer(projectId: string, req: AnswerRequest): Promise<StartExecutionResponse>;

  // 上传（服务端中转，一次 multipart POST 完成）
  upload(projectId: string, req: UploadRequest): Promise<UploadResponse>; // POST /v1/projects/{id}/uploads
  prepareMedia(projectId: string, mediaId: string): Promise<PrepareMediaResponse>; // POST /v1/projects/{id}/media/{mediaId}/prepare
  startAudioSeparation(
    projectId: string,
    itemId: string,
    req: StartAudioSeparationRequest,
  ): Promise<{ workflowId: string; status: "running" }>;
  getAudioSeparation(
    projectId: string,
    itemId: string,
    timelineId?: string,
  ): Promise<AudioSeparationStatus>;

  // 渲染 / 导出
  startRender(projectId: string): Promise<StartRenderResponse>; // POST /v1/projects/{id}/renders
  getRenderStatus(projectId: string, renderKey: string): Promise<RenderStatus>; // GET /v1/projects/{id}/renders/{renderKey}
}
