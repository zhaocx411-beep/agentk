import { describe, expect, it } from "vitest";

import {
  captionLayerStyle,
  duckGainAtFrame,
  effectivePreviewGain,
  itemVisualOpacity,
  resizeCrop,
  screenDeltaToSourceCropDelta,
  sidechainIntervals,
  transitionDisableReason,
  transitionFramesForDrop,
  transitionHandleSourceFrame,
  transitionAwareFadeEnvelope,
  transitionPreviewAt,
  validateM3BDoc,
  visualFitGeometry,
} from "./m3bSemantics";
import type { TimelineDoc } from "./timeline/types";
import { newDoc } from "./timeline/types";

function fixture(): TimelineDoc {
  const doc = newDoc("m3b", 1920, 1080, 30);
  doc.media = [
    {
      mediaId: "video",
      uri: "/video.mp4",
      kind: "video",
      durationInFrames: 180,
      probe: { hasAudio: true },
    },
    { mediaId: "voice", uri: "/voice.m4a", kind: "audio", durationInFrames: 180 },
    { mediaId: "bed", uri: "/bed.m4a", kind: "audio", durationInFrames: 180 },
  ];
  doc.tracks = [
    { trackId: "v", kind: "video" },
    { trackId: "voice", kind: "audio" },
    {
      trackId: "bed",
      kind: "audio",
      volume: 0.5,
      ducking: {
        sidechainTrackIds: ["voice", "v"],
        gain: 0.2,
        attackFrames: 10,
        releaseFrames: 10,
      },
    },
    { trackId: "c", kind: "caption" },
  ];
  doc.items = [
    {
      itemId: "from",
      trackId: "v",
      kind: "video",
      mediaId: "video",
      from: 0,
      durationInFrames: 60,
      trimStart: 0,
      volume: 0.8,
    },
    {
      itemId: "to",
      trackId: "v",
      kind: "video",
      mediaId: "video",
      from: 60,
      durationInFrames: 60,
      trimStart: 90,
    },
    {
      itemId: "voice",
      trackId: "voice",
      kind: "audio",
      mediaId: "voice",
      from: 40,
      durationInFrames: 20,
    },
    {
      itemId: "muted",
      trackId: "voice",
      kind: "audio",
      mediaId: "voice",
      from: 80,
      durationInFrames: 10,
      muted: true,
    },
    {
      itemId: "bed",
      trackId: "bed",
      kind: "audio",
      mediaId: "bed",
      from: 0,
      durationInFrames: 120,
      volume: 0.5,
      fadeInFrames: 10,
    },
  ];
  doc.transitions = [
    {
      transitionId: "tr",
      fromItemId: "from",
      toItemId: "to",
      kind: "crossfade",
      frames: 10,
    },
  ];
  return doc;
}

describe("M3B transition semantics", () => {
  it("computes crossfade and dip endpoints", () => {
    const doc = fixture();
    expect(transitionPreviewAt(doc, 60)).toMatchObject({
      fromOpacity: 1,
      toOpacity: 0,
    });
    expect(transitionPreviewAt(doc, 65)).toMatchObject({
      fromOpacity: 0.5,
      toOpacity: 0.5,
    });
    const middle = transitionPreviewAt(doc, 65);
    expect(
      effectivePreviewGain(doc, doc.tracks[0], doc.items[0], 65, middle),
    ).toBeCloseTo(0.4);
    expect(
      effectivePreviewGain(doc, doc.tracks[0], doc.items[1], 65, middle),
    ).toBeCloseTo(0.5);
    expect(transitionPreviewAt(doc, 70)).toBeNull();
    doc.transitions![0].kind = "dipToBlack";
    expect(transitionPreviewAt(doc, 65)).toMatchObject({
      fromOpacity: 0,
      toOpacity: 0,
      blackOpacity: 1,
    });
  });

  it("reads the from-item source handle after nominal end", () => {
    const doc = fixture();
    const from = doc.items[0];
    expect(transitionHandleSourceFrame(from, 60, 60)).toBe(60);
    expect(transitionHandleSourceFrame(from, 65, 60)).toBe(65);
    expect(transitionDisableReason(doc, from, doc.items[1], 10)).toBeNull();
    expect(transitionDisableReason(doc, from, doc.items[1], 70)).toContain("时长");
  });

  it("clamps the drop duration to a short but usable source handle", () => {
    const doc = fixture();
    const from = doc.items[0];
    const to = doc.items[1];
    doc.media[0].durationInFrames = 61;

    expect(transitionFramesForDrop(doc, from, to)).toBe(1);
    expect(transitionDisableReason(doc, from, to, 1)).toBeNull();
  });

  it("lets transitions own outgoing fadeOut and incoming fadeIn continuously", () => {
    const doc = fixture();
    doc.items[0].fadeInFrames = 10;
    doc.items[0].fadeOutFrames = 10;
    doc.items[1].fadeInFrames = 10;
    doc.items[1].fadeOutFrames = 10;
    const from = doc.items[0];
    const to = doc.items[1];

    expect(transitionAwareFadeEnvelope(doc, from, 5)).toBeCloseTo(0.5);
    expect(transitionAwareFadeEnvelope(doc, from, 59)).toBe(1);
    expect(itemVisualOpacity(doc, from, 59)).toBe(1);
    expect(transitionAwareFadeEnvelope(doc, to, 60)).toBe(1);

    const start = transitionPreviewAt(doc, 60);
    const middle = transitionPreviewAt(doc, 65);
    expect(effectivePreviewGain(doc, doc.tracks[0], from, 60, start)).toBeCloseTo(0.8);
    expect(effectivePreviewGain(doc, doc.tracks[0], to, 60, start)).toBe(0);
    expect(effectivePreviewGain(doc, doc.tracks[0], from, 65, middle)).toBeCloseTo(0.4);
    expect(effectivePreviewGain(doc, doc.tracks[0], to, 65, middle)).toBeCloseTo(0.5);
    expect(effectivePreviewGain(doc, doc.tracks[0], to, 70, null)).toBe(1);
    expect(transitionAwareFadeEnvelope(doc, to, 115)).toBeCloseTo(0.5);
  });
});

