import { describe, expect, it } from "vitest";

import type { TranscriptEntry } from "@/remote";

import {
  activeTranscriptOccurrence,
  estimatedSelectedFrames,
  occurrenceRange,
  selectedTranscriptRanges,
} from "./transcriptView";

function entry(id: string, fromFrame: number, toFrame: number): TranscriptEntry {
  return {
    occurrenceId: id,
    itemId: "item",
    trackId: "v1",
    mediaId: "m1",
    mediaGeneration: 1,
    segmentId: "segment",
    wordId: id,
    text: id,
    sourceStartMs: fromFrame,
    sourceEndMs: toFrame,
    fromFrame,
    toFrame,
    granularity: "word",
  };
}

describe("transcript view selection", () => {
  const entries = [
    entry("a", 0, 10),
    entry("b", 10, 20),
    entry("c", 50, 60),
    entry("d", 60, 70),
  ];

  it("keeps non-contiguous selections as separate occurrence ranges", () => {
    expect(selectedTranscriptRanges(entries, new Set(["a", "b", "d"]))).toEqual([
      { fromOccurrenceId: "a", toOccurrenceId: "b" },
      { fromOccurrenceId: "d", toOccurrenceId: "d" },
    ]);
  });

  it("builds stable shift ranges and merges overlap only for duration display", () => {
    expect([...occurrenceRange(entries, "d", "b")]).toEqual(["b", "c", "d"]);
    expect(estimatedSelectedFrames(entries, new Set(["a", "b", "c"]))).toBe(30);
  });

  it("finds the current occurrence from server-provided frame bounds", () => {
    expect(activeTranscriptOccurrence(entries, 9)).toBe("a");
    expect(activeTranscriptOccurrence(entries, 10)).toBe("b");
    expect(activeTranscriptOccurrence(entries, 35)).toBeNull();
  });
});
