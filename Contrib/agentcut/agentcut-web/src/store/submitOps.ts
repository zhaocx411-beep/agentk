// 时间线串行提交队列：本地乐观应用后排队，一次只允许一个请求 in-flight。
// 每个批次在真正发送前读取最新 confirmedRevision，网络/429/5xx 采用有限
// 指数退避；409 时绝不自动重放，丢弃依赖旧版本的整条队列、全量同步并提示重试。

import { docDurationInFrames, type Operation } from "@/domain/timeline/types";
import { withAutoTrackCleanup } from "@/domain/timeline/autoTracks";
import { ApiError, api } from "@/remote";
import type { TranscriptDeleteRange } from "@/remote";

import { historyLabel, invertOperations } from "./historyOps";
import { markAgentTurnConcurrentMutation } from "./agentSync";
import type { HistoryEntry } from "./historyStore";
import { snapshotHistoryEntry, useHistoryStore } from "./historyStore";
import { useSequenceStore } from "./sequenceStore";
import { useTimelineStore } from "./timelineStore";
import { toast } from "./toastStore";
import {
  invalidateTranscriptView,
  refreshTranscriptView,
  useTranscriptStore,
} from "./transcriptStore";

interface QueueEntryBase {
  projectId: string;
  timelineId?: string;
  requestId: string;
  clientMutationId: string;
  history?: HistoryEntry;
  resolve: (message: string | null) => void;
}

interface OpsQueueEntry extends QueueEntryBase {
  kind: "ops";
  operations: Operation[];
}

interface TranscriptDeleteQueueEntry extends QueueEntryBase {
  kind: "transcriptDelete";
  ranges: TranscriptDeleteRange[];
}

type QueueEntry = OpsQueueEntry | TranscriptDeleteQueueEntry;

const queue: QueueEntry[] = [];
let generation = 0;
let activeRunnerGeneration: number | null = null;
let activeRequestController: AbortController | null = null;
let activeRecoveryController: AbortController | null = null;
let activeProjectId: string | null | undefined;
let activeTimelineId: string | null | undefined;
const RETRY_DELAYS_MS = [300, 1000, 3000];

/**
 * 提交一批 Op。成功返回 null；失败返回给用户看的错误消息
 * （本地校验失败时批次不会入队，服务端失败时批次留在 pending 里标记 failed）。
 */
export async function submitOps(
  projectId: string,
  operations: Operation[],
  options: { recordHistory?: boolean } = {},
): Promise<string | null> {
  if (activeProjectId !== undefined && activeProjectId !== projectId) {
    return activeProjectId === null
      ? "项目切换中，本次操作未提交"
      : "项目已切换，本次操作未提交";
  }
  if (activeRecoveryController) return "正在重新加载时间线，请稍后再编辑";
  const timelineId = useSequenceStore.getState().activeTimelineId ?? undefined;
  if (activeTimelineId !== undefined && activeTimelineId !== timelineId) {
    return "时间线已切换，本次操作未提交";
  }
  const store = useTimelineStore.getState();
  if (store.pendingOps.some((batch) => batch.status === "failed")) {
    return "存在尚未恢复的保存失败修改，请先放弃本地修改并重新加载";
  }
  markAgentTurnConcurrentMutation(projectId);
  const scopedOperations = store.localDoc
    ? withAutoTrackCleanup(store.localDoc, operations)
    : operations;
  let history: HistoryEntry | undefined;
  if (options.recordHistory !== false && store.localDoc) {
    try {
      history = {
        id: crypto.randomUUID(),
        label: historyLabel(scopedOperations),
        forward: structuredClone(scopedOperations),
        inverse: invertOperations(store.localDoc, scopedOperations),
      };
    } catch {
      // 不可安全逆转的系统 op 仍允许提交，但不进入用户历史。
    }
  }
  const clientMutationId = crypto.randomUUID();
  const batch = {
    requestId: clientMutationId,
    clientMutationId,
    baseRevision: store.confirmedRevision,
    operations: scopedOperations,
    status: "queued" as const,
  };

  const localErr = store.enqueueBatch(batch);
  if (localErr) return localErr.detail;

  return new Promise<string | null>((resolve) => {
    queue.push({
      kind: "ops",
      projectId,
      timelineId,
      requestId: batch.requestId,
      clientMutationId,
      operations: batch.operations,
      history,
      resolve,
    });
    void processQueue();
  });
}