describe("M3B duck semantics", () => {
  it("unions sidechains and ignores muted items", () => {
    const doc = fixture();
    const config = doc.tracks[2].ducking!;
    expect(sidechainIntervals(doc, config)).toEqual([{ start: 0, end: 120 }]);
    doc.media[0].probe!.hasAudio = false;
    expect(sidechainIntervals(doc, config)).toEqual([{ start: 40, end: 60 }]);
  });

  it("combines track, item, fade and duck gains", () => {
    const doc = fixture();
    doc.media[0].probe!.hasAudio = false;
    const intervals = sidechainIntervals(doc, doc.tracks[2].ducking!);
    expect(duckGainAtFrame(intervals, doc.tracks[2].ducking, 30)).toBe(1);
    expect(duckGainAtFrame(intervals, doc.tracks[2].ducking, 35)).toBeCloseTo(0.6);
    expect(duckGainAtFrame(intervals, doc.tracks[2].ducking, 50)).toBe(0.2);
    // frame 5: fade=.5, no duck, track=.5, item=.5
    expect(effectivePreviewGain(doc, doc.tracks[2], doc.items[4], 5)).toBeCloseTo(
      0.125,
    );
  });
});

describe("crop and caption semantics", () => {
  it("clamps crop resize to normalized source coordinates", () => {
    expect(
      resizeCrop({ x: 0.2, y: 0.2, width: 0.5, height: 0.5 }, "nw", -1, -1),
    ).toEqual({ x: 0, y: 0, width: 0.7, height: 0.7 });
  });

  it("computes crop, rotation, fit and PiP geometry in order", () => {
    const transform = {
      x: 0.5,
      y: 0.5,
      width: 0.5,
      height: 0.5,
      fit: "contain" as const,
      crop: { x: 0.25, y: 0, width: 0.5, height: 1 },
      rotationDegrees: 90,
    };
    const geometry = visualFitGeometry(1920, 1080, 1920, 1080, transform);
    expect(geometry.rotatedSourceWidth).toBeCloseTo(1080);
    expect(geometry.rotatedSourceHeight).toBeCloseTo(960);
    expect(geometry.fittedWidth).toBeLessThanOrEqual(960);
    expect(geometry.fittedHeight).toBeLessThanOrEqual(540);
    expect(geometry.offsetX).toBeGreaterThanOrEqual(0);
    expect(geometry.offsetY).toBeGreaterThanOrEqual(0);
    const localDelta = screenDeltaToSourceCropDelta(
      0,
      40,
      400,
      225,
      1920,
      1080,
      1920,
      1080,
      transform,
    );
    expect(localDelta.x).toBeGreaterThan(0);
    expect(localDelta.y).toBeCloseTo(0);
  });

  it("maps caption style without accepting arbitrary CSS", () => {
    const item = {
      itemId: "caption",
      trackId: "c",
      kind: "text" as const,
      from: 0,
      durationInFrames: 30,
      text: "中文\n换行",
      captionStyle: {
        textColor: "#FFFFFF",
        backgroundColor: "#00000080",
        outlineColor: "#000000",
        outlineWidth: 2,
        x: 0.25,
        y: 0.3,
        align: "left" as const,
      },
    };
    expect(captionLayerStyle(item)).toMatchObject({
      left: "25%",
      top: "30%",
      textAlign: "left",
      whiteSpace: "pre-wrap",
    });
    const doc = fixture();
    doc.items.push({
      ...item,
      captionStyle: { fontFamily: "/tmp/font.ttf" as "sans" },
    });
    expect(validateM3BDoc(doc).some((issue) => issue.level === "error")).toBe(true);
  });
});
