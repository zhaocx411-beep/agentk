import { describe, expect, it } from "vitest";

import { newDoc } from "./types";
import { compatibleTargetTrack, snapFrame, snapTargets } from "./snap";

describe("timeline snapping", () => {
  it("collects playhead, timeline boundaries and neighboring clip edges", () => {
    const doc = newDoc("x", 1920, 1080, 30);
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push(
      {
        itemId: "a",
        trackId: "v1",
        kind: "image",
        mediaId: "img",
        from: 10,
        durationInFrames: 20,
      },
      {
        itemId: "b",
        trackId: "v1",
        kind: "image",
        mediaId: "img",
        from: 50,
        durationInFrames: 10,
      },
    );
    expect(snapTargets(doc, "v1", "a", 42)).toEqual([0, 42, 50, 60]);
  });

  it("snaps either moving clip edge using a fixed pixel threshold", () => {
    expect(snapFrame(47, [50], 2, 8)).toEqual({ frame: 50, target: 50 });
    // 20-frame clip: proposed start 31 makes its end 51, so end snaps to 50.
    expect(snapFrame(31, [50], 2, 8, 20)).toEqual({ frame: 30, target: 50 });
    expect(snapFrame(40, [50], 2, 8).target).toBeNull();
  });

  it("allows only unlocked compatible destination tracks", () => {
    const doc = newDoc("x", 1920, 1080, 30);
    doc.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "v2", kind: "video" },
      { trackId: "a1", kind: "audio" },
      { trackId: "locked", kind: "video", locked: true },
    );
    const item = {
      itemId: "image",
      trackId: "v1",
      kind: "image" as const,
      mediaId: "img",
      from: 0,
      durationInFrames: 30,
    };

    expect(compatibleTargetTrack(doc, item, "v2")).toBe("v2");
    expect(compatibleTargetTrack(doc, item, "a1")).toBe("v1");
    expect(compatibleTargetTrack(doc, item, "locked")).toBe("v1");
  });
});
