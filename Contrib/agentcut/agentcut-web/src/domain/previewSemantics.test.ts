import { describe, expect, it } from "vitest";

import type { Item, Track } from "./timeline/types";
import { effectiveItemGain, fadeEnvelope, visualLayerStyle } from "./previewSemantics";

const item: Item = {
  itemId: "i",
  trackId: "v",
  kind: "video",
  mediaId: "m",
  from: 10,
  durationInFrames: 100,
};
const track: Track = { trackId: "v", kind: "video" };

describe("preview M3A semantics", () => {
  it("uses historical default volume and combines gains", () => {
    expect(effectiveItemGain(track, item)).toBe(1);
    expect(effectiveItemGain({ ...track, volume: 0.5 }, { ...item, volume: 1.5 })).toBe(
      0.75,
    );
    expect(effectiveItemGain({ ...track, volume: 0 }, { ...item, volume: 0 })).toBe(1);
  });

  it("honors track and item mute", () => {
    expect(effectiveItemGain({ ...track, muted: true }, item)).toBe(0);
    expect(effectiveItemGain(track, { ...item, muted: true })).toBe(0);
  });

  it("computes linear fade endpoints and midpoint", () => {
    const fading = { ...item, fadeInFrames: 20, fadeOutFrames: 20 };
    expect(fadeEnvelope(fading, 10)).toBe(0);
    expect(fadeEnvelope(fading, 20)).toBe(0.5);
    expect(fadeEnvelope(fading, 60)).toBe(1);
    expect(fadeEnvelope(fading, 100)).toBe(0.5);
    expect(fadeEnvelope(fading, 110)).toBe(0);
  });

  it("maps normalized transform to scalable CSS", () => {
    const style = visualLayerStyle(
      {
        ...item,
        transform: { x: 0.55, y: 0.05, width: 0.4, height: 0.4, opacity: 0.8, fit: "cover" },
      },
      60,
    );
    expect(style).toMatchObject({
      top: "5%",
      width: "40%",
      height: "40%",
      opacity: 0.8,
    });
    expect(Number.parseFloat(String(style.left))).toBeCloseTo(55);
  });
});

