import { beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";

const startAudioSeparation = vi.fn();
const getAudioSeparation = vi.fn();
const getTimeline = vi.fn();
const getTranscriptView = vi.fn();
vi.mock("@/remote", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/remote")>();
  return {
    ...actual,
    api: {
      startAudioSeparation: (...args: unknown[]) =>
        startAudioSeparation(...args),
      getAudioSeparation: (...args: unknown[]) => getAudioSeparation(...args),
      getTimeline: (...args: unknown[]) => getTimeline(...args),
      getTranscriptView: (...args: unknown[]) => getTranscriptView(...args),
    },
  };
});

import { useHistoryStore } from "./historyStore";
import { separateAudioItem } from "./separateAudio";
import { resetTranscriptStore } from "./transcriptStore";
import { useTimelineStore } from "./timelineStore";

function docs() {
  const before = newDoc("before", 1920, 1080, 30);
  before.media.push({
    mediaId: "video",
    uri: "/video",
    kind: "video",
    durationInFrames: 120,
    probe: { hasAudio: true },
  });
  before.tracks.push({ trackId: "v1", kind: "video" });
  before.items.push({
    itemId: "clip",
    trackId: "v1",
    kind: "video",
    mediaId: "video",
    from: 0,
    durationInFrames: 60,
  });
  const after = structuredClone(before);
  after.items[0].muted = true;
  after.media.push({
    mediaId: "audio",
    uri: "/audio",
    kind: "audio",
    durationInFrames: 120,
  });
  after.tracks.push({ trackId: "a1", kind: "audio" });
  after.items.push({
    itemId: "audio-item",
    trackId: "a1",
    kind: "audio",
    mediaId: "audio",
    from: 0,
    durationInFrames: 60,
  });
  return { before, after };
}

beforeEach(() => {
  startAudioSeparation.mockReset();
  getAudioSeparation.mockReset();
  getTimeline.mockReset();
  getTranscriptView.mockResolvedValue({
    timelineRevision: 2,
    entries: [],
    warnings: [],
  });
  useHistoryStore.getState().clear();
  resetTranscriptStore("p1");
  const { before } = docs();
  useTimelineStore.getState().setConfirmed(before, 1);
});

describe("separateAudioItem", () => {
  it("applies one completed result and records snapshot undo", async () => {
    const { after } = docs();
    startAudioSeparation.mockResolvedValue({ workflowId: "wf", status: "running" });
    getAudioSeparation.mockResolvedValue({
      workflowId: "wf",
      status: "completed",
      result: {
        revision: 2,
        doc: after,
        audioMediaId: "audio",
        audioItemId: "audio-item",
        audioTrackId: "a1",
      },
    });

    await expect(separateAudioItem("p1", "clip")).resolves.toBeNull();

    expect(useTimelineStore.getState().confirmedRevision).toBe(2);
    expect(useHistoryStore.getState().undoStack).toEqual([
      expect.objectContaining({ kind: "snapshot", label: "分离音频" }),
    ]);
  });

  it("does not duplicate history when a repeated click returns an older completed run", async () => {
    const { after } = docs();
    useTimelineStore.getState().setConfirmed(after, 2);
    startAudioSeparation.mockResolvedValue({ workflowId: "wf", status: "running" });
    getAudioSeparation.mockResolvedValue({
      workflowId: "wf",
      status: "completed",
      result: {
        revision: 2,
        doc: after,
        audioMediaId: "audio",
        audioItemId: "audio-item",
        audioTrackId: "a1",
      },
    });
    getTimeline.mockResolvedValue({ doc: after, revision: 2, durationInFrames: 60 });

    await expect(separateAudioItem("p1", "clip")).resolves.toBeNull();

    expect(useHistoryStore.getState().undoStack).toHaveLength(0);
  });

  it("deduplicates repeated separation clicks while one request is active", async () => {
    const { after } = docs();
    let releaseStatus!: () => void;
    const statusGate = new Promise<void>((resolve) => {
      releaseStatus = resolve;
    });
    startAudioSeparation.mockResolvedValue({
      workflowId: "wf",
      status: "running",
    });
    getAudioSeparation.mockImplementation(async () => {
      await statusGate;
      return {
        workflowId: "wf",
        status: "completed",
        result: {
          revision: 3,
          doc: after,
          audioMediaId: "audio",
          audioItemId: "audio-item",
          audioTrackId: "a1",
        },
      };
    });

    const first = separateAudioItem("p1", "clip");
    const second = separateAudioItem("p1", "clip");
    expect(second).toBe(first);
    expect(startAudioSeparation).toHaveBeenCalledTimes(1);
    await vi.waitFor(() => expect(getAudioSeparation).toHaveBeenCalledTimes(1));

    releaseStatus();
    await expect(Promise.all([first, second])).resolves.toEqual([null, null]);
    expect(getAudioSeparation).toHaveBeenCalledTimes(1);
  });
});
