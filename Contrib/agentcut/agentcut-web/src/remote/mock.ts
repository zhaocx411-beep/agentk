// CutApiClient 的内存 mock 实现（阶段 A：不发真实网络请求）。
// Timeline 语义直接复用 domain/applyOps —— 这正是后端在
// TimelineEntityWorkflow 里做的事，因此 mock 的 revision/冲突行为
// 与真实后端一致，UI 层联调后切换到 REST 实现时行为不变。

import { applyOps } from "@/domain/timeline/applyOps";
import { millisecondsToFrame } from "@/domain/timeline/time";
import { OpError, newDoc, DEFAULT_FPS, docDurationInFrames } from "@/domain/timeline/types";
import type { Media, TimelineDoc } from "@/domain/timeline/types";
import type {
  AgentProfile,
  AudioSeparationStatus,
  AnswerRequest,
  ApplyOpsRequest,
  ApplyOpsResponse,
  CreateProjectRequest,
  CutApiClient,
  DeleteTranscriptRequest,
  DeleteTranscriptResponse,
  EventsPage,
  ProjectRef,
  ProjectShare,
  PrepareMediaResponse,
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

interface MockProject {
  detail: ProjectRef;
  doc: TimelineDoc;
  revision: number;
  activeTimelineId: string;
  timelines: TimelineSummary[];
  timelineDocs: Map<string, { doc: TimelineDoc; revision: number }>;
  /** requestId → 已应用结果（幂等去重，对应 Temporal UpdateID 语义）。 */
  appliedRequests: Map<string, ApplyOpsResponse>;
  events: SessionEvent[];
  /** 已启动的 Execution 计数（生成 exe_N 的 executionId）。 */
  executions: number;
  streamSubscribers: Set<StreamHandlers>;
  renders: Map<string, RenderStatus>;
  separations: Map<string, AudioSeparationStatus>;
}

function projectExecCount(p: MockProject): number {
  return Math.max(1, p.executions);
}

function notFound(what: string): ApiError {
  return new ApiError(404, { error: `${what} not found`, code: "NOT_FOUND" });
}

/** 模拟网络往返，让 UI 的 loading 态可见。 */
function delay(ms = 30): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function abortError(): Error {
  const error = new Error("The operation was aborted");
  error.name = "AbortError";
  return error;
}

export class MockCutApiClient implements CutApiClient {
  private projects = new Map<string, MockProject>();
  private seq = 0;
  private timelineSeq = 0;
  private eventSeq = 0;
  private shareSeq = 0;

  async listProfiles(): Promise<AgentProfile[]> {
    await delay();
    return [
      {
        profileId: "agentcut-default",
        name: "AgentCut",
        description: "通用视频剪辑 Agent",
        isDefault: true,
      },
    ];
  }

  /** 测试/演示用：预置一个带默认轨道的项目。 */
  seedDemoProject(title = "未命名项目", profileId = "agentcut"): ProjectRef {
    const projectId = `proj_${++this.seq}`;
    const timelineId = `tl_${this.seq}_1`;
    const doc = newDoc(title, 1920, 1080, DEFAULT_FPS);
    doc.tracks = [
      { trackId: "v1", kind: "video", name: "视频轨", createdBy: "auto" },
    ];
    const detail: ProjectRef = {
      projectId,
      title,
      createdAtUnixMs: Date.now(),
      sessionId: `sess_${projectId}`,
      profileId,
    };
    this.projects.set(projectId, {
      detail,
      doc,
      revision: 1,
      activeTimelineId: timelineId,
      timelines: [{
        timelineId,
        name: "剪辑 1",
        revision: 1,
        durationInFrames: 0,
        currentFrame: 0,
        zoom: 1,
        createdAtUnixMs: Date.now(),
        updatedAtUnixMs: Date.now(),
      }],
      timelineDocs: new Map([[timelineId, { doc: structuredClone(doc), revision: 1 }]]),
      appliedRequests: new Map(),
      events: [],
      executions: 0,
      streamSubscribers: new Set(),
      renders: new Map(),
      separations: new Map(),
    });
    return detail;
  }

  private mustGet(projectId: string): MockProject {
    const p = this.projects.get(projectId);
    if (!p) throw notFound(`project "${projectId}"`);
    return p;
  }

  private timelineSnapshot(project: MockProject): TimelineSnapshot {
    return {
      doc: structuredClone(project.doc),
      revision: project.revision,
      durationInFrames: docDurationInFrames(project.doc),
      activeTimelineId: project.activeTimelineId,
      timelines: structuredClone(project.timelines),
    };
  }

  async listProjects() {
    await delay();
    return [...this.projects.values()].map((p) => p.detail);
  }

  async createProject(req: CreateProjectRequest) {
    await delay();
    return this.seedDemoProject(req.title, req.profileId);
  }

  async renameProject(projectId: string, req: { title: string }) {
    await delay();
    const project = this.mustGet(projectId);
    project.detail = { ...project.detail, title: req.title };
    project.doc = {
      ...project.doc,
      metadata: { ...project.doc.metadata, name: req.title },
    };
    project.revision += 1;
    return project.detail;
  }

  async getProject(projectId: string) {
    await delay();
    return this.mustGet(projectId).detail;
  }

  async deleteProject(projectId: string) {
    await delay();
    this.mustGet(projectId);
    this.projects.delete(projectId);
  }

  async createProjectShare(projectId: string): Promise<ProjectShare> {
    await delay();
    this.mustGet(projectId);
    const shareId = `share_${++this.shareSeq}`;
    return {
      shareId,
      url: `https://share.agentcut.local/${shareId}`,
    };
  }

  async getTimeline(
    projectId: string,
    opts: { signal?: AbortSignal } = {},
  ): Promise<TimelineSnapshot> {
    if (opts.signal?.aborted) throw abortError();
    await delay();
    if (opts.signal?.aborted) throw abortError();
    const p = this.mustGet(projectId);
    return this.timelineSnapshot(p);
  }

  async createTimeline(
    projectId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    await delay();
    const project = this.mustGet(projectId);
    const timelineId = `tl_mock_${++this.timelineSeq}`;
    const doc = structuredClone(project.doc);
    doc.items = [];
    doc.transitions = [];
    doc.tracks = [{
      trackId: `trk_mock_${this.timelineSeq}`,
      kind: "video",
      name: "视频轨",
      createdBy: "auto",
    }];
    const now = Date.now();
    project.timelineDocs.set(timelineId, { doc: structuredClone(doc), revision: 0 });
    project.timelines.push({
      timelineId,
      name: req.name?.trim() || `剪辑 ${project.timelines.length + 1}`,
      revision: 0,
      durationInFrames: 0,
      currentFrame: 0,
      zoom: 1,
      createdAtUnixMs: now,
      updatedAtUnixMs: now,
    });
    project.activeTimelineId = timelineId;
    project.doc = doc;
    project.revision = 0;
    return this.timelineSnapshot(project);
  }

  async switchTimeline(
    projectId: string,
    timelineId: string,
    _req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    await delay();
    const project = this.mustGet(projectId);
    const target = project.timelineDocs.get(timelineId);
    if (!target) throw notFound(`timeline "${timelineId}"`);
    project.activeTimelineId = timelineId;
    project.doc = structuredClone(target.doc);
    project.revision = target.revision;
    return this.timelineSnapshot(project);
  }

  async renameTimeline(
    projectId: string,
    timelineId: string,
    req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    await delay();
    const project = this.mustGet(projectId);
    const timeline = project.timelines.find((candidate) => candidate.timelineId === timelineId);
    if (!timeline) throw notFound(`timeline "${timelineId}"`);
    const name = req.name?.trim();
    if (!name) throw new ApiError(422, { code: "BAD_REQUEST", error: "name is required" });
    timeline.name = name;
    timeline.updatedAtUnixMs = Date.now();
    return this.timelineSnapshot(project);
  }

  async deleteTimeline(
    projectId: string,
    timelineId: string,
    _req: TimelineMutationRequest,
  ): Promise<TimelineSnapshot> {
    await delay();
    const project = this.mustGet(projectId);
    if (project.timelines.length <= 1) {
      throw new ApiError(409, {
        code: "TIMELINE_UPDATE_FAILED",
        error: "the last timeline cannot be deleted",
      });
    }
    const index = project.timelines.findIndex((timeline) => timeline.timelineId === timelineId);
    if (index < 0) throw notFound(`timeline "${timelineId}"`);
    const wasActive = project.activeTimelineId === timelineId;
    project.timelines.splice(index, 1);
    project.timelineDocs.delete(timelineId);
    if (wasActive) {
      const target = project.timelines[Math.min(index, project.timelines.length - 1)]!;
      const stored = project.timelineDocs.get(target.timelineId)!;
      project.activeTimelineId = target.timelineId;
      project.doc = structuredClone(stored.doc);
      project.revision = stored.revision;
    }
    return this.timelineSnapshot(project);
  }

  async updateTimelineView(
    projectId: string,
    timelineId: string,
    req: UpdateTimelineViewRequest,
  ): Promise<TimelineSummary> {
    await delay();
    const project = this.mustGet(projectId);
    if (timelineId !== project.activeTimelineId) {
      throw new ApiError(409, {
        code: "TIMELINE_NOT_ACTIVE",
        error: `timeline "${timelineId}" is not active`,
      });
    }
    const timeline = project.timelines.find((candidate) => candidate.timelineId === timelineId);
    if (!timeline) throw notFound(`timeline "${timelineId}"`);
    timeline.currentFrame = Math.max(0, Math.round(req.currentFrame));
    timeline.zoom = req.zoom;
    timeline.updatedAtUnixMs = Date.now();
    return structuredClone(timeline);
  }

  async applyOps(
    projectId: string,
    req: ApplyOpsRequest,
    opts: { signal?: AbortSignal } = {},
  ): Promise<ApplyOpsResponse> {
    if (opts.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    await delay();
    if (opts.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    const p = this.mustGet(projectId);
    if (req.timelineId && req.timelineId !== p.activeTimelineId) {
      throw new ApiError(409, {
        code: "TIMELINE_NOT_ACTIVE",
        error: `timeline "${req.timelineId}" is not active`,
      });
    }
    const dup = p.appliedRequests.get(req.requestId);
    if (dup) return dup; // 幂等：同 requestId 重放返回首次结果
    if (req.baseRevision !== p.revision) {
      throw new ApiError(409, {
        code: "REVISION_CONFLICT",
        error: `baseRevision ${req.baseRevision} != current ${p.revision}`,
        latestRevision: p.revision,
      });
    }
    try {
      const { doc, summary } = applyOps(p.doc, req.operations);
      p.doc = doc;
      p.revision += 1;
      p.timelineDocs.set(p.activeTimelineId, {
        doc: structuredClone(doc),
        revision: p.revision,
      });
      const active = p.timelines.find((timeline) => timeline.timelineId === p.activeTimelineId);
      if (active) {
        active.revision = p.revision;
        active.durationInFrames = docDurationInFrames(doc);
        active.updatedAtUnixMs = Date.now();
      }
      const resp: ApplyOpsResponse = { revision: p.revision, summary, doc };
      p.appliedRequests.set(req.requestId, resp);
      return resp;
    } catch (err) {
      if (err instanceof OpError) {
        throw new ApiError(422, { code: err.code, error: err.message });
      }
      throw err;
    }
  }

  async restoreTimeline(
    projectId: string,
    req: RestoreTimelineRequest,
    opts: { signal?: AbortSignal } = {},
  ): Promise<RestoreTimelineResponse> {
    if (opts.signal?.aborted) throw abortError();
    await delay();
    if (opts.signal?.aborted) throw abortError();
    const project = this.mustGet(projectId);
    if (req.timelineId && req.timelineId !== project.activeTimelineId) {
      throw new ApiError(409, {
        code: "TIMELINE_NOT_ACTIVE",
        error: `timeline "${req.timelineId}" is not active`,
      });
    }
    if (req.baseRevision !== project.revision) {
      throw new ApiError(409, {
        code: "REVISION_CONFLICT",
        error: `baseRevision ${req.baseRevision} != current ${project.revision}`,
        latestRevision: project.revision,
      });
    }
    project.doc = structuredClone(req.doc);
    project.revision += 1;
    project.timelineDocs.set(project.activeTimelineId, {
      doc: structuredClone(project.doc),
      revision: project.revision,
    });
    const active = project.timelines.find(
      (timeline) => timeline.timelineId === project.activeTimelineId,
    );
    if (active) {
      active.revision = project.revision;
      active.durationInFrames = docDurationInFrames(project.doc);
      active.updatedAtUnixMs = Date.now();
    }
    return { revision: project.revision, doc: structuredClone(project.doc) };
  }

  async getTranscriptView(
    projectId: string,
    opts: { signal?: AbortSignal } = {},
  ): Promise<TranscriptView> {
    if (opts.signal?.aborted) throw abortError();
    await delay();
    if (opts.signal?.aborted) throw abortError();
    const project = this.mustGet(projectId);
    // mock 不复制后端的 occurrence 签名与权威帧映射逻辑，避免前端测试替身
    // 演变为另一套删除编译器。真实联调由 HTTP 实现提供完整视图。
    return { timelineRevision: project.revision, entries: [], warnings: [] };
  }

  async deleteTranscriptEdits(
    projectId: string,
    _req: DeleteTranscriptRequest,
    opts: { signal?: AbortSignal } = {},
  ): Promise<DeleteTranscriptResponse> {
    if (opts.signal?.aborted) throw abortError();
    this.mustGet(projectId);
    throw new ApiError(501, {
      code: "MOCK_TRANSCRIPT_EDIT_UNAVAILABLE",
      error: "mock 模式不模拟服务端 occurrenceId 与文字删除编译",
    });
  }

  /** 追加一条持久事件并推送给所有 stream 订阅者（模拟 agent-k 的 Run Stream 旁路）。 */
  private emit(p: MockProject, kind: string, data: unknown): SessionEvent {
    const ev: SessionEvent = {
      eventId: `evt_${++this.eventSeq}`,
      executionId: `exe_${projectExecCount(p)}`,
      kind,
      data,
      createdAt: new Date().toISOString(),
    };
    p.events.push(ev);
    for (const h of p.streamSubscribers) h.onEvent(ev);
    return ev;
  }

  async sendMessage(projectId: string, req: SendMessageRequest): Promise<StartExecutionResponse> {
    await delay();
    const p = this.mustGet(projectId);
    p.executions += 1;
    this.emit(p, "user.input", { text: req.text });
    // 模拟一次简单的 Agent 回复轮次（真实回复逻辑属 agent-k，不在 mock 范围）。
    setTimeout(() => {
      this.emit(p, "assistant.text", {
        block: 0,
        text: `（mock 回复）收到："${req.text}"。真实 Agent 需要接 agent-k 后端。`,
      });
      this.emit(p, "round.completed", { finishReason: "stop" });
    }, 400);
    return { executionId: `exe_${p.executions}` };
  }

  async listEvents(
    projectId: string,
    opts: { pageToken?: string; limit?: number } = {},
  ): Promise<EventsPage> {
    await delay();
    const events = this.mustGet(projectId).events;
    const limit = Math.min(opts.limit || 20, 100);
    // 新到旧分页（对齐后端桥接层）：pageToken 是"从第几条（新到旧序）开始"。
    const newestFirst = [...events].reverse();
    const start = opts.pageToken ? Number(opts.pageToken) : 0;
    const slice = newestFirst.slice(start, start + limit);
    const next = start + limit < newestFirst.length ? String(start + limit) : "";
    return { events: slice, nextPageToken: next };
  }

  streamEvents(projectId: string, handlers: StreamHandlers): () => void {
    const p = this.projects.get(projectId);
    if (!p) {
      handlers.onClose?.("error");
      return () => {};
    }
    p.streamSubscribers.add(handlers);
    return () => {
      p.streamSubscribers.delete(handlers);
    };
  }

  async answer(projectId: string, req: AnswerRequest): Promise<StartExecutionResponse> {
    await delay();
    const p = this.mustGet(projectId);
    p.executions += 1;
    this.emit(p, "ask_user.responded", { toolCallId: req.toolCallId, answers: req.answers });
    return { executionId: `exe_${p.executions}` };
  }

  async upload(projectId: string, req: UploadRequest): Promise<UploadResponse> {
    await delay();
    const p = this.mustGet(projectId);
    // 与后端一致：一次上传直接登记素材（addMedia op）、revision +1。
    // addMedia 属于服务端 op，前端 ApplyOps 未移植，这里直接改 mock 的权威文档。
    const fps = p.doc.metadata.fps;
    const durationInFrames =
      req.kind === "image"
        ? 0
        : Math.max(1, millisecondsToFrame(req.durationMs ?? 1000, fps));
    const mediaId = `media_${p.doc.media.length + 1}`;
    const doc = structuredClone(p.doc);
    const hasAudio = req.kind !== "image";
    const media: Media = {
      mediaId,
      uri: `mock://blob/${projectId}/${mediaId}`,
      kind: req.kind,
      durationInFrames,
      processing: {
        status: "processing",
        readyForTimeline: true,
        probe: req.kind === "image" ? "skipped" : "succeeded",
        thumbnail: req.kind === "audio" ? "skipped" : "pending",
        waveform: hasAudio ? "pending" : "skipped",
        transcript: hasAudio ? "pending" : "skipped",
        understanding: req.kind === "video" ? "pending" : "skipped",
      },
    };
    doc.media.push(media);
    p.doc = doc;
    p.revision += 1;
    this.completeMockPreparation(p, mediaId);
    return { media, revision: p.revision };
  }

  async prepareMedia(projectId: string, mediaId: string): Promise<PrepareMediaResponse> {
    await delay();
    const project = this.mustGet(projectId);
    const media = project.doc.media.find((candidate) => candidate.mediaId === mediaId);
    if (!media) throw notFound(`media "${mediaId}"`);
    const hasAudio = media.kind !== "image" && media.probe?.hasAudio !== false;
    media.processing = {
      status: "processing",
      readyForTimeline: media.kind === "image" || !!media.probe,
      probe: media.kind === "image" ? "skipped" : media.probe ? "succeeded" : "pending",
      thumbnail: media.kind === "audio" ? "skipped" : "pending",
      waveform: hasAudio ? "pending" : "skipped",
      transcript: hasAudio ? "pending" : "skipped",
      understanding: media.kind === "video" ? "pending" : "skipped",
    };
    project.revision += 1;
    this.completeMockPreparation(project, mediaId);
    return { workflowId: `media-prepare:${projectId}:${mediaId}`, status: "started" };
  }

  async startAudioSeparation(
    projectId: string,
    itemId: string,
    req: StartAudioSeparationRequest,
  ): Promise<{ workflowId: string; status: "running" }> {
    await delay();
    const project = this.mustGet(projectId);
    if (req.timelineId && req.timelineId !== project.activeTimelineId) {
      throw new ApiError(409, {
        code: "TIMELINE_NOT_ACTIVE",
        error: "timeline is no longer active",
      });
    }
    if (req.baseRevision !== project.revision) {
      throw new ApiError(409, {
        code: "REVISION_CONFLICT",
        error: "timeline revision changed",
        latestRevision: project.revision,
      });
    }
    const item = project.doc.items.find((candidate) => candidate.itemId === itemId);
    const media = project.doc.media.find((candidate) => candidate.mediaId === item?.mediaId);
    const track = project.doc.tracks.find((candidate) => candidate.trackId === item?.trackId);
    if (!item || item.kind !== "video" || !media || media.probe?.hasAudio !== true) {
      throw new ApiError(422, { code: "NO_AUDIO", error: "video has no audio stream" });
    }
    if (track?.locked) {
      throw new ApiError(422, { code: "LOCKED_TRACK", error: "source track is locked" });
    }
    const doc = structuredClone(project.doc);
    const audioMediaId = `med_sep_${itemId}`;
    const audioItemId = `item_sep_${itemId}`;
    let audioTrack = doc.tracks.find((candidate) => candidate.kind === "audio" && !candidate.locked);
    if (!audioTrack) {
      audioTrack = {
        trackId: `a_sep_${itemId}`,
        kind: "audio",
        name: "分离音频",
        createdBy: "auto",
      };
      doc.tracks.push(audioTrack);
    }
    if (!doc.media.some((candidate) => candidate.mediaId === audioMediaId)) {
      doc.media.push({
        mediaId: audioMediaId,
        uri: `mock://separated/${audioMediaId}`,
        kind: "audio",
        durationInFrames: media.durationInFrames,
        probe: { durationMs: media.probe?.durationMs, hasAudio: true, codec: "mp3" },
      });
    }
    if (!doc.items.some((candidate) => candidate.itemId === audioItemId)) {
      doc.items.push({
        itemId: audioItemId,
        trackId: audioTrack.trackId,
        kind: "audio",
        mediaId: audioMediaId,
        from: item.from,
        durationInFrames: item.durationInFrames,
        trimStart: item.trimStart,
        speed: item.speed,
        volume: item.volume,
        fadeInFrames: item.fadeInFrames,
        fadeOutFrames: item.fadeOutFrames,
      });
    }
    const source = doc.items.find((candidate) => candidate.itemId === itemId)!;
    source.muted = true;
    project.doc = doc;
    project.revision += 1;
    const workflowId = `separate-audio:${projectId}:${itemId}`;
    project.separations.set(itemId, {
      workflowId,
      status: "completed",
      result: {
        revision: project.revision,
        doc,
        audioMediaId,
        audioItemId,
        audioTrackId: audioTrack.trackId,
      },
    });
    return { workflowId, status: "running" };
  }

  async getAudioSeparation(
    projectId: string,
    itemId: string,
    _timelineId?: string,
  ): Promise<AudioSeparationStatus> {
    await delay();
    const status = this.mustGet(projectId).separations.get(itemId);
    if (!status) throw notFound(`audio separation "${itemId}"`);
    return structuredClone(status);
  }

  private completeMockPreparation(project: MockProject, mediaId: string): void {
    setTimeout(() => {
      const media = project.doc.media.find((candidate) => candidate.mediaId === mediaId);
      if (!media?.processing) return;
      media.processing = {
        ...media.processing,
        status: "ready",
        readyForTimeline: true,
        probe: media.processing.probe === "skipped" ? "skipped" : "succeeded",
        thumbnail: media.processing.thumbnail === "skipped" ? "skipped" : "succeeded",
        waveform: media.processing.waveform === "skipped" ? "skipped" : "succeeded",
        transcript: media.processing.transcript === "skipped" ? "skipped" : "succeeded",
        understanding:
          media.processing.understanding === "skipped" ? "skipped" : "succeeded",
      };
      project.revision += 1;
    }, 500);
  }

  async startRender(projectId: string): Promise<StartRenderResponse> {
    await delay();
    const p = this.mustGet(projectId);
    const renderKey = `render_${p.renders.size + 1}`;
    p.renders.set(renderKey, {
      renderKey,
      status: "running",
      revision: p.revision,
      progress: { done: 0, total: 3, current: "准备渲染" },
    });
    // 模拟渲染推进到完成。
    setTimeout(() => {
      p.renders.set(renderKey, {
        renderKey,
        status: "completed",
        revision: p.revision,
        // 浏览器只接触同源播放地址，不暴露 mock/blob 的内部存储 URI。
        outputUri: `/v1/projects/${encodeURIComponent(projectId)}/renders/${encodeURIComponent(renderKey)}.mp4`,
        progress: { done: 3, total: 3, current: "完成" },
        warnings: [],
      });
    }, 500);
    return { renderKey, revision: p.revision, status: "accepted" };
  }

  async getRenderStatus(projectId: string, renderKey: string): Promise<RenderStatus> {
    await delay();
    const st = this.mustGet(projectId).renders.get(renderKey);
    if (!st) throw notFound(`render "${renderKey}"`);
    return { ...st };
  }
}
