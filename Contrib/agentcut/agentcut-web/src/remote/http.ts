// CutApiClient 的真实 REST 实现（阶段 B）：直连 cut-apiserver。
//
// 契约核对来源（不是凭空猜的）：platform/api/*.go 逐路由读过一遍，
// 字段名、错误 body 形状都按后端实际实现对齐，而不是设计文档最初设想的
// 版本（尤其是上传：服务端中转单次 multipart POST，不是预签名直传两段式）。
//
// 会话相关四个接口（sendMessage/listEvents/streamEvents/answer）走
// cut-apiserver 的 agent-k 桥接层（platform/api/agentk.go）：agent-k 已
// 迁移到 Connect RPC，桥接层把 StartExecution/ListEvents/WatchEvents 翻译
// 成浏览器友好的 REST+SSE。事件信封 {eventId,executionId?,roundId?,kind,
// data,createdAt}；/events 新到旧分页 {events,nextPageToken}；SSE 命名帧
// + 无进行中 Execution 时 204。
//
// 鉴权：开发环境用可配置的 X-AgentCut-User 头，不做真正的登录；默认值由
// remote/index.ts 从 VITE_AGENTCUT_USER 读取。

import type {
  AgentProfile,
  AudioSeparationStatus,
  AnswerRequest,
  ApiErrorBody,
  ApplyOpsRequest,
  ApplyOpsResponse,
  CreateProjectRequest,
  CutApiClient,
  DeleteTranscriptRequest,
  DeleteTranscriptResponse,
  DeltaFrame,
  EventsPage,
  PrepareMediaResponse,
  ProjectRef,
  ProjectShare,
  RenameProjectRequest,
  RenderStatus,
  RestoreTimelineRequest,
  RestoreTimelineResponse,
  SendMessageRequest,
  SessionEvent,
  StartExecutionResponse,
  StartAudioSeparationRequest,
  StartRenderResponse,
  StreamHandlers,
  TimelineSnapshot,
  TimelineSummary,
  TimelineMutationRequest,
  TranscriptView,
  UploadRequest,
  UploadResponse,
  UpdateTimelineViewRequest,
} from "./client";
import { ApiError } from "./client";

const USER_HEADER = "X-AgentCut-User";
const TIMELINE_REQUEST_TIMEOUT_MS = 10_000;

export interface HttpCutApiClientOptions {
  /** 默认空字符串：配合 vite dev proxy，相对路径 /v1/... 由 vite 转发到后端。 */
  baseUrl?: string;
  /** 对齐后端开发身份的 X-AgentCut-User 头；生产身份由可信网关注入。 */
  user?: string;
}

async function parseError(resp: Response): Promise<ApiError> {
  let body: ApiErrorBody;
  try {
    body = (await resp.json()) as ApiErrorBody;
  } catch {
    body = { error: resp.statusText || `HTTP ${resp.status}` };
  }
  return new ApiError(resp.status, body);
}

export class HttpCutApiClient implements CutApiClient {
  private readonly baseUrl: string;
  private readonly user: string;

  constructor(opts: HttpCutApiClientOptions = {}) {
    this.baseUrl = opts.baseUrl ?? "";
    this.user = opts.user ?? "dev";
  }

  private async request<T>(
    path: string,
    init: RequestInit = {},
    parseJson = true,
    timeoutMs?: number,
  ): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set(USER_HEADER, this.user);
    if (init.body && typeof init.body === "string" && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    const controller = timeoutMs ? new AbortController() : null;
    let timedOut = false;
    const forwardAbort = () => controller?.abort(init.signal?.reason);
    if (controller && init.signal) {
      if (init.signal.aborted) forwardAbort();
      else init.signal.addEventListener("abort", forwardAbort, { once: true });
    }
    const timer = controller
      ? globalThis.setTimeout(() => {
          timedOut = true;
          controller.abort();
        }, timeoutMs)
      : null;
    try {
      const resp = await fetch(this.baseUrl + path, {
        ...init,
        headers,
        signal: controller?.signal ?? init.signal,
      });
      if (!resp.ok) throw await parseError(resp);
      if (!parseJson) return undefined as T;
      if (resp.status === 204) return undefined as T;
      return (await resp.json()) as T;
    } catch (error) {
      if (timedOut) {
        throw new Error("请求超时，服务暂时不可用，请稍后重试");
      }
      throw error;
    } finally {
      if (timer !== null) globalThis.clearTimeout(timer);
      init.signal?.removeEventListener("abort", forwardAbort);
    }
  }

