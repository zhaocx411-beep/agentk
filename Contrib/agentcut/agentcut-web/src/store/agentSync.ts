// Agent 改动感知（设计 4.4）：Timeline 权威状态在 TimelineEntityWorkflow 里，
// 没有 "timeline.updated" 事件可订阅。前端在会话事件流里看到 Agent 的
// Timeline 工具执行成功（tool.exec_succeeded）后，全量 GET /timeline 合并进
// confirmedDoc（不信任 result 里的 revision/summary 做增量还原），并短暂
// 高亮 summary 中受影响的 item。
//
// 事件契约（agent-k internal/event/event.go，protojson camelCase）：
//   assistant.tool_call    data = { toolCallId, toolName, args }
//   tool.exec_succeeded    data = { toolCallId, result, artifacts?, duration }
// exec_succeeded 里没有工具名，需要用 toolCallId 回查 assistant.tool_call。

import { api } from "@/remote";
import type { SessionEvent } from "@/remote";

import { useTimelineStore } from "./timelineStore";
import { snapshotHistoryEntry, useHistoryStore } from "./historyStore";
import { refreshTranscriptView, resetTranscriptStore } from "./transcriptStore";

/** 会改动 Timeline 的 Agent 工具（tools/abi.go；timeline_get 只读，不刷）。 */
const TIMELINE_MUTATING_TOOLS = new Set([
  // Agent-K 事件里的 toolName 通常是 Profile 暴露给模型的别名。
  "timeline_edit_clips",
  "timeline_edit_tracks",
  "timeline_set_captions",
  "timeline_edit_media",
  "timeline_edit_transcript",
  // 同时兼容直接以 capability ID 暴露工具的 Profile。
  "contrib.agentcut.timeline_edit_clips",
  "contrib.agentcut.timeline_edit_tracks",
  "contrib.agentcut.timeline_set_captions",
  "contrib.agentcut.timeline_edit_media",
  "contrib.agentcut.timeline_edit_transcript",
]);

const HIGHLIGHT_MS = 3000;

interface ToolCallData {
  toolCallId?: string;
  toolName?: string;
}

interface ExecSucceededData {
  toolCallId?: string;
  /** Timeline 工具的 result 是 entity.ApplyOpsResult：{ revision, summary }。 */
  result?: {
    revision?: number;
    summary?: {
      addedItemIds?: string[];
      updatedItemIds?: string[];
    };
  };
}

// toolCallId → toolName。会话事件本身有限（Session 事件上限量级为千），
// 简单累积不做淘汰；项目切换时 reset。
const toolNameByCallId = new Map<string, string>();

// 手势或本地提交进行中收到的刷新请求先攒着，解除阻塞后统一放一次。
let deferredRefresh: { projectId: string; itemIds: string[] } | null = null;
let highlightTimer: ReturnType<typeof setTimeout> | null = null;
let projectGeneration = 0;
let activeProjectId: string | null = null;
const activeRefreshes = new Set<AbortController>();
let activeAgentTurn:
  | {
      projectId: string;
      beforeDoc: import("@/domain/timeline/types").TimelineDoc;
      beforeRevision: number;
      concurrentUserMutation: boolean;
      sawAgentMutation: boolean;
      finishing: boolean;
    }
  | null = null;

export function beginAgentTurn(projectId: string): void {
  const timeline = useTimelineStore.getState();
  if (!timeline.doc || timeline.pendingOps.length > 0) {
    activeAgentTurn = null;
    return;
  }
  activeAgentTurn = {
    projectId,
    beforeDoc: structuredClone(timeline.doc),
    beforeRevision: timeline.confirmedRevision,
    concurrentUserMutation: false,
    sawAgentMutation: false,
    finishing: false,
  };
}

export function cancelAgentTurn(projectId: string): void {
  if (activeAgentTurn?.projectId === projectId) activeAgentTurn = null;
}

export function markAgentTurnConcurrentMutation(projectId: string): void {
  if (activeAgentTurn?.projectId === projectId) {
    activeAgentTurn.concurrentUserMutation = true;
  }
}

export async function finishAgentTurn(projectId: string): Promise<void> {
  const turn = activeAgentTurn;
  if (!turn || turn.projectId !== projectId || turn.finishing) {
    requestTimelineCalibration(projectId);
    return;
  }
  turn.finishing = true;
  try {
    const fresh = await api.getTimeline(projectId);
    if (activeAgentTurn !== turn || activeProjectId !== projectId) return;
    useTimelineStore.getState().applyPollingSnapshot(fresh.doc, fresh.revision);
    void refreshTranscriptView(projectId, fresh.revision, { force: true });
    if (
      !turn.concurrentUserMutation &&
      fresh.revision > turn.beforeRevision
    ) {
      useHistoryStore
        .getState()
        .push(snapshotHistoryEntry("本次 Agent 修改", turn.beforeDoc, fresh.doc));
    } else if (fresh.revision > turn.beforeRevision) {
      useHistoryStore.getState().clear();
    }
  } catch (error) {
    console.warn("timeline calibration after agent turn failed", error);
  } finally {
    if (activeAgentTurn === turn) activeAgentTurn = null;
  }
}

