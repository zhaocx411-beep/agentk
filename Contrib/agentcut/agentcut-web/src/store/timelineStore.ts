// Timeline 本地状态（TimelineClientState / PendingOpBatch）。权威 doc 与
// pendingOps 派生出的 localDoc 严格分离；串行发送、冲突恢复和 Agent 校准
// 分别由 submitOps / agentSync 编排。

import { create } from "zustand";

import { applyOps } from "@/domain/timeline/applyOps";
import { OpError } from "@/domain/timeline/types";
import type { Operation, TimelineDoc } from "@/domain/timeline/types";
import { usePreviewOverridesStore } from "./previewOverridesStore";

export type SyncStatus = "synced" | "pending" | "conflict" | "offline";

export interface PendingOpBatch {
  /** 网络重试幂等键；后端会派生项目隔离的 Temporal UpdateID。 */
  requestId: string;
  /** 每次用户操作唯一的前端 mutation 标识。 */
  clientMutationId?: string;
  /** 提交时认为的服务端版本。 */
  baseRevision: number;
  operations: Operation[];
  /** false 表示高层服务端 mutation；不在前端推演，只占用同一串行队列。 */
  optimistic?: boolean;
  status: "queued" | "sending" | "failed";
  attempts?: number;
}

export type SnapshotApplyResult = "applied" | "stale" | "deferred";

export interface RebasePendingResult {
  applied: boolean;
  stale: boolean;
  error: OpError | null;
}

export interface TimelineClientState {
  /** 最近一次确认的服务端文档（confirmed）。 */
  doc: TimelineDoc | null;
  /** 对应 doc 的 revision。 */
  confirmedRevision: number;
  /** 已提交但还未确认的 op 批次，按提交顺序排列。 */
  pendingOps: PendingOpBatch[];
  /** confirmedDoc 叠加 pendingOps 后的乐观视图，UI 实际渲染这个。 */
  localDoc: TimelineDoc | null;
  syncStatus: SyncStatus;
  /** 用户手势（拖拽/裁剪）进行中：Agent 改动的全量刷新会延迟到手势结束（设计 4.4）。 */
  gestureActive: boolean;
  /** Agent 刚改过的 item（ChangeSummary），Timeline 短暂高亮提示。 */
  agentHighlightItemIds: string[];

  // ---- actions ----

  /** 全量设置服务端确认状态（初次拉取 / 冲突后刷新 / Agent 改动后拉取）。 */
  setConfirmed(doc: TimelineDoc, revision: number): void;
  /** 后台轮询快照：禁止 revision 回退，也不打断待提交操作或用户手势。 */
  applyPollingSnapshot(doc: TimelineDoc, revision: number): SnapshotApplyResult;
  /** 409 后在新服务端基线上严格重放全部 pending；任一领域错误时原状态不变。 */
  rebasePending(doc: TimelineDoc, revision: number): RebasePendingResult;
  /** 追加一个乐观批次并重算 localDoc。批次非法（ApplyOps 抛错）则拒绝并返回错误。 */
  enqueueBatch(batch: PendingOpBatch): OpError | null;
  /**
   * 服务端确认某批次：从 pendingOps 移除并推进 confirmedRevision。
   * 服务端提供 doc 时必须以它为权威基线，再重放后续排队操作。
   */
  confirmBatch(
    requestId: string,
    newRevision: number,
    authoritativeDoc?: TimelineDoc,
  ): OpError | null;
  /** 更新排队批次的发送状态/实际 baseRevision。 */
  markBatchSending(requestId: string, baseRevision: number, attempts: number): void;
  /** 标记批次发送失败。 */
  markBatchFailed(requestId: string): void;
  /** 冲突处理（4.3 简化策略）：丢弃全部 pending，等待全量刷新。 */
  dropPendingOnConflict(): void;
  setSyncStatus(status: SyncStatus): void;
  setGestureActive(active: boolean): void;
  setAgentHighlight(itemIds: string[]): void;
  resetTimeline(): void;
}

/**
 * localDoc 永远由 doc + pendingOps 派生计算（同一套 ApplyOps 纯函数），
 * 不直接对 localDoc 做增量 mutation，避免本地状态和服务端语义漂移。
 */
function deriveLocalDoc(
  doc: TimelineDoc | null,
  pendingOps: PendingOpBatch[],
): TimelineDoc | null {
  if (!doc) return null;
  let cur = doc;
  for (const batch of pendingOps) {
    if (batch.optimistic === false) continue;
    cur = applyOps(cur, batch.operations).doc;
  }
  return cur;
}

