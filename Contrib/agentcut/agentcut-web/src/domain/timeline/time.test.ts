import { describe, expect, it } from "vitest";

import {
  clampFrame,
  formatFrameTimecode,
  frameFromRatio,
  frameToSeconds,
  millisecondsToFrame,
  secondsToFrame,
} from "./time";

describe("timeline frame clock", () => {
  it("uses one deterministic rounding policy for seconds and milliseconds", () => {
    expect(secondsToFrame(1.5, 30)).toBe(45);
    expect(millisecondsToFrame(1500, 30)).toBe(45);
    expect(frameToSeconds(45, 30)).toBe(1.5);
    expect(secondsToFrame(0.051, 30, "floor")).toBe(1);
    expect(secondsToFrame(0.051, 30, "ceil")).toBe(2);
  });

  it("clamps seek positions to integer document frames", () => {
    expect(clampFrame(-10, 90)).toBe(0);
    expect(clampFrame(45.6, 90)).toBe(46);
    expect(clampFrame(120, 90)).toBe(90);
    expect(frameFromRatio(0.505, 100)).toBe(51);
  });

  it("formats the same frame value as timeline timecode", () => {
    expect(formatFrameTimecode(1845, 30)).toBe("01:01:15");
  });
});