  // ---- 项目 ----

  async listProfiles(): Promise<AgentProfile[]> {
    const { profiles } = await this.request<{ profiles: AgentProfile[] }>("/v1/profiles");
    return profiles ?? [];
  }

  async listProjects(): Promise<ProjectRef[]> {
    const { projects } = await this.request<{ projects: ProjectRef[] }>("/v1/projects");
    return projects;
  }

  async createProject(req: CreateProjectRequest): Promise<ProjectRef> {
    return this.request<ProjectRef>("/v1/projects", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  async renameProject(projectId: string, req: RenameProjectRequest): Promise<ProjectRef> {
    return this.request<ProjectRef>(`/v1/projects/${encodeURIComponent(projectId)}`, {
      method: "PATCH",
      body: JSON.stringify(req),
    });
  }

  async getProject(projectId: string): Promise<ProjectRef> {
    // 没有独立的 GET /v1/projects/{id} 路由（后端 requireProject 本身也是
    // 拉列表再按 id 过滤），这里做一样的事。
    const projects = await this.listProjects();
    const found = projects.find((p) => p.projectId === projectId);
    if (!found) throw new ApiError(404, { error: `project "${projectId}" not found`, code: "NOT_FOUND" });
    return found;
  }

  async deleteProject(projectId: string): Promise<void> {
    await this.request<void>(`/v1/projects/${encodeURIComponent(projectId)}`, { method: "DELETE" }, false);
  }

  async createProjectShare(projectId: string): Promise<ProjectShare> {
    return this.request<ProjectShare>(
      `/v1/projects/${encodeURIComponent(projectId)}/shares`,
      { method: "POST", body: JSON.stringify({ access: "read-only" }) },
    );
  }

  // ---- Timeline ----

  async getTimeline(
    projectId: string,
    opts: { signal?: AbortSignal } = {},
  ): Promise<TimelineSnapshot> {
    const snap = await this.request<TimelineSnapshot>(
      `/v1/projects/${encodeURIComponent(projectId)}/timeline`,
      { signal: opts.signal },
      true,
      TIMELINE_REQUEST_TIMEOUT_MS,
    );
    // Go 的 nil slice 序列化成 null，前端类型/applyOps 都按数组处理，
    // 在边界归一化，避免 null.filter/.find 之类的运行时炸点。
    snap.doc.media ??= [];
    snap.doc.tracks ??= [];
    snap.doc.items ??= [];
    if (!snap.activeTimelineId || !snap.timelines?.length) {
      const timelineId = `tl_legacy_${projectId}`;
      snap.activeTimelineId = timelineId;
      snap.timelines = [{
        timelineId,
        name: "剪辑 1",
        revision: snap.revision,
        durationInFrames: snap.durationInFrames,
        currentFrame: 0,
        zoom: 1,
        createdAtUnixMs: 0,
        updatedAtUnixMs: 0,
      }];
    }
    return snap;
  }

  async createTimeline(
    projectId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    return this.request<TimelineSnapshot>(
      `/v1/projects/${encodeURIComponent(projectId)}/timelines`,
      { method: "POST", body: JSON.stringify(req) },
      true,
      TIMELINE_REQUEST_TIMEOUT_MS,
    );
  }

  async switchTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    return this.request<TimelineSnapshot>(
      `/v1/projects/${encodeURIComponent(projectId)}/timelines/${encodeURIComponent(timelineId)}/activate`,
      { method: "POST", body: JSON.stringify(req) },
      true,
      TIMELINE_REQUEST_TIMEOUT_MS,
    );
  }

  async renameTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    return this.request<TimelineSnapshot>(
      `/v1/projects/${encodeURIComponent(projectId)}/timelines/${encodeURIComponent(timelineId)}`,
      { method: "PATCH", body: JSON.stringify(req) },
      true,
      TIMELINE_REQUEST_TIMEOUT_MS,
    );
  }