/** 高层文字删除与普通 Timeline op 共用同一个串行队列，且不做客户端帧推演。 */
export async function submitTranscriptDelete(
  projectId: string,
  ranges: TranscriptDeleteRange[],
): Promise<string | null> {
  if (activeProjectId !== undefined && activeProjectId !== projectId) {
    return activeProjectId === null
      ? "项目切换中，本次操作未提交"
      : "项目已切换，本次操作未提交";
  }
  if (activeRecoveryController) return "正在重新加载时间线，请稍后再编辑";
  const timelineId = useSequenceStore.getState().activeTimelineId ?? undefined;
  if (activeTimelineId !== undefined && activeTimelineId !== timelineId) {
    return "时间线已切换，本次操作未提交";
  }
  const store = useTimelineStore.getState();
  if (store.pendingOps.some((batch) => batch.status === "failed")) {
    return "存在尚未恢复的保存失败修改，请先放弃本地修改并重新加载";
  }
  if (ranges.length === 0) return "请先选择要删除的文字";
  markAgentTurnConcurrentMutation(projectId);
  const clientMutationId = crypto.randomUUID();
  const batch = {
    requestId: clientMutationId,
    clientMutationId,
    baseRevision: store.confirmedRevision,
    operations: [],
    optimistic: false,
    status: "queued" as const,
  };
  const localError = store.enqueueBatch(batch);
  if (localError) return localError.detail;
  return new Promise<string | null>((resolve) => {
    queue.push({
      kind: "transcriptDelete",
      projectId,
      timelineId,
      requestId: clientMutationId,
      clientMutationId,
      ranges: structuredClone(ranges),
      resolve,
    });
    void processQueue();
  });
}

/** 绑定当前可写项目；null 表示切换窗口中，undefined 仅用于无项目的测试启动阶段。 */
export function setSubmitQueueProject(projectId: string | null, timelineId?: string | null): void {
  activeProjectId = projectId;
  activeTimelineId = timelineId;
}

/** 项目切换时取消尚未发送的工作；进行中的请求用 generation 忽略结果。 */
export function resetSubmitQueue(message = "项目已切换，本地待提交修改已取消"): void {
  generation += 1;
  activeRequestController?.abort();
  activeRecoveryController?.abort();
  activeRecoveryController = null;
  const abandoned = queue.splice(0);
  for (const entry of abandoned) entry.resolve(message);
  // 不提前释放 active runner：先等待 AbortSignal 让旧请求真正收尾，新
  // generation 再接管队列，保证任何时刻仍只有一个 applyOps 请求在发送。
}

/**
 * 放弃失败批次及所有依赖它的乐观修改，清空历史并重新读取权威 Timeline。
 * 离线/冲突但没有残留批次时也可调用，作为无需刷新浏览器的恢复入口。
 */
export async function discardFailedChangesAndReload(projectId: string): Promise<string | null> {
  if (activeProjectId !== projectId) return "项目已切换，未重新加载旧项目";
  generation += 1;
  const recoveryGeneration = generation;
  activeRequestController?.abort();
  activeRecoveryController?.abort();
  const controller = new AbortController();
  activeRecoveryController = controller;
  const abandoned = queue.splice(0);
  for (const entry of abandoned) entry.resolve("本地失败修改已放弃");

  useTimelineStore.getState().dropPendingOnConflict();
  useTimelineStore.getState().setSyncStatus("pending");
  useHistoryStore.getState().clear();

  try {
    const fresh = await api.getTimeline(projectId, { signal: controller.signal });
    if (
      recoveryGeneration !== generation ||
      controller.signal.aborted ||
      activeProjectId !== projectId
    ) {
      return "重新加载已取消";
    }
    if (fresh.revision < useTimelineStore.getState().confirmedRevision) {
      throw new Error("服务端返回了过期的时间线版本");
    }
    useTimelineStore.getState().setConfirmed(fresh.doc, fresh.revision);
    invalidateTranscriptView(projectId);
    void refreshTranscriptView(projectId, fresh.revision, { force: true });
    toast.success("已放弃本地失败修改并重新加载时间线");
    return null;
  } catch (error) {
    if (
      recoveryGeneration !== generation ||
      controller.signal.aborted ||
      isAbortError(error)
    ) {
      return "重新加载已取消";
    }
    const message =
      error instanceof ApiError
        ? error.body.error ?? error.message
        : error instanceof Error
          ? error.message
          : "重新加载失败";
    useTimelineStore.getState().setSyncStatus("offline");
    toast.error(`重新加载时间线失败：${message}`);
    return `重新加载时间线失败：${message}`;
  } finally {
    if (activeRecoveryController === controller) activeRecoveryController = null;
  }
}

