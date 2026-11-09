import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TimelineDoc } from "@/domain/timeline/types";

const getTimeline = vi.fn();
const getTranscriptView = vi.fn();
vi.mock("@/remote", () => ({
  api: {
    getTimeline: (...args: unknown[]) => getTimeline(...args),
    getTranscriptView: (...args: unknown[]) => getTranscriptView(...args),
  },
}));

import {
  beginAgentTurn,
  finishAgentTurn,
  markAgentTurnConcurrentMutation,
  onSessionEvent,
  requestTimelineCalibration,
  resetAgentSync,
} from "./agentSync";
import { useHistoryStore } from "./historyStore";
import { useTimelineStore } from "./timelineStore";

function doc(): TimelineDoc {
  return {
    schemaVersion: 1,
    metadata: { width: 1920, height: 1080, fps: 30 },
    media: [],
    tracks: [{ trackId: "v1", kind: "video", name: "视频 1" }],
    items: [
      { itemId: "it_a", trackId: "v1", kind: "text", from: 0, durationInFrames: 30, text: "a" },
    ],
  };
}

function toolCall(id: number, callId: string, toolName: string) {
  return {
    eventId: `evt_${id}`,
    kind: "assistant.tool_call",
    data: { toolCallId: callId, toolName, args: {} },
    createdAt: "2026-01-01T00:00:00Z",
  };
}

function execSucceeded(id: number, callId: string, updatedItemIds: string[] = []) {
  return {
    eventId: `evt_${id}`,
    kind: "tool.exec_succeeded",
    data: { toolCallId: callId, result: { revision: 9, summary: { updatedItemIds } } },
    createdAt: "2026-01-01T00:00:01Z",
  };
}

async function flush() {
  await Promise.resolve();
  await Promise.resolve();
}