export const useTimelineStore = create<TimelineClientState>((set, get) => ({
  doc: null,
  confirmedRevision: 0,
  pendingOps: [],
  localDoc: null,
  syncStatus: "synced",
  gestureActive: false,
  agentHighlightItemIds: [],

  setConfirmed(doc, revision) {
    let applied = false;
    set((s) => {
      if (revision < s.confirmedRevision) return {};
      applied = true;
      try {
        return {
          doc,
          confirmedRevision: revision,
          localDoc: deriveLocalDoc(doc, s.pendingOps),
          syncStatus: s.pendingOps.length > 0 ? ("pending" as const) : ("synced" as const),
        };
      } catch (error) {
        if (!(error instanceof OpError)) throw error;
        // 权威基线变化导致乐观批次无法重放时，不再静默跳过。明确放弃整条
        // 依赖链并进入 conflict，由顶部栏提供重新加载入口。
        return {
          doc,
          confirmedRevision: revision,
          pendingOps: [],
          localDoc: doc,
          syncStatus: "conflict" as const,
        };
      }
    });
    if (applied) usePreviewOverridesStore.getState().reset();
  },

  applyPollingSnapshot(doc, revision) {
    const state = get();
    if (revision < state.confirmedRevision) return "stale";
    if (state.pendingOps.length > 0 || state.gestureActive) return "deferred";
    set({
      doc,
      confirmedRevision: revision,
      localDoc: doc,
      syncStatus: "synced",
    });
    usePreviewOverridesStore.getState().reset();
    return "applied";
  },

  rebasePending(doc, revision) {
    const state = get();
    if (revision < state.confirmedRevision) {
      return { applied: false, stale: true, error: null };
    }
    let localDoc = doc;
    try {
      for (const batch of state.pendingOps) {
        localDoc = applyOps(localDoc, batch.operations).doc;
      }
    } catch (error) {
      if (error instanceof OpError) {
        return { applied: false, stale: false, error };
      }
      throw error;
    }
    set({
      doc,
      confirmedRevision: revision,
      localDoc,
      syncStatus: state.pendingOps.length > 0 ? "pending" : "synced",
    });
    usePreviewOverridesStore.getState().reset();
    return { applied: true, stale: false, error: null };
  },

  enqueueBatch(batch) {
    const { localDoc, pendingOps } = get();
    if (!localDoc) {
      return new OpError(-1, "", "BAD_REQUEST", "timeline not loaded");
    }
    if (batch.optimistic !== false) {
      // 先在当前乐观视图上试算，非法批次直接拒绝，不入队。
      try {
        applyOps(localDoc, batch.operations);
      } catch (err) {
        if (err instanceof OpError) return err;
        throw err;
      }
    }
    const nextPending = [...pendingOps, batch];
    set((s) => ({
      pendingOps: nextPending,
      localDoc: deriveLocalDoc(s.doc, nextPending),
      syncStatus: "pending",
    }));
    return null;
  },

  confirmBatch(requestId, newRevision, authoritativeDoc) {
    let replayError: OpError | null = null;
    let applied = false;
    set((s) => {
      const index = s.pendingOps.findIndex((b) => b.requestId === requestId);
      if (index < 0 || !s.doc || newRevision < s.confirmedRevision) return {};
      const confirmedBatch = s.pendingOps[index];
      applied = true;
      // 旧后端不返回 doc 时保留纯函数推演兜底；正常提交路径会在成功后 GET，
      // 或直接传入新版响应中的权威 doc。
      const confirmedDoc =
        authoritativeDoc ??
        (confirmedBatch.optimistic === false
          ? s.doc
          : applyOps(s.doc, confirmedBatch.operations).doc);
      const rest = s.pendingOps.filter((b) => b.requestId !== requestId);
      try {
        return {
          doc: confirmedDoc,
          pendingOps: rest,
          confirmedRevision: newRevision,
          localDoc: deriveLocalDoc(confirmedDoc, rest),
          syncStatus: rest.length > 0 ? ("pending" as const) : ("synced" as const),
        };
      } catch (error) {
        if (!(error instanceof OpError)) throw error;
        replayError = error;
        return {
          doc: confirmedDoc,
          pendingOps: [],
          confirmedRevision: newRevision,
          localDoc: confirmedDoc,
          syncStatus: "conflict" as const,
        };
      }
    });
    if (applied) usePreviewOverridesStore.getState().reset();
    return replayError;
  },

  markBatchSending(requestId, baseRevision, attempts) {
    set((s) => ({
      pendingOps: s.pendingOps.map((b) =>
        b.requestId === requestId
          ? { ...b, baseRevision, attempts, status: "sending" as const }
          : b,
      ),
      syncStatus: "pending",
    }));
  },

  markBatchFailed(requestId) {
    set((s) => ({
      pendingOps: s.pendingOps.map((b) =>
        b.requestId === requestId ? { ...b, status: "failed" as const } : b,
      ),
    }));
  },

  dropPendingOnConflict() {
    set((s) => ({
      pendingOps: [],
      localDoc: s.doc,
      syncStatus: "conflict",
    }));
  },

  setSyncStatus(status) {
    set({ syncStatus: status });
  },

  setGestureActive(active) {
    set({ gestureActive: active });
  },

  setAgentHighlight(itemIds) {
    set({ agentHighlightItemIds: itemIds });
  },

  resetTimeline() {
    set({
      doc: null,
      confirmedRevision: 0,
      pendingOps: [],
      localDoc: null,
      syncStatus: "synced",
      gestureActive: false,
      agentHighlightItemIds: [],
    });
  },
}));