async function processQueue(): Promise<void> {
  if (activeRunnerGeneration !== null) return;
  const runGeneration = generation;
  activeRunnerGeneration = runGeneration;
  try {
    while (queue.length > 0 && runGeneration === generation) {
      const entry = queue[0];
      const controller = new AbortController();
      activeRequestController = controller;
      const result = await sendWithRetry(entry, runGeneration, controller.signal);
      if (activeRequestController === controller) activeRequestController = null;
      // sendWithRetry 的冲突分支会清空 queue；只在仍是队首时 shift。
      if (queue[0] === entry) queue.shift();
      entry.resolve(result);
    }
  } finally {
    if (activeRunnerGeneration === runGeneration) activeRunnerGeneration = null;
    if (queue.length > 0) void processQueue();
  }
}

async function sendWithRetry(
  entry: QueueEntry,
  runGeneration: number,
  signal: AbortSignal,
): Promise<string | null> {
  let transientAttempt = 0;
  for (;;) {
    if (runGeneration !== generation || signal.aborted) return "提交已取消";
    const baseRevision = useTimelineStore.getState().confirmedRevision;
    if (entry.kind === "transcriptDelete" && !entry.history) {
      const beforeDoc = useTimelineStore.getState().doc;
      if (beforeDoc) {
        entry.history = {
          id: crypto.randomUUID(),
          kind: "snapshot",
          label: "删除文字稿片段",
          beforeDoc: structuredClone(beforeDoc),
        };
      }
    }
    useTimelineStore
      .getState()
      .markBatchSending(entry.requestId, baseRevision, transientAttempt + 1);
    try {
      const res =
        entry.kind === "ops"
          ? await api.applyOps(
              entry.projectId,
              {
                requestId: entry.requestId,
                clientMutationId: entry.clientMutationId,
                timelineId: entry.timelineId,
                baseRevision,
                operations: entry.operations,
              },
              { signal },
            )
          : await api.deleteTranscriptEdits(
              entry.projectId,
              {
                clientMutationId: entry.clientMutationId,
                timelineId: entry.timelineId,
                baseRevision,
                ranges: entry.ranges,
                mode: "ripple",
              },
              { signal },
            );
      if (runGeneration !== generation || signal.aborted) return "提交已取消";
      const authoritative =
        entry.kind === "transcriptDelete" || res.doc
          ? { doc: res.doc!, revision: res.revision }
          : await api.getTimeline(entry.projectId, { signal });
      if (runGeneration !== generation || signal.aborted) return "提交已取消";
      if (authoritative.revision < res.revision) {
        throw new Error(
          `authoritative timeline revision ${authoritative.revision} is behind mutation revision ${res.revision}`,
        );
      }
      const historyIsSafe = authoritative.revision === res.revision;
      if (
        historyIsSafe &&
        entry.kind === "transcriptDelete" &&
        entry.history?.kind === "snapshot"
      ) {
        entry.history = snapshotHistoryEntry(
          entry.history.label,
          entry.history.beforeDoc!,
          authoritative.doc,
        );
      }
      if (!historyIsSafe) {
        useHistoryStore.getState().clear();
        for (const queued of queue) queued.history = undefined;
      }
      const replayError = useTimelineStore
        .getState()
        .confirmBatch(entry.requestId, authoritative.revision, authoritative.doc);
      useSequenceStore
        .getState()
        .updateActive(authoritative.revision, docDurationInFrames(authoritative.doc));
      if (replayError) {
        const message = `后续本地修改无法在最新时间线上重放（${replayError.message}），已放弃并重新加载权威版本`;
        useHistoryStore.getState().clear();
        const abandoned = queue.splice(0);
        for (const queued of abandoned) {
          queued.history = undefined;
          if (queued !== entry) queued.resolve(message);
        }
        toast.error(message);
        return message;
      }
      if (historyIsSafe && entry.history) useHistoryStore.getState().push(entry.history);
      if (entry.kind === "transcriptDelete") {
        useTranscriptStore.getState().clearSelection();
      }
      invalidateTranscriptView(entry.projectId);
      void refreshTranscriptView(entry.projectId, authoritative.revision, { force: true });
      return null;
    } catch (err) {
      if (runGeneration !== generation || signal.aborted || isAbortError(err)) {
        return "提交已取消";
      }
      if (isRevisionConflict(err)) {
        return recoverFromConflict(entry, runGeneration, signal);
      }
      if (isTransient(err) && transientAttempt < RETRY_DELAYS_MS.length) {
        const delay = RETRY_DELAYS_MS[transientAttempt];
        transientAttempt += 1;
        toast.warning(`保存失败，${Math.round(delay / 100) / 10} 秒后自动重试`);
        try {
          await sleep(delay, signal);
        } catch {
          return "提交已取消";
        }
        continue;
      }
      const message =
        err instanceof ApiError
          ? err.body.error ?? err.message
          : err instanceof Error
            ? err.message
            : "提交失败，请重试";
      if (!isDomainRejection(err)) {
        const preserved =
          entry.kind === "transcriptDelete"
            ? `文字删除请求失败：${message}；选择与恢复入口仍保留`
            : `时间线保存失败：${message}；本地编辑仍保留`;
        return preservePendingAndStop(entry, preserved);
      }
      useTimelineStore.getState().markBatchFailed(entry.requestId);
      toast.error(
        entry.kind === "transcriptDelete"
          ? `文字删除未应用：${message}`
          : `时间线保存失败：${message}`,
      );
      // 当前批次最终失败后，后续批次的乐观视图建立在它之上，不能越过它
      // 继续发送；统一丢弃并拉回服务端确认状态。
      useTimelineStore.getState().dropPendingOnConflict();
      useTimelineStore.getState().setSyncStatus("offline");
      useHistoryStore.getState().clear();
      const abandoned = queue.splice(0);
      for (const queued of abandoned) {
        if (queued !== entry) queued.resolve(`前序保存失败，未提交：${message}`);
      }
      try {
        const fresh = await api.getTimeline(entry.projectId, { signal });
        if (runGeneration === generation && !signal.aborted) {
          useTimelineStore.getState().setConfirmed(fresh.doc, fresh.revision);
        }
      } catch {
        // 保持 offline，下一次项目刷新会重新拉取。
      }
      return message;
    }
  }
}

