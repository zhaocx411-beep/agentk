import { describe, expect, it } from "vitest";

import { emptyAutoTrackIds, withAutoTrackCleanup } from "./autoTracks";
import { newDoc } from "./types";

describe("auto track cleanup", () => {
  it("keeps V1, occupied, locked, manual and legacy tracks", () => {
    const doc = newDoc("project", 1920, 1080, 30);
    doc.tracks = [
      { trackId: "v1", kind: "video", createdBy: "auto" },
      { trackId: "v2", kind: "video", createdBy: "auto" },
      { trackId: "v3", kind: "video", createdBy: "auto", locked: true },
      { trackId: "a1", kind: "audio", createdBy: "auto" },
      { trackId: "a2", kind: "audio", createdBy: "auto" },
      { trackId: "manual", kind: "audio", createdBy: "user" },
      { trackId: "legacy", kind: "audio" },
    ];
    doc.media = [{
      mediaId: "audio",
      uri: "blob://audio",
      kind: "audio",
      durationInFrames: 30,
    }];
    doc.items = [{
      itemId: "audio-item",
      trackId: "a1",
      kind: "audio",
      mediaId: "audio",
      from: 0,
      durationInFrames: 30,
    }];

    expect(emptyAutoTrackIds(doc)).toEqual(["v2", "a2"]);
  });

  it("appends cleanup after deleting the last A2 item", () => {
    const doc = newDoc("project", 1920, 1080, 30);
    doc.tracks = [
      { trackId: "v1", kind: "video", createdBy: "auto" },
      { trackId: "a1", kind: "audio", createdBy: "auto" },
      { trackId: "a2", kind: "audio", createdBy: "auto" },
    ];
    doc.media = [{
      mediaId: "audio",
      uri: "blob://audio",
      kind: "audio",
      durationInFrames: 30,
    }];
    doc.items = [{
      itemId: "audio-item",
      trackId: "a2",
      kind: "audio",
      mediaId: "audio",
      from: 0,
      durationInFrames: 30,
    }];

    expect(withAutoTrackCleanup(doc, [{
      op: "removeItems",
      itemIds: ["audio-item"],
    }])).toEqual([
      { op: "removeItems", itemIds: ["audio-item"] },
      { op: "removeTrack", trackId: "a1" },
      { op: "removeTrack", trackId: "a2" },
    ]);
  });
});