beforeEach(() => {
  resetAgentSync("p1");
  getTimeline.mockReset();
  getTimeline.mockResolvedValue({ doc: doc(), revision: 9, durationInFrames: 30 });
  getTranscriptView.mockReset();
  getTranscriptView.mockResolvedValue({
    timelineRevision: 9,
    entries: [],
    warnings: [],
  });
  useTimelineStore.setState({
    doc: null,
    confirmedRevision: 0,
    pendingOps: [],
    localDoc: null,
    syncStatus: "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
  useHistoryStore.getState().clear();
});

describe("agentSync", () => {
  it("Timeline 工具成功后全量刷新并高亮受影响 item", async () => {
    onSessionEvent("p1", toolCall(1, "c1", "contrib.agentcut.timeline_edit_clips"));
    onSessionEvent("p1", execSucceeded(2, "c1", ["it_a"]));
    await flush();

    expect(getTimeline).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    const s = useTimelineStore.getState();
    expect(s.confirmedRevision).toBe(9);
    expect(s.agentHighlightItemIds).toEqual(["it_a"]);
  });

  it("Profile 工具别名成功后同样触发全量刷新", async () => {
    onSessionEvent("p1", toolCall(1, "c1", "timeline_edit_tracks"));
    onSessionEvent("p1", execSucceeded(2, "c1"));
    await flush();

    expect(getTimeline).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(useTimelineStore.getState().confirmedRevision).toBe(9);
  });

  it("只读工具（timeline_get）与未知 toolCallId 不触发刷新", async () => {
    useTimelineStore.getState().setConfirmed(doc(), 9);
    onSessionEvent("p1", toolCall(1, "c1", "contrib.agentcut.timeline_get"));
    onSessionEvent("p1", execSucceeded(2, "c1"));
    onSessionEvent("p1", execSucceeded(3, "c-unknown"));
    await flush();

    expect(getTimeline).not.toHaveBeenCalled();
  });

  it("未知工具结果携带更高 revision 时仍进行校准", async () => {
    useTimelineStore.getState().setConfirmed(doc(), 1);
    onSessionEvent("p1", execSucceeded(3, "c-unknown"));
    await flush();

    expect(getTimeline).toHaveBeenCalledOnce();
    expect(useTimelineStore.getState().confirmedRevision).toBe(9);
  });

  it("stream_end 最终校准会重新获取权威 Timeline", async () => {
    requestTimelineCalibration("p1");
    await flush();

    expect(getTimeline).toHaveBeenCalledOnce();
    expect(getTranscriptView).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(useTimelineStore.getState().confirmedRevision).toBe(9);
  });

  it("等 revision 的 stream_end 校准保留仍安全的历史记录", async () => {
    useTimelineStore.getState().setConfirmed(doc(), 9);
    useHistoryStore.getState().push({
      id: "history",
      label: "移动片段",
      forward: [{ op: "moveItem", itemId: "it_a", from: 10 }],
      inverse: [{ op: "moveItem", itemId: "it_a", from: 0 }],
    });

    requestTimelineCalibration("p1");
    await flush();

    expect(useHistoryStore.getState().undoStack).toHaveLength(1);
  });

  it("拖拽手势进行中不刷新，手势结束后合并放行一次", async () => {
    useTimelineStore.getState().setGestureActive(true);
    onSessionEvent("p1", toolCall(1, "c1", "contrib.agentcut.timeline_edit_clips"));
    onSessionEvent("p1", execSucceeded(2, "c1", ["it_a"]));
    onSessionEvent("p1", toolCall(3, "c2", "contrib.agentcut.timeline_set_captions"));
    onSessionEvent("p1", execSucceeded(4, "c2", ["it_b"]));
    await flush();
    expect(getTimeline).not.toHaveBeenCalled();

    useTimelineStore.getState().setGestureActive(false);
    await flush();
    expect(getTimeline).toHaveBeenCalledTimes(1);
    expect(useTimelineStore.getState().agentHighlightItemIds).toEqual(["it_a", "it_b"]);
  });

  it("本地批次待确认时延迟 Agent 刷新，确认后再拉取", async () => {
    const base = doc();
    base.tracks[0] = { trackId: "v1", kind: "caption", name: "字幕 1" };
    useTimelineStore.getState().setConfirmed(base, 1);
    useTimelineStore.getState().enqueueBatch({
      requestId: "local",
      baseRevision: 1,
      operations: [{ op: "moveItem", itemId: "it_a", from: 60 }],
      status: "sending",
    });
    onSessionEvent("p1", toolCall(1, "c1", "timeline_edit_clips"));
    onSessionEvent("p1", execSucceeded(2, "c1", ["it_a"]));
    await flush();
    expect(getTimeline).not.toHaveBeenCalled();

    useTimelineStore.getState().confirmBatch("local", 2);
    await flush();
    expect(getTimeline).toHaveBeenCalledOnce();
    expect(useTimelineStore.getState().confirmedRevision).toBe(9);
  });

  it("项目 generation 变更后忽略并中止旧项目的在途刷新", async () => {
    resetAgentSync("old");
    let resolveOld!: (value: { doc: TimelineDoc; revision: number }) => void;
    getTimeline.mockImplementationOnce(
      (_projectId: string, opts: { signal?: AbortSignal }) =>
        new Promise((resolve, reject) => {
          resolveOld = resolve;
          opts.signal?.addEventListener("abort", () => {
            const error = new Error("aborted");
            error.name = "AbortError";
            reject(error);
          });
        }),
    );
    onSessionEvent("old", toolCall(1, "c1", "timeline_edit_clips"));
    onSessionEvent("old", execSucceeded(2, "c1", ["it_a"]));
    await Promise.resolve();

    resetAgentSync("new");
    const next = doc();
    next.metadata.name = "new project";
    useTimelineStore.getState().resetTimeline();
    useTimelineStore.getState().setConfirmed(next, 1);
    resolveOld({ doc: doc(), revision: 99 });
    await flush();

    expect(useTimelineStore.getState().confirmedRevision).toBe(1);
    expect(useTimelineStore.getState().doc?.metadata.name).toBe("new project");
  });

  it("records multiple Agent timeline revisions as one snapshot history entry", async () => {
    useTimelineStore.getState().setConfirmed(doc(), 1);
    const after = doc();
    after.metadata.name = "agent result";
    getTimeline.mockResolvedValue({ doc: after, revision: 3, durationInFrames: 30 });
    beginAgentTurn("p1");
    onSessionEvent("p1", toolCall(1, "c1", "timeline_edit_clips"));
    onSessionEvent("p1", execSucceeded(2, "c1", ["it_a"]));
    onSessionEvent("p1", toolCall(3, "c2", "timeline_edit_tracks"));
    onSessionEvent("p1", execSucceeded(4, "c2"));

    await finishAgentTurn("p1");

    expect(useHistoryStore.getState().undoStack).toHaveLength(1);
    expect(useHistoryStore.getState().undoStack[0]).toMatchObject({
      kind: "snapshot",
      label: "本次 Agent 修改",
    });
  });

  it("does not add Agent history for a no-op or concurrent user mutation", async () => {
    useTimelineStore.getState().setConfirmed(doc(), 1);
    getTimeline.mockResolvedValue({ doc: doc(), revision: 1, durationInFrames: 30 });
    beginAgentTurn("p1");
    await finishAgentTurn("p1");
    expect(useHistoryStore.getState().undoStack).toHaveLength(0);

    const after = doc();
    after.metadata.name = "mixed result";
    getTimeline.mockResolvedValue({ doc: after, revision: 3, durationInFrames: 30 });
    beginAgentTurn("p1");
    onSessionEvent("p1", toolCall(5, "c3", "timeline_edit_clips"));
    onSessionEvent("p1", execSucceeded(6, "c3"));
    markAgentTurnConcurrentMutation("p1");
    await finishAgentTurn("p1");
    expect(useHistoryStore.getState().undoStack).toHaveLength(0);
  });
});