async function recoverFromConflict(
  current: QueueEntry,
  runGeneration: number,
  signal: AbortSignal,
): Promise<string | null> {
  const message = "检测到时间线版本冲突，本次操作及其后续操作均未应用，请重试";
  useTimelineStore.getState().dropPendingOnConflict();
  useHistoryStore.getState().clear();
  const abandoned = queue.splice(0);
  for (const entry of abandoned) {
    if (entry !== current) entry.resolve(message);
  }
  try {
    const fresh = await api.getTimeline(current.projectId, { signal });
    if (runGeneration === generation && !signal.aborted) {
      useTimelineStore.getState().setConfirmed(fresh.doc, fresh.revision);
      invalidateTranscriptView(current.projectId);
      void refreshTranscriptView(current.projectId, fresh.revision, { force: true });
    }
  } catch (error) {
    if (runGeneration !== generation || signal.aborted || isAbortError(error)) {
      return "提交已取消";
    }
    useTimelineStore.getState().setSyncStatus("offline");
    toast.error(`${message}；获取服务端最新版本失败，请刷新页面`);
    return `${message}；获取服务端最新版本失败，请刷新页面`;
  }
  if (runGeneration !== generation || signal.aborted) return "提交已取消";
  toast.error(message);
  return message;
}

function preservePendingAndStop(current: QueueEntry, message: string): string {
  useTimelineStore.getState().markBatchFailed(current.requestId);
  useTimelineStore.getState().setSyncStatus("offline");
  const abandoned = queue.splice(0);
  for (const entry of abandoned) {
    if (entry !== current) entry.resolve(message);
  }
  toast.error(message);
  return message;
}

function isRevisionConflict(err: unknown): boolean {
  return (
    err instanceof ApiError &&
    err.status === 409 &&
    err.body.code === "REVISION_CONFLICT"
  );
}

function isTransient(err: unknown): boolean {
  return !(err instanceof ApiError) || err.status === 429 || err.status >= 500;
}

function isDomainRejection(err: unknown): boolean {
  return err instanceof ApiError && err.status === 422;
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}
