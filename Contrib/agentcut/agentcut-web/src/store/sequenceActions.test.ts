import { beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import type { TimelineSnapshot } from "@/remote";

const apiMock = vi.hoisted(() => ({
  createTimeline: vi.fn(),
  switchTimeline: vi.fn(),
  renameTimeline: vi.fn(),
  deleteTimeline: vi.fn(),
  updateTimelineView: vi.fn(),
  getTranscriptView: vi.fn(),
}));

vi.mock("@/remote", async () => {
  const actual = await vi.importActual<typeof import("@/remote")>("@/remote");
  return { ...actual, api: apiMock };
});

import {
  createSequence,
  deleteSequence,
  renameSequence,
  switchSequence,
} from "./sequenceActions";
import { usePlaybackStore } from "./playbackStore";
import { resetSequenceViewSave, useSequenceStore } from "./sequenceStore";
import { setSubmitQueueProject } from "./submitOps";
import { useTimelineStore } from "./timelineStore";

function snapshot(
  activeTimelineId: string,
  timelines: TimelineSnapshot["timelines"],
  itemId?: string,
): TimelineSnapshot {
  const doc = newDoc("project", 1920, 1080, 30);
  doc.tracks = [{ trackId: `v_${activeTimelineId}`, kind: "video", createdBy: "auto" }];
  if (itemId) {
    doc.media = [{ mediaId: "image", uri: "blob://image", kind: "image", durationInFrames: 0 }];
    doc.items = [{
      itemId,
      trackId: `v_${activeTimelineId}`,
      kind: "image",
      mediaId: "image",
      from: 0,
      durationInFrames: 30,
    }];
  }
  const active = timelines?.find((timeline) => timeline.timelineId === activeTimelineId);
  return {
    doc,
    revision: active?.revision ?? 0,
    durationInFrames: itemId ? 30 : 0,
    activeTimelineId,
    timelines,
  };
}

const summaries = [
  {
    timelineId: "tl_1",
    name: "剪辑 1",
    revision: 2,
    durationInFrames: 30,
    currentFrame: 18,
    zoom: 1.5,
    createdAtUnixMs: 1,
    updatedAtUnixMs: 2,
  },
  {
    timelineId: "tl_2",
    name: "剪辑 2",
    revision: 0,
    durationInFrames: 0,
    currentFrame: 0,
    zoom: 0.75,
    createdAtUnixMs: 3,
    updatedAtUnixMs: 4,
  },
];

beforeEach(() => {
  vi.clearAllMocks();
  resetSequenceViewSave();
  useTimelineStore.getState().resetTimeline();
  useSequenceStore.getState().reset();
  useSequenceStore.setState({
    activeTimelineId: "tl_1",
    timelines: structuredClone(summaries),
    zoom: 1.5,
  });
  const first = snapshot("tl_1", summaries, "clip-one");
  useTimelineStore.getState().setConfirmed(first.doc, first.revision);
  usePlaybackStore.setState({ currentFrame: 18, isPlaying: true });
  setSubmitQueueProject("p1", "tl_1");
  apiMock.getTranscriptView.mockResolvedValue({
    timelineRevision: 0,
    entries: [],
    warnings: [],
  });
});

describe("sequence actions", () => {
  it("switches documents, playback position and zoom without mixing sequences", async () => {
    apiMock.switchTimeline.mockResolvedValue(snapshot("tl_2", summaries));

    expect(await switchSequence("p1", "tl_2")).toBeNull();

    expect(useSequenceStore.getState().activeTimelineId).toBe("tl_2");
    expect(useSequenceStore.getState().zoom).toBe(0.75);
    expect(useTimelineStore.getState().localDoc?.items).toEqual([]);
    expect(usePlaybackStore.getState().currentFrame).toBe(0);
    expect(usePlaybackStore.getState().isPlaying).toBe(false);
  });

  it("creates, renames and deletes persisted sequences through the API", async () => {
    const createdSummaries = [
      ...summaries,
      { ...summaries[1]!, timelineId: "tl_3", name: "剪辑 3", createdAtUnixMs: 5 },
    ];
    apiMock.createTimeline.mockResolvedValue(snapshot("tl_3", createdSummaries));
    expect(await createSequence("p1")).toBeNull();
    expect(useSequenceStore.getState().activeTimelineId).toBe("tl_3");

    const renamed = createdSummaries.map((timeline) =>
      timeline.timelineId === "tl_3" ? { ...timeline, name: "成片版" } : timeline,
    );
    apiMock.renameTimeline.mockResolvedValue(snapshot("tl_3", renamed));
    expect(await renameSequence("p1", "tl_3", "成片版")).toBeNull();
    expect(useSequenceStore.getState().timelines.at(-1)?.name).toBe("成片版");

    apiMock.deleteTimeline.mockResolvedValue(snapshot("tl_2", summaries));
    expect(await deleteSequence("p1", "tl_3")).toBeNull();
    expect(useSequenceStore.getState().activeTimelineId).toBe("tl_2");
  });
});
