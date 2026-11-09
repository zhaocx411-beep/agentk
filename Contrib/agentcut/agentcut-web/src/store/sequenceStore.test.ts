import { beforeEach, describe, expect, it } from "vitest";

import { newDoc } from "@/domain/timeline/types";

import { useSequenceStore } from "./sequenceStore";

describe("sequenceStore", () => {
  beforeEach(() => useSequenceStore.getState().reset());

  it("hydrates the active timeline playback metadata and zoom", () => {
    useSequenceStore.getState().hydrate("project-1", {
      doc: newDoc("project", 1920, 1080, 30),
      revision: 7,
      durationInFrames: 120,
      activeTimelineId: "tl_2",
      timelines: [
        {
          timelineId: "tl_1",
          name: "剪辑 1",
          revision: 3,
          durationInFrames: 60,
          currentFrame: 12,
          zoom: 1,
          createdAtUnixMs: 1,
          updatedAtUnixMs: 2,
        },
        {
          timelineId: "tl_2",
          name: "剪辑 2",
          revision: 7,
          durationInFrames: 120,
          currentFrame: 42,
          zoom: 1.75,
          createdAtUnixMs: 3,
          updatedAtUnixMs: 4,
        },
      ],
    });

    expect(useSequenceStore.getState().activeTimelineId).toBe("tl_2");
    expect(useSequenceStore.getState().zoom).toBe(1.75);
    expect(useSequenceStore.getState().timelines[0]?.currentFrame).toBe(12);
  });

  it("migrates a legacy server snapshot without losing its revision", () => {
    useSequenceStore.getState().hydrate("legacy", {
      doc: newDoc("legacy", 1920, 1080, 30),
      revision: 5,
      durationInFrames: 90,
    });

    expect(useSequenceStore.getState().timelines).toEqual([
      expect.objectContaining({
        timelineId: "tl_legacy_legacy",
        name: "剪辑 1",
        revision: 5,
        durationInFrames: 90,
      }),
    ]);
  });
});
