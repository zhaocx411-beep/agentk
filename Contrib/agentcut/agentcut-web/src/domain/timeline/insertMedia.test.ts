import { describe, expect, it } from "vitest";

import { buildAppendMediaOps, buildInsertMediaOps } from "./insertMedia";
import { newDoc } from "./types";
import type { Media } from "./types";

function media(kind: Media["kind"]): Media {
  return {
    mediaId: `m_${kind}`,
    uri: `mock://${kind}`,
    kind,
    durationInFrames: kind === "image" ? 0 : 90,
  };
}

describe("buildAppendMediaOps", () => {
  it("creates a compatible track and item for an empty timeline", () => {
    const doc = newDoc("test", 1920, 1080, 30);

    const operations = buildAppendMediaOps(doc, media("video"));

    expect(operations).toHaveLength(2);
    expect(operations[0]).toMatchObject({
      op: "addTrack",
      index: 0,
      track: { kind: "video", name: "视频轨", createdBy: "auto" },
    });
    const trackId =
      operations[0]?.op === "addTrack" ? operations[0].track.trackId : "";
    expect(trackId).toMatch(/^trk_/);
    expect(operations[1]).toMatchObject({
      op: "addItem",
      item: {
        trackId,
        kind: "video",
        mediaId: "m_video",
        from: 0,
        durationInFrames: 90,
      },
    });
  });

  it("appends to an existing unlocked compatible track", () => {
    const doc = newDoc("test", 1920, 1080, 30);
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "existing",
      trackId: "v1",
      kind: "video",
      mediaId: "existing-media",
      from: 10,
      durationInFrames: 40,
    });

    const operations = buildAppendMediaOps(doc, media("image"));

    expect(operations).toHaveLength(1);
    expect(operations[0]).toMatchObject({
      op: "addItem",
      item: {
        trackId: "v1",
        kind: "image",
        from: 50,
        durationInFrames: 90,
      },
    });
  });

  it("creates a new track when every compatible track is locked", () => {
    const doc = newDoc("test", 1920, 1080, 30);
    doc.tracks.push({ trackId: "a1", kind: "audio", locked: true });

    const operations = buildAppendMediaOps(doc, media("audio"));

    expect(operations[0]).toMatchObject({
      op: "addTrack",
      index: 1,
      track: { kind: "audio", name: "音频轨", createdBy: "auto" },
    });
    const trackId =
      operations[0]?.op === "addTrack" ? operations[0].track.trackId : "";
    expect(operations[1]).toMatchObject({
      op: "addItem",
      item: { trackId, kind: "audio", mediaId: "m_audio", from: 0 },
    });
  });
});

describe("buildInsertMediaOps", () => {
  it("reuses A1 when the requested time range does not overlap", () => {
    const doc = newDoc("test", 1920, 1080, 30);
    doc.tracks = [{ trackId: "a1", kind: "audio", createdBy: "auto" }];
    doc.items = [{
      itemId: "first",
      trackId: "a1",
      kind: "audio",
      mediaId: "m_audio",
      from: 0,
      durationInFrames: 90,
    }];

    const plan = buildInsertMediaOps(doc, media("audio"), 90, "a1", false, "a2");

    expect(plan.createdTrack).toBe(false);
    expect(plan.track.trackId).toBe("a1");
    expect(plan.operations).toHaveLength(1);
  });

  it("creates A2 when overlapping audio has no available existing track", () => {
    const doc = newDoc("test", 1920, 1080, 30);
    doc.tracks = [{ trackId: "a1", kind: "audio", createdBy: "auto" }];
    doc.items = [{
      itemId: "first",
      trackId: "a1",
      kind: "audio",
      mediaId: "m_audio",
      from: 0,
      durationInFrames: 90,
    }];

    const plan = buildInsertMediaOps(doc, media("audio"), 30, "a1", false, "a2");

    expect(plan.createdTrack).toBe(true);
    expect(plan.track).toMatchObject({
      trackId: "a2",
      kind: "audio",
      createdBy: "auto",
    });
    expect(plan.operations[0]).toMatchObject({ op: "addTrack", index: 1 });
  });

  it("creates V2 for an explicit video layer above V1", () => {
    const doc = newDoc("test", 1920, 1080, 30);
    doc.tracks = [{ trackId: "v1", kind: "video", createdBy: "auto" }];

    const plan = buildInsertMediaOps(doc, media("video"), 0, "v1", true, "v2");

    expect(plan.createdTrack).toBe(true);
    expect(plan.track.trackId).toBe("v2");
    expect(plan.operations[0]).toMatchObject({ op: "addTrack", index: 1 });
  });
});
