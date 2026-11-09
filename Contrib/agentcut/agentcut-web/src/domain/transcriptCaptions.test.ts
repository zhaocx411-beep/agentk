import { describe, expect, it } from "vitest";

import { newDoc } from "./timeline/types";
import type { Media } from "./timeline/types";
import {
  buildTranscriptCaptionOps,
  transcriptSegmentsToCaptions,
  transcriptSegmentsForItem,
} from "./transcriptCaptions";

const transcriptMedia: Media = {
  mediaId: "m1",
  uri: "blob://m1",
  kind: "video",
  durationInFrames: 300,
  transcript: {
    segments: [
      { startMs: 0, endMs: 1050, text: "第一句" },
      { startMs: 1500, endMs: 2000, text: " 第二句 " },
    ],
  },
};

describe("transcript caption conversion", () => {
  it("rounds real millisecond timestamps into project frames", () => {
    expect(transcriptSegmentsToCaptions(transcriptMedia.transcript!.segments, 30)).toEqual([
      { from: 0, durationInFrames: 32, text: "第一句" },
      { from: 45, durationInFrames: 15, text: "第二句" },
    ]);
  });

  it("maps source timestamps through item from, trimStart and speed", () => {
    expect(
      transcriptSegmentsForItem(
        [
          { startMs: 0, endMs: 1050, text: "跨越裁剪边界" },
          { startMs: 1500, endMs: 2000, text: "二倍速" },
        ],
        {
          itemId: "clip1",
          trackId: "v1",
          kind: "video",
          mediaId: "m1",
          from: 300,
          durationInFrames: 60,
          trimStart: 30,
          speed: 2,
        },
        30,
      ),
    ).toEqual([
      { from: 300, durationInFrames: 1, text: "跨越裁剪边界" },
      { from: 308, durationInFrames: 7, text: "二倍速" },
    ]);
  });

  it("creates and then reuses a stable per-media caption track", () => {
    const doc = newDoc("测试", 1920, 1080, 30);
    doc.tracks = [{ trackId: "v1", kind: "video" }];
    doc.media = [transcriptMedia];
    doc.items = [
      {
        itemId: "clip1",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 90,
        durationInFrames: 90,
      },
    ];

    const first = buildTranscriptCaptionOps(doc, transcriptMedia);
    expect(first).toHaveLength(2);
    expect(first[0]).toMatchObject({ op: "addTrack", track: { kind: "caption" } });
    const trackID = first[0].op === "addTrack" ? first[0].track.trackId : "";
    expect(trackID).toMatch(/^cauto_/);
    expect(first[1]).toMatchObject({ op: "setCaptions", trackId: trackID });

    doc.tracks.push({ trackId: trackID, kind: "caption", name: "自动字幕：视频" });
    doc.items.push({
      itemId: "old-caption",
      trackId: trackID,
      kind: "text",
      from: 90,
      durationInFrames: 32,
      text: "旧字幕",
      stylePresetId: "subtitle-outline",
      captionStyle: {
        fontSize: 42,
        textColor: "#FFFF00",
        backgroundColor: "#00000080",
      },
    });
    const repeated = buildTranscriptCaptionOps(doc, transcriptMedia);
    expect(repeated).toHaveLength(1);
    expect(repeated[0]).toMatchObject({ op: "setCaptions", trackId: trackID });
    expect(repeated[0].op).toBe("setCaptions");
    if (repeated[0].op !== "setCaptions") throw new Error("expected setCaptions");
    expect(repeated[0].captions[0]).toMatchObject({
      stylePresetId: "subtitle-outline",
      captionStyle: {
        fontSize: 42,
        textColor: "#FFFF00",
        backgroundColor: "#00000080",
      },
    });
  });

  it("uses different tracks for different media so generation cannot erase prior captions", () => {
    const doc = newDoc("测试", 1920, 1080, 30);
    doc.tracks = [{ trackId: "v1", kind: "video" }];
    doc.items = [
      {
        itemId: "clip1",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 90,
      },
      {
        itemId: "clip2",
        trackId: "v1",
        kind: "video",
        mediaId: "m2",
        from: 120,
        durationInFrames: 90,
      },
    ];
    const secondMedia = { ...transcriptMedia, mediaId: "m2", uri: "blob://m2" };
    const first = buildTranscriptCaptionOps(doc, transcriptMedia);
    const second = buildTranscriptCaptionOps(doc, secondMedia);
    const firstID = first[0].op === "addTrack" ? first[0].track.trackId : "";
    const secondID = second[0].op === "addTrack" ? second[0].track.trackId : "";
    expect(firstID).not.toBe(secondID);
  });

  it("does not try to recreate a locked auto-caption track with the same ID", () => {
    const doc = newDoc("locked", 1920, 1080, 30);
    doc.media = [transcriptMedia];
    doc.tracks = [{ trackId: "v1", kind: "video" }];
    doc.items = [
      {
        itemId: "clip1",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 90,
      },
    ];
    const first = buildTranscriptCaptionOps(doc, transcriptMedia);
    const trackID =
      first[0].op === "addTrack" ? first[0].track.trackId : "";
    doc.tracks.push({ trackId: trackID, kind: "caption", locked: true });

    expect(buildTranscriptCaptionOps(doc, transcriptMedia)).toEqual([]);
  });

  it("disables generation when there are no valid segments or no timeline instance", () => {
    const doc = newDoc("测试", 1920, 1080, 30);
    expect(
      buildTranscriptCaptionOps(doc, {
        ...transcriptMedia,
        transcript: { segments: [] },
      }),
    ).toEqual([]);
    expect(buildTranscriptCaptionOps(doc, transcriptMedia)).toEqual([]);
  });
});
