import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc, type TimelineDoc } from "@/domain/timeline/types";

const applyOpsMock = vi.fn();
const getTimelineMock = vi.fn();
const getTranscriptViewMock = vi.fn();
const deleteTranscriptEditsMock = vi.fn();
vi.mock("@/remote", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/remote")>();
  return {
    ...original,
    api: {
      applyOps: (...args: unknown[]) => applyOpsMock(...args),
      getTimeline: (...args: unknown[]) => getTimelineMock(...args),
      getTranscriptView: (...args: unknown[]) => getTranscriptViewMock(...args),
      deleteTranscriptEdits: (...args: unknown[]) => deleteTranscriptEditsMock(...args),
    },
  };
});

import { ApiError } from "@/remote";

import { useHistoryStore } from "./historyStore";
import {
  discardFailedChangesAndReload,
  resetSubmitQueue,
  setSubmitQueueProject,
  submitOps,
  submitTranscriptDelete,
} from "./submitOps";
import { useTimelineStore } from "./timelineStore";
import { resetTranscriptStore, useTranscriptStore } from "./transcriptStore";

function baseDoc(name = "local"): TimelineDoc {
  const doc = newDoc(name, 1920, 1080, 30);
  doc.tracks.push({ trackId: "c1", kind: "caption" });
  return doc;
}

