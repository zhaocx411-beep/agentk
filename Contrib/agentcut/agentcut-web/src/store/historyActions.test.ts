// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";

const restoreTimeline = vi.fn();
const getTranscriptView = vi.fn();
vi.mock("@/remote", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/remote")>();
  return {
    ...original,
    api: {
      restoreTimeline: (...args: unknown[]) => restoreTimeline(...args),
      getTranscriptView: (...args: unknown[]) => getTranscriptView(...args),
    },
  };
});

import { ApiError } from "@/remote";

import { undoTimeline } from "./historyActions";
import { snapshotHistoryEntry, useHistoryStore } from "./historyStore";
import { resetTranscriptStore } from "./transcriptStore";
import { useTimelineStore } from "./timelineStore";

beforeEach(() => {
  restoreTimeline.mockReset();
  getTranscriptView.mockResolvedValue({
    timelineRevision: 3,
    entries: [],
    warnings: [],
  });
  resetTranscriptStore("p1");
  useHistoryStore.getState().clear();
  const before = newDoc("before", 1920, 1080, 30);
  const after = newDoc("after", 1920, 1080, 30);
  useTimelineStore.getState().setConfirmed(after, 2);
  useHistoryStore.getState().push(snapshotHistoryEntry("本次 Agent 修改", before, after));
});

describe("snapshot history restore", () => {
  it("restores the full authoritative document with the current revision", async () => {
    const restored = newDoc("before", 1920, 1080, 30);
    restoreTimeline.mockResolvedValue({ revision: 3, doc: restored });

    await undoTimeline("p1");

    expect(restoreTimeline).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ baseRevision: 2, doc: expect.objectContaining({
        metadata: expect.objectContaining({ name: "before" }),
      }) }),
    );
    expect(useTimelineStore.getState().confirmedRevision).toBe(3);
    expect(useHistoryStore.getState().redoStack).toHaveLength(1);
  });

  it("keeps the undo entry when a concurrent revision causes conflict", async () => {
    restoreTimeline.mockRejectedValue(
      new ApiError(409, {
        code: "REVISION_CONFLICT",
        error: "revision changed",
        latestRevision: 3,
      }),
    );

    await undoTimeline("p1");

    expect(useHistoryStore.getState().undoStack).toHaveLength(1);
    expect(useHistoryStore.getState().redoStack).toHaveLength(0);
  });
});
