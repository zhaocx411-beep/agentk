import { describe, expect, it } from "vitest";

import {
  autoCaptionMediaIdForTrack,
  autoCaptionTrackBaseId,
  autoCaptionTrackId,
  captionProjectionsForVideoTrack,
  mainTimelineTracks,
} from "./captionTracks";
import { newDoc } from "./timeline/types";

describe("caption track projection", () => {
  it("places authoritative caption tracks above the video stack", () => {
    const doc = newDoc("tracks", 1920, 1080, 30);
    doc.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "c1", kind: "caption" },
      { trackId: "a1", kind: "audio" },
    );

    expect(mainTimelineTracks(doc).map((track) => track.trackId)).toEqual([
      "c1",
      "v1",
      "a1",
    ]);
  });

  it("places higher video layers above V1 and audio layers below it", () => {
    const doc = newDoc("track-order", 1920, 1080, 30);
    doc.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "v2", kind: "video" },
      { trackId: "c1", kind: "caption" },
      { trackId: "a1", kind: "audio" },
      { trackId: "a2", kind: "audio" },
    );

    expect(mainTimelineTracks(doc).map((track) => track.trackId)).toEqual([
      "c1",
      "v2",
      "v1",
      "a1",
      "a2",
    ]);
  });

  it("reuses collision-suffixed IDs and resolves them to source media", () => {
    const doc = newDoc("ids", 1920, 1080, 30);
    doc.media.push({
      mediaId: "media-one",
      uri: "blob://one",
      kind: "video",
      durationInFrames: 90,
    });
    const base = autoCaptionTrackBaseId("media-one");
    doc.tracks.push(
      { trackId: base, kind: "video" },
      { trackId: `${base}_2`, kind: "caption" },
    );

    expect(autoCaptionTrackId(doc, "media-one")).toBe(`${base}_2`);
    expect(autoCaptionMediaIdForTrack(doc, `${base}_2`)).toBe("media-one");
  });

  it("maps multiple media onto their own video tracks without leakage", () => {
    const doc = newDoc("multi", 1920, 1080, 30);
    doc.media.push(
      {
        mediaId: "m1",
        uri: "blob://one",
        kind: "video",
        durationInFrames: 90,
      },
      {
        mediaId: "m2",
        uri: "blob://two",
        kind: "video",
        durationInFrames: 90,
      },
    );
    const c1 = autoCaptionTrackBaseId("m1");
    const c2 = autoCaptionTrackBaseId("m2");
    doc.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "v2", kind: "video" },
      { trackId: c1, kind: "caption" },
      { trackId: c2, kind: "caption" },
    );
    doc.items.push(
      {
        itemId: "v1-item",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 60,
      },
      {
        itemId: "v2-item",
        trackId: "v2",
        kind: "video",
        mediaId: "m2",
        from: 0,
        durationInFrames: 60,
      },
      {
        itemId: "m1-caption",
        trackId: c1,
        kind: "text",
        from: 0,
        durationInFrames: 20,
        text: "one",
      },
      {
        itemId: "m2-caption",
        trackId: c2,
        kind: "text",
        from: 0,
        durationInFrames: 20,
        text: "two",
      },
    );

    expect(
      captionProjectionsForVideoTrack(doc, "v1").map(
        (projection) => projection.item.itemId,
      ),
    ).toEqual(["m1-caption"]);
    expect(
      captionProjectionsForVideoTrack(doc, "v2").map(
        (projection) => projection.item.itemId,
      ),
    ).toEqual(["m2-caption"]);
  });

  it("does not reinterpret an orphaned auto track as a legacy global track", () => {
    const doc = newDoc("orphan", 1920, 1080, 30);
    doc.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "cauto_orphan", kind: "caption" },
    );
    doc.items.push(
      {
        itemId: "video",
        trackId: "v1",
        kind: "video",
        mediaId: "remaining",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "caption",
        trackId: "cauto_orphan",
        kind: "text",
        from: 0,
        durationInFrames: 10,
        text: "orphan",
      },
    );

    expect(captionProjectionsForVideoTrack(doc, "v1")).toEqual([]);
  });
});
