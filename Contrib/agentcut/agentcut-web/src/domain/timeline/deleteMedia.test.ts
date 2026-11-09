import { describe, expect, it } from "vitest";

import { autoCaptionTrackBaseId } from "../captionTracks";
import { buildDeleteMediaOps } from "./deleteMedia";
import { newDoc } from "./types";

describe("buildDeleteMediaOps", () => {
  it("removes every timeline instance before removing referenced media", () => {
    const doc = newDoc("delete", 1920, 1080, 30);
    doc.items = [
      {
        itemId: "video-use",
        trackId: "v1",
        mediaId: "target",
        kind: "video",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "audio-use",
        trackId: "a1",
        mediaId: "target",
        kind: "audio",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "keep",
        trackId: "v1",
        mediaId: "other",
        kind: "video",
        from: 30,
        durationInFrames: 30,
      },
    ];

    expect(buildDeleteMediaOps(doc, "target")).toEqual([
      { op: "removeItems", itemIds: ["video-use", "audio-use"] },
      { op: "removeMedia", mediaId: "target" },
    ]);
  });

  it("removes an unused media directly", () => {
    const doc = newDoc("delete", 1920, 1080, 30);
    expect(buildDeleteMediaOps(doc, "unused")).toEqual([
      { op: "removeMedia", mediaId: "unused" },
    ]);
  });

  it("removes the media's internal auto-caption track with the source", () => {
    const doc = newDoc("delete captions", 1920, 1080, 30);
    doc.media.push({
      mediaId: "target",
      uri: "blob://target",
      kind: "video",
      durationInFrames: 30,
    });
    const captionTrackId = autoCaptionTrackBaseId("target");
    doc.tracks.push({ trackId: captionTrackId, kind: "caption" });
    doc.items.push({
      itemId: "caption",
      trackId: captionTrackId,
      kind: "text",
      from: 0,
      durationInFrames: 10,
      text: "caption",
    });

    expect(buildDeleteMediaOps(doc, "target")).toEqual([
      { op: "removeTrack", trackId: captionTrackId },
      { op: "removeMedia", mediaId: "target" },
    ]);
  });
});