  async deleteTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    return this.request<TimelineSnapshot>(
      `/v1/projects/${encodeURIComponent(projectId)}/timelines/${encodeURIComponent(timelineId)}`,
      { method: "DELETE", body: JSON.stringify(req) },
      true,
      TIMELINE_REQUEST_TIMEOUT_MS,
    );
  }

  async updateTimelineView(
    projectId: string,
    timelineId: string,
    req: UpdateTimelineViewRequest,
  ): Promise<TimelineSummary> {
    return this.request<TimelineSummary>(
      `/v1/projects/${encodeURIComponent(projectId)}/timelines/${encodeURIComponent(timelineId)}/view`,
      { method: "PATCH", body: JSON.stringify(req) },
      true,
      TIMELINE_REQUEST_TIMEOUT_MS,
    );
  }

  async applyOps(
    projectId: string,
    req: ApplyOpsRequest,
    opts: { signal?: AbortSignal } = {},
  ): Promise<ApplyOpsResponse> {
    return this.request<ApplyOpsResponse>(`/v1/projects/${encodeURIComponent(projectId)}/timeline/ops`, {
      method: "POST",
      body: JSON.stringify(req),
      signal: opts.signal,
    });
  }

  async restoreTimeline(
    projectId: string,
    req: RestoreTimelineRequest,
    opts: { signal?: AbortSignal } = {},
  ): Promise<RestoreTimelineResponse> {
    return this.request<RestoreTimelineResponse>(
      `/v1/projects/${encodeURIComponent(projectId)}/timeline:restore`,
      {
        method: "POST",
        body: JSON.stringify(req),
        signal: opts.signal,
      },
    );
  }

  async getTranscriptView(
    projectId: string,
    opts: { signal?: AbortSignal } = {},
  ): Promise<TranscriptView> {
    const view = await this.request<TranscriptView>(
      `/v1/projects/${encodeURIComponent(projectId)}/transcript-view`,
      { signal: opts.signal },
    );
    view.entries ??= [];
    view.warnings ??= [];
    return view;
  }

  async deleteTranscriptEdits(
    projectId: string,
    req: DeleteTranscriptRequest,
    opts: { signal?: AbortSignal } = {},
  ): Promise<DeleteTranscriptResponse> {
    return this.request<DeleteTranscriptResponse>(
      `/v1/projects/${encodeURIComponent(projectId)}/transcript-edits:delete`,
      {
        method: "POST",
        body: JSON.stringify(req),
        signal: opts.signal,
      },
    );
  }

  // ---- 会话（agent-k 桥接） ----
  // 契约来源：platform/api/agentk.go（cut-apiserver 把 agent-k 的 Connect
  // RPC 翻译成 REST+SSE），前后端形状在同一个仓库里对齐。

  async sendMessage(projectId: string, req: SendMessageRequest): Promise<StartExecutionResponse> {
    // 202 Accepted + {executionId}，不等待模型和 Tool 执行完成。
    return this.request<StartExecutionResponse>(
      `/v1/projects/${encodeURIComponent(projectId)}/messages`,
      { method: "POST", body: JSON.stringify(req) },
    );
  }

  async listEvents(
    projectId: string,
    opts: { pageToken?: string; limit?: number } = {},
  ): Promise<EventsPage> {
    const params = new URLSearchParams();
    if (opts.pageToken) params.set("pageToken", opts.pageToken);
    if (opts.limit !== undefined) params.set("limit", String(opts.limit));
    const qs = params.size > 0 ? `?${params}` : "";
    const page = await this.request<EventsPage>(
      `/v1/projects/${encodeURIComponent(projectId)}/events${qs}`,
    );
    page.events ??= [];
    page.nextPageToken ??= "";
    return page;
  }

  streamEvents(
    projectId: string,
    handlers: StreamHandlers,
    opts: { executionId?: string } = {},
  ): () => void {
    // 不用 EventSource：1) 帧带 `event: <kind>` 名字，onmessage 收不到，
    // 而 kind 集合是开放的，无法穷举 addEventListener；2) 没有 active run
    // 时返回 204，EventSource 会当作错误无限重连；3) 需要带 X-AgentCut-User
    // 自定义头。改用 fetch + ReadableStream 手工解析 SSE。
    const controller = new AbortController();
    const qs = opts.executionId ? `?executionId=${encodeURIComponent(opts.executionId)}` : "";
    const url = `${this.baseUrl}/v1/projects/${encodeURIComponent(projectId)}/stream${qs}`;

    (async () => {
      const resp = await fetch(url, {
        headers: { [USER_HEADER]: this.user, Accept: "text/event-stream" },
        signal: controller.signal,
      });
      if (resp.status === 204) {
        // 只表示连接时没有 active run，不代表 Session 完成。
        // 204 重试与 end 后的 listEvents 校准由调用方（AgentPanel）负责。
        handlers.onClose?.("no_active_run");
        return;
      }
      if (!resp.ok || !resp.body) {
        throw await parseError(resp);
      }

      const decoder = new TextDecoder();
      const reader = resp.body.getReader();
      let buf = "";
      let ended = false;

      const handleFrame = (frame: string) => {
        // SSE 帧：event:/id:/data: 行，data 可能多行（拼接后为 JSON）。
        let eventName = "message";
        const dataLines: string[] = [];
        for (const line of frame.split("\n")) {
          if (line.startsWith(":")) continue; // 心跳注释
          if (line.startsWith("event:")) eventName = line.slice(6).trim();
          else if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
        }
        if (dataLines.length === 0 && eventName !== "stream.end") return;
        const raw = dataLines.join("\n");
        if (eventName === "stream.end") {
          ended = true;
          return;
        }
        try {
          if (eventName === "delta") {
            handlers.onDelta?.(JSON.parse(raw) as DeltaFrame);
          } else {
            // 其余命名事件都是已持久化的事件信封，data 与 /events 相同。
            handlers.onEvent(JSON.parse(raw) as SessionEvent);
          }
        } catch {
          // 单帧解析失败不中断整个流。
          console.warn("streamEvents: unparsable frame", eventName, raw);
        }
      };

      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        // 帧以空行分隔；同时兼容 \r\n。
        let sep: number;
        while ((sep = buf.search(/\n\n|\r\n\r\n/)) >= 0) {
          const frame = buf.slice(0, sep).replaceAll("\r\n", "\n");
          buf = buf.slice(sep).replace(/^(\r?\n){2}/, "");
          handleFrame(frame);
          if (ended) break;
        }
        if (ended) break;
      }
      handlers.onClose?.("end");
    })().catch((err: unknown) => {
      if (controller.signal.aborted) return; // 主动取消不算错误
      console.error("streamEvents failed", err);
      handlers.onClose?.("error");
    });

    return () => controller.abort();
  }

  async answer(projectId: string, req: AnswerRequest): Promise<StartExecutionResponse> {
    return this.request<StartExecutionResponse>(
      `/v1/projects/${encodeURIComponent(projectId)}/answers`,
      { method: "POST", body: JSON.stringify(req) },
    );
  }

  // ---- 上传 ----

  async upload(projectId: string, req: UploadRequest): Promise<UploadResponse> {
    const form = new FormData();
    form.set("file", req.file);
    form.set("kind", req.kind);
    if (req.durationMs !== undefined) form.set("durationMs", String(Math.round(req.durationMs)));

    const headers = new Headers();
    headers.set(USER_HEADER, this.user);
    const resp = await fetch(`${this.baseUrl}/v1/projects/${encodeURIComponent(projectId)}/uploads`, {
      method: "POST",
      headers,
      body: form,
    });
    if (!resp.ok) throw await parseError(resp);
    return (await resp.json()) as UploadResponse;
  }

  async prepareMedia(projectId: string, mediaId: string): Promise<PrepareMediaResponse> {
    return this.request<PrepareMediaResponse>(
      `/v1/projects/${encodeURIComponent(projectId)}/media/${encodeURIComponent(mediaId)}/prepare`,
      { method: "POST" },
    );
  }

  async startAudioSeparation(
    projectId: string,
    itemId: string,
    req: StartAudioSeparationRequest,
  ): Promise<{ workflowId: string; status: "running" }> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/items/${encodeURIComponent(itemId)}/audio-separation`,
      { method: "POST", body: JSON.stringify(req) },
    );
  }

  async getAudioSeparation(
    projectId: string,
    itemId: string,
    timelineId?: string,
  ): Promise<AudioSeparationStatus> {
    const query = timelineId ? `?timelineId=${encodeURIComponent(timelineId)}` : "";
    return this.request<AudioSeparationStatus>(
      `/v1/projects/${encodeURIComponent(projectId)}/items/${encodeURIComponent(itemId)}/audio-separation${query}`,
    );
  }

  // ---- 渲染 / 导出 ----

  async startRender(projectId: string): Promise<StartRenderResponse> {
    return this.request<StartRenderResponse>(`/v1/projects/${encodeURIComponent(projectId)}/renders`, {
      method: "POST",
    });
  }

  async getRenderStatus(projectId: string, renderKey: string): Promise<RenderStatus> {
    return this.request<RenderStatus>(
      `/v1/projects/${encodeURIComponent(projectId)}/renders/${encodeURIComponent(renderKey)}`,
    );
  }
}
