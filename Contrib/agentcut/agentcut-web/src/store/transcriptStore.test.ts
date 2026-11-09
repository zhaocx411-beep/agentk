import { beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";

const getTranscriptView = vi.fn();
vi.mock("@/remote", () => ({
  api: {
    getTranscriptView: (...args: unknown[]) => getTranscriptView(...args),
  },
}));

import {
  refreshTranscriptView,
  resetTranscriptStore,
  useTranscriptStore,
} from "./transcriptStore";
import { useTimelineStore } from "./timelineStore";

function setTimeline(revision: number) {
  const doc = newDoc("test", 1920, 1080, 30);
  useTimelineStore.setState({
    doc,
    localDoc: doc,
    confirmedRevision: revision,
    pendingOps: [],
    syncStatus: "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
}

beforeEach(() => {
  getTranscriptView.mockReset();
  resetTranscriptStore("p1");
  setTimeline(3);
});

describe("transcriptStore", () => {
  it("loads the authoritative view and keeps occurrence identities", async () => {
    getTranscriptView.mockResolvedValue({
      timelineRevision: 3,
      warnings: ["segment fallback"],
      entries: [{ occurrenceId: "occ-1", text: "hello" }],
    });

    await refreshTranscriptView("p1", 3);

    const state = useTranscriptStore.getState();
    expect(state.status).toBe("ready");
    expect(state.entries[0]?.occurrenceId).toBe("occ-1");
    expect(state.warnings).toEqual(["segment fallback"]);
  });

  it("surfaces request failures", async () => {
    getTranscriptView.mockRejectedValue(new Error("backend unavailable"));
    await refreshTranscriptView("p1", 3);
    expect(useTranscriptStore.getState().status).toBe("error");
    expect(useTranscriptStore.getState().error).toContain("backend unavailable");
  });

  it("aborts and ignores an old project request", async () => {
    let resolveOld!: (value: object) => void;
    let oldSignal: AbortSignal | undefined;
    getTranscriptView.mockImplementationOnce(
      (_projectId: string, options: { signal?: AbortSignal }) =>
        new Promise((resolve, reject) => {
          resolveOld = resolve;
          oldSignal = options.signal;
          options.signal?.addEventListener("abort", () => {
            const error = new Error("aborted");
            error.name = "AbortError";
            reject(error);
          });
        }),
    );
    const old = refreshTranscriptView("p1", 3);
    await Promise.resolve();

    resetTranscriptStore("p2");
    setTimeline(1);
    expect(oldSignal?.aborted).toBe(true);
    resolveOld({ timelineRevision: 3, entries: [], warnings: [] });
    await old;

    const state = useTranscriptStore.getState();
    expect(state.projectId).toBe("p2");
    expect(state.timelineRevision).toBeNull();
  });
});