async function flush() {
  await Promise.resolve();
  await Promise.resolve();
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("window", { setTimeout, clearTimeout });
  resetSubmitQueue("test reset");
  setSubmitQueueProject("p1");
  applyOpsMock.mockReset();
  getTimelineMock.mockReset();
  getTranscriptViewMock.mockReset();
  deleteTranscriptEditsMock.mockReset();
  getTranscriptViewMock.mockResolvedValue({
    timelineRevision: 1,
    entries: [],
    warnings: [],
  });
  resetTranscriptStore("p1");
  useHistoryStore.getState().clear();
  const doc = baseDoc();
  useTimelineStore.setState({
    doc,
    confirmedRevision: 1,
    pendingOps: [],
    localDoc: doc,
    syncStatus: "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
});

afterEach(() => {
  resetSubmitQueue("test cleanup");
  vi.useRealTimers();
});

describe("submitOps mutation queue", () => {
  it("serializes mutations and gives each one a unique clientMutationId", async () => {
    let releaseFirst!: (value: {
      revision: number;
      summary: object;
      doc: TimelineDoc;
    }) => void;
    let inFlight = 0;
    let maxInFlight = 0;
    const serverAfterFirst = baseDoc("server-first");
    const serverAfterSecond = structuredClone(serverAfterFirst);
    serverAfterSecond.metadata.backgroundColor = "#111111";
    applyOpsMock
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            inFlight += 1;
            maxInFlight = Math.max(maxInFlight, inFlight);
            releaseFirst = (value) => {
              inFlight -= 1;
              resolve(value);
            };
          }),
      )
      .mockImplementationOnce(async () => {
        inFlight += 1;
        maxInFlight = Math.max(maxInFlight, inFlight);
        inFlight -= 1;
        return { revision: 3, summary: {}, doc: serverAfterSecond };
      });

    const first = submitOps("p1", [{ op: "setMetadata", metadata: { name: "first" } }]);
    const second = submitOps("p1", [
      { op: "setMetadata", metadata: { backgroundColor: "#111111" } },
    ]);
    await flush();
    expect(applyOpsMock).toHaveBeenCalledTimes(1);

    releaseFirst({ revision: 2, summary: {}, doc: serverAfterFirst });
    await expect(first).resolves.toBeNull();
    await expect(second).resolves.toBeNull();

    expect(applyOpsMock).toHaveBeenCalledTimes(2);
    expect(maxInFlight).toBe(1);
    const firstRequest = applyOpsMock.mock.calls[0][1];
    const secondRequest = applyOpsMock.mock.calls[1][1];
    expect(firstRequest.baseRevision).toBe(1);
    expect(secondRequest.baseRevision).toBe(2);
    expect(firstRequest.clientMutationId).toBe(firstRequest.requestId);
    expect(secondRequest.clientMutationId).toBe(secondRequest.requestId);
    expect(firstRequest.clientMutationId).not.toBe(secondRequest.clientMutationId);
    expect(useTimelineStore.getState().doc).toEqual(serverAfterSecond);
  });

  it("serializes transcript delete behind normal ops and sends only occurrence ranges", async () => {
    let releaseFirst!: (value: {
      revision: number;
      summary: object;
      doc: TimelineDoc;
    }) => void;
    const afterFirst = baseDoc("after normal op");
    const afterDelete = baseDoc("after transcript delete");
    applyOpsMock.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          releaseFirst = resolve;
        }),
    );
    deleteTranscriptEditsMock.mockResolvedValue({
      revision: 3,
      doc: afterDelete,
      summary: {},
      deletedRanges: [{ fromFrame: 10, toFrame: 20 }],
      deletedDurationInFrames: 10,
    });

    const normal = submitOps("p1", [{ op: "setMetadata", metadata: { name: "normal" } }]);
    const transcriptDelete = submitTranscriptDelete("p1", [
      { fromOccurrenceId: "occ-a", toOccurrenceId: "occ-b" },
    ]);
    await flush();
    expect(applyOpsMock).toHaveBeenCalledOnce();
    expect(deleteTranscriptEditsMock).not.toHaveBeenCalled();

    releaseFirst({ revision: 2, summary: {}, doc: afterFirst });
    await expect(normal).resolves.toBeNull();
    await expect(transcriptDelete).resolves.toBeNull();

    expect(deleteTranscriptEditsMock).toHaveBeenCalledOnce();
    const request = deleteTranscriptEditsMock.mock.calls[0][1];
    expect(request.baseRevision).toBe(2);
    expect(request.mode).toBe("ripple");
    expect(request.ranges).toEqual([
      { fromOccurrenceId: "occ-a", toOccurrenceId: "occ-b" },
    ]);
    expect(JSON.stringify(request)).not.toContain("fromFrame");
    expect(JSON.stringify(request)).not.toContain("toFrame");
    expect(useTimelineStore.getState().doc).toEqual(afterDelete);
    expect(useHistoryStore.getState().undoStack).toHaveLength(2);
    expect(useHistoryStore.getState().undoStack.at(-1)).toEqual(
      expect.objectContaining({
        kind: "snapshot",
        label: "删除文字稿片段",
        beforeDoc: expect.objectContaining({
          metadata: expect.objectContaining({ name: "after normal op" }),
        }),
        afterDoc: afterDelete,
      }),
    );
  });

  it("clears transcript selection and refreshes on revision conflict", async () => {
    useTranscriptStore.getState().setSelection(["occ-a"], "occ-a");
    deleteTranscriptEditsMock.mockRejectedValue(
      new ApiError(409, {
        code: "REVISION_CONFLICT",
        error: "revision changed",
        latestRevision: 2,
      }),
    );
    const fresh = baseDoc("conflict refresh");
    getTimelineMock.mockResolvedValue({ doc: fresh, revision: 2, durationInFrames: 0 });
    getTranscriptViewMock.mockResolvedValue({
      timelineRevision: 2,
      entries: [],
      warnings: [],
    });

    await expect(
      submitTranscriptDelete("p1", [
        { fromOccurrenceId: "occ-a", toOccurrenceId: "occ-a" },
      ]),
    ).resolves.toContain("未应用，请重试");

    expect(useTimelineStore.getState().doc).toEqual(fresh);
    expect(useTranscriptStore.getState().selectedOccurrenceIds.size).toBe(0);
  });

  it("keeps the local timeline unchanged when locked tracks reject transcript delete", async () => {
    const before = useTimelineStore.getState().localDoc;
    useTranscriptStore.getState().setSelection(["occ-a"], "occ-a");
    deleteTranscriptEditsMock.mockRejectedValue(
      new ApiError(422, {
        code: "LOCKED_TRACK_CONFLICT",
        error: "locked track would be shifted",
      }),
    );
    getTimelineMock.mockResolvedValue({
      doc: before,
      revision: 1,
      durationInFrames: 0,
    });

    await expect(
      submitTranscriptDelete("p1", [
        { fromOccurrenceId: "occ-a", toOccurrenceId: "occ-a" },
      ]),
    ).resolves.toContain("locked track");

    expect(useTimelineStore.getState().localDoc).toBe(before);
    expect(useTranscriptStore.getState().selectedOccurrenceIds.has("occ-a")).toBe(true);
  });

  it("keeps a failed transcript request in the shared recovery path after retries", async () => {
    const before = useTimelineStore.getState().localDoc;
    deleteTranscriptEditsMock.mockRejectedValue(
      new ApiError(503, { error: "service unavailable" }),
    );
    const failed = submitTranscriptDelete("p1", [
      { fromOccurrenceId: "occ-a", toOccurrenceId: "occ-a" },
    ]);
    await flush();
    for (const delay of [300, 1000, 3000]) {
      await vi.advanceTimersByTimeAsync(delay);
      await flush();
    }
    await expect(failed).resolves.toContain("恢复入口仍保留");
    expect(deleteTranscriptEditsMock).toHaveBeenCalledTimes(4);
    expect(useTimelineStore.getState().pendingOps).toEqual([
      expect.objectContaining({ optimistic: false, status: "failed" }),
    ]);
    expect(useTimelineStore.getState().localDoc).toBe(before);

    const fresh = baseDoc("recovered");
    getTimelineMock.mockResolvedValue({ doc: fresh, revision: 6, durationInFrames: 0 });
    getTranscriptViewMock.mockResolvedValue({
      timelineRevision: 6,
      entries: [],
      warnings: [],
    });
    await expect(discardFailedChangesAndReload("p1")).resolves.toBeNull();
    expect(useTimelineStore.getState().pendingOps).toHaveLength(0);
    expect(useTimelineStore.getState().doc).toEqual(fresh);
  });

  it("uses a GET snapshot as authority when the mutation response has no doc", async () => {
    const canonical = baseDoc("canonical server name");
    applyOpsMock.mockResolvedValue({ revision: 2, summary: {} });
    getTimelineMock.mockResolvedValue({
      revision: 2,
      doc: canonical,
      durationInFrames: 0,
    });

    await expect(
      submitOps("p1", [{ op: "setMetadata", metadata: { name: "optimistic name" } }]),
    ).resolves.toBeNull();

    expect(getTimelineMock).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(useTimelineStore.getState().doc?.metadata.name).toBe("canonical server name");
  });

  it("clears unsafe history when the authoritative GET advances beyond the mutation", async () => {
    const canonical = baseDoc("external update");
    applyOpsMock.mockResolvedValue({ revision: 2, summary: {} });
    getTimelineMock.mockResolvedValue({
      revision: 3,
      doc: canonical,
      durationInFrames: 0,
    });

    await expect(
      submitOps("p1", [{ op: "setMetadata", metadata: { name: "local edit" } }]),
    ).resolves.toBeNull();

    expect(useHistoryStore.getState().undoStack).toHaveLength(0);
    expect(useTimelineStore.getState().confirmedRevision).toBe(3);
  });

  it("rejects stale-project gestures before they enter the queue", async () => {
    setSubmitQueueProject("new-project");

    await expect(
      submitOps("old-project", [{ op: "setMetadata", metadata: { name: "stale" } }]),
    ).resolves.toContain("项目已切换");
    expect(applyOpsMock).not.toHaveBeenCalled();
    expect(useTimelineStore.getState().pendingOps).toHaveLength(0);
  });

  it("does not replay on REVISION_CONFLICT and drops dependent queued edits", async () => {
    applyOpsMock.mockRejectedValueOnce(
      new ApiError(409, {
        code: "REVISION_CONFLICT",
        error: "revision changed",
        latestRevision: 2,
      }),
    );
    const fresh = baseDoc("server");
    getTimelineMock.mockResolvedValue({ doc: fresh, revision: 2, durationInFrames: 0 });

    const first = submitOps("p1", [{ op: "setMetadata", metadata: { name: "first" } }]);
    const dependent = submitOps("p1", [
      { op: "setMetadata", metadata: { backgroundColor: "#222222" } },
    ]);

    await expect(first).resolves.toContain("未应用，请重试");
    await expect(dependent).resolves.toContain("未应用，请重试");
    expect(applyOpsMock).toHaveBeenCalledOnce();
    expect(useTimelineStore.getState().pendingOps).toHaveLength(0);
    expect(useTimelineStore.getState().doc?.metadata.name).toBe("server");
    expect(useHistoryStore.getState().undoStack).toHaveLength(0);
  });

  it("recovers after consecutive 5xx failures and allows editing to continue", async () => {
    applyOpsMock.mockRejectedValue(
      new ApiError(503, {
        error: "timeline service unavailable",
      }),
    );

    const failed = submitOps("p1", [
      { op: "setMetadata", metadata: { name: "unsaved local edit" } },
    ]);
    await flush();
    for (const delay of [300, 1000, 3000]) {
      await vi.advanceTimersByTimeAsync(delay);
      await flush();
    }

    await expect(failed).resolves.toContain("本地编辑仍保留");
    expect(applyOpsMock).toHaveBeenCalledTimes(4);
    expect(useTimelineStore.getState().pendingOps).toEqual([
      expect.objectContaining({ status: "failed" }),
    ]);
    expect(useTimelineStore.getState().localDoc?.metadata.name).toBe("unsaved local edit");

    useHistoryStore.getState().push({
      id: "unsafe-history",
      label: "旧修改",
      forward: [],
      inverse: [],
    });
    const fresh = baseDoc("authoritative after recovery");
    getTimelineMock.mockResolvedValue({ doc: fresh, revision: 8, durationInFrames: 0 });

    await expect(discardFailedChangesAndReload("p1")).resolves.toBeNull();
    expect(useTimelineStore.getState().pendingOps).toHaveLength(0);
    expect(useTimelineStore.getState().syncStatus).toBe("synced");
    expect(useTimelineStore.getState().confirmedRevision).toBe(8);
    expect(useTimelineStore.getState().localDoc?.metadata.name).toBe(
      "authoritative after recovery",
    );
    expect(useHistoryStore.getState().undoStack).toHaveLength(0);

    const editedAgain = baseDoc("continued edit");
    applyOpsMock.mockResolvedValue({ revision: 9, summary: {}, doc: editedAgain });
    await expect(
      submitOps("p1", [{ op: "setMetadata", metadata: { name: "continued edit" } }]),
    ).resolves.toBeNull();
    expect(useTimelineStore.getState().confirmedRevision).toBe(9);
    expect(useTimelineStore.getState().pendingOps).toHaveLength(0);
  });

  it("aborts and ignores an old project response before starting the new queue", async () => {
    setSubmitQueueProject("old-project");
    let finishOld!: (value: { revision: number; summary: object; doc: TimelineDoc }) => void;
    let oldSignal: AbortSignal | undefined;
    applyOpsMock
      .mockImplementationOnce(
        (_projectId: string, _request: unknown, opts: { signal?: AbortSignal }) =>
          new Promise((resolve) => {
            oldSignal = opts.signal;
            finishOld = resolve;
          }),
      )
      .mockResolvedValueOnce({ revision: 1, summary: {}, doc: baseDoc("new server") });

    const oldResult = submitOps("old-project", [
      { op: "setMetadata", metadata: { name: "old edit" } },
    ]);
    await flush();
    resetSubmitQueue("switched");
    setSubmitQueueProject("new-project");
    expect(oldSignal?.aborted).toBe(true);

    useTimelineStore.getState().resetTimeline();
    useTimelineStore.getState().setConfirmed(baseDoc("new"), 0);
    const newResult = submitOps("new-project", [
      { op: "setMetadata", metadata: { name: "new edit" } },
    ]);
    expect(applyOpsMock).toHaveBeenCalledTimes(1);
    await expect(oldResult).resolves.toBe("switched");

    finishOld({ revision: 2, summary: {}, doc: baseDoc("stale old server") });
    await expect(newResult).resolves.toBeNull();
    expect(applyOpsMock).toHaveBeenCalledTimes(2);
    expect(applyOpsMock.mock.calls[1][0]).toBe("new-project");
    expect(useTimelineStore.getState().doc?.metadata.name).toBe("new server");
  });
});