// 订阅 store：手势结束时放行延迟的刷新。
useTimelineStore.subscribe((state, prev) => {
  const wasBlocked = prev.gestureActive || prev.pendingOps.length > 0;
  const isBlocked = state.gestureActive || state.pendingOps.length > 0;
  if (wasBlocked && !isBlocked && deferredRefresh) {
    const { projectId, itemIds } = deferredRefresh;
    deferredRefresh = null;
    void refreshNow(projectId, itemIds, projectGeneration);
  }
});

/** 项目切换/重新挂载时清空跨项目状态。 */
export function resetAgentSync(projectId: string | null = null): void {
  projectGeneration += 1;
  activeProjectId = projectId;
  for (const controller of activeRefreshes) controller.abort();
  activeRefreshes.clear();
  if (highlightTimer) clearTimeout(highlightTimer);
  highlightTimer = null;
  toolNameByCallId.clear();
  deferredRefresh = null;
  activeAgentTurn = null;
  resetTranscriptStore(projectId);
}

/**
 * 会话事件入口：AgentPanel 把所有持久事件（stream 与 listEvents 两条来源，
 * 已按 eventId 去重）转发进来。只关心 assistant.tool_call / tool.exec_succeeded。
 */
export function onSessionEvent(projectId: string, ev: SessionEvent): void {
  if (activeProjectId !== projectId) return;
  if (ev.kind === "assistant.tool_call") {
    const data = ev.data as ToolCallData;
    if (data?.toolCallId && data.toolName) {
      toolNameByCallId.set(data.toolCallId, data.toolName);
    }
    return;
  }
  if (ev.kind !== "tool.exec_succeeded") return;

  const data = ev.data as ExecSucceededData;
  const toolName = data?.toolCallId ? toolNameByCallId.get(data.toolCallId) : undefined;
  const reportedRevision = data.result?.revision;
  const revisionAdvanced =
    typeof reportedRevision === "number" &&
    reportedRevision > useTimelineStore.getState().confirmedRevision;
  if ((!toolName || !TIMELINE_MUTATING_TOOLS.has(toolName)) && !revisionAdvanced) return;
  if (activeAgentTurn?.projectId === projectId) {
    activeAgentTurn.sawAgentMutation = true;
  }

  const summary = data.result?.summary;
  const itemIds = [...(summary?.addedItemIds ?? []), ...(summary?.updatedItemIds ?? [])];

  const timeline = useTimelineStore.getState();
  if (timeline.gestureActive || timeline.pendingOps.length > 0) {
    // 用户正在拖拽：不打断本地手势，攒到手势结束后刷（多次改动合并成一次，
    // 高亮取并集）。
    deferredRefresh = {
      projectId,
      itemIds: [...new Set([...(deferredRefresh?.itemIds ?? []), ...itemIds])],
    };
    return;
  }
  void refreshNow(projectId, itemIds, projectGeneration);
}

/** SSE stream_end / 最终事件校准入口：即使工具事件丢失也会全量核对一次。 */
export function requestTimelineCalibration(projectId: string): void {
  if (activeProjectId !== projectId) return;
  const timeline = useTimelineStore.getState();
  if (timeline.gestureActive || timeline.pendingOps.length > 0) {
    deferredRefresh = {
      projectId,
      itemIds: deferredRefresh?.itemIds ?? [],
    };
    return;
  }
  void refreshNow(projectId, [], projectGeneration);
}

async function refreshNow(
  projectId: string,
  itemIds: string[],
  expectedGeneration: number,
): Promise<void> {
  const controller = new AbortController();
  activeRefreshes.add(controller);
  try {
    const fresh = await api.getTimeline(projectId, { signal: controller.signal });
    if (controller.signal.aborted || expectedGeneration !== projectGeneration) return;
    const store = useTimelineStore.getState();
    const previousRevision = store.confirmedRevision;
    const applied = store.applyPollingSnapshot(fresh.doc, fresh.revision);
    if (applied === "deferred") {
      deferredRefresh = {
        projectId,
        itemIds: [...new Set([...(deferredRefresh?.itemIds ?? []), ...itemIds])],
      };
      return;
    }
    if (applied === "stale") return;
    // 只有 revision 真正前进才说明权威文档发生过外部修改；stream_end 的
    // 等版本校准不能无条件抹掉用户仍然安全的 Undo/Redo。
    if (
      fresh.revision > previousRevision &&
      activeAgentTurn?.projectId !== projectId
    ) {
      useHistoryStore.getState().clear();
    }
    void refreshTranscriptView(projectId, fresh.revision, { force: true });
    if (itemIds.length > 0) {
      store.setAgentHighlight(itemIds);
      if (highlightTimer) clearTimeout(highlightTimer);
      highlightTimer = setTimeout(() => {
        useTimelineStore.getState().setAgentHighlight([]);
      }, HIGHLIGHT_MS);
    }
  } catch (err) {
    if (controller.signal.aborted || (err instanceof Error && err.name === "AbortError")) return;
    // 刷新失败不打断会话 UI；下一个改动事件或用户操作会再触发拉取。
    console.warn("timeline refresh after agent edit failed", err);
  } finally {
    activeRefreshes.delete(controller);
  }
}
