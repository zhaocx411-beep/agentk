import { describe, expect, it } from "vitest";

import { newDoc, type Item } from "./types";
import {
  buildPasteOperations,
  buildSelectionDeleteOperation,
  clampFadeFrames,
  clampEditDelta,
  createTimelineClipboard,
  duplicateTargetFrame,
  exceedsPointerThreshold,
  fadeDisabledReason,
  normalizeSelectionRect,
  selectionFromMarquee,
  updateItemSelection,
} from "./editing";

function fixture() {
  const doc = newDoc("editing", 1920, 1080, 30);
  doc.tracks.push({ trackId: "v1", kind: "video" });
  doc.media.push({
    mediaId: "m1",
    kind: "video",
    uri: "/m1.mp4",
    durationInFrames: 100,
  });
  const item: Item = {
    itemId: "i1",
    trackId: "v1",
    kind: "video",
    mediaId: "m1",
    from: 20,
    durationInFrames: 40,
    trimStart: 10,
  };
  doc.items.push(item);
  return { doc, item };
}

describe("timeline editing helpers", () => {
  it("clamps trim handles to one frame and source boundaries", () => {
    const { doc, item } = fixture();
    expect(clampEditDelta(doc, item, "trimStart", -99)).toBe(-10);
    expect(clampEditDelta(doc, item, "trimStart", 99)).toBe(39);
    expect(clampEditDelta(doc, item, "trimEnd", -99)).toBe(-39);
    expect(clampEditDelta(doc, item, "trimEnd", 99)).toBe(50);
  });

  it("toggles additive selection and creates one atomic multi-delete operation", () => {
    let selection = updateItemSelection([], "a", false);
    selection = updateItemSelection(selection, "b", true);
    expect(selection).toEqual(["a", "b"]);
    expect(buildSelectionDeleteOperation(selection)).toEqual({
      op: "removeItems",
      itemIds: ["a", "b"],
    });
    expect(updateItemSelection(selection, "a", true)).toEqual(["b"]);
    expect(buildSelectionDeleteOperation([])).toBeNull();
  });

  it("activates pointer gestures only after the four-pixel threshold", () => {
    expect(exceedsPointerThreshold(10, 10, 14, 10)).toBe(false);
    expect(exceedsPointerThreshold(10, 10, 14.1, 10)).toBe(true);
    expect(exceedsPointerThreshold(0, 0, 3, 4)).toBe(true);
  });

  it("copies relative timing and rejects same-track paste overlap", () => {
    const { doc, item } = fixture();
    doc.items.push({
      ...item,
      itemId: "i2",
      from: 70,
      durationInFrames: 10,
    });
    const clipboard = createTimelineClipboard(doc, ["i1", "i2"]);
    expect(clipboard).not.toBeNull();
    expect(duplicateTargetFrame(clipboard!)).toBe(80);

    let serial = 0;
    const pasted = buildPasteOperations(doc, clipboard!, 100, () => `copy-${++serial}`);
    expect(pasted).toEqual({
      itemIds: ["copy-1", "copy-2"],
      operations: [
        {
          op: "addItem",
          item: expect.objectContaining({ itemId: "copy-1", from: 100, trackId: "v1" }),
        },
        {
          op: "addItem",
          item: expect.objectContaining({ itemId: "copy-2", from: 150, trackId: "v1" }),
        },
      ],
    });

    const overlapping = buildPasteOperations(doc, clipboard!, 30, () => "overlap");
    expect(overlapping).toEqual({
      error: expect.stringContaining("重叠"),
    });
  });

  it("selects intersecting clips with additive marquee semantics", () => {
    const marquee = normalizeSelectionRect(30, 30, 0, 0);
    const candidates = [
      {
        itemId: "a",
        rect: { left: 5, top: 5, right: 15, bottom: 15 },
      },
      {
        itemId: "b",
        rect: { left: 40, top: 5, right: 50, bottom: 15 },
      },
    ];
    expect(selectionFromMarquee([], candidates, marquee, false)).toEqual(["a"]);
    expect(selectionFromMarquee(["b"], candidates, marquee, true)).toEqual([
      "b",
      "a",
    ]);
  });

  it("clamps fades against the opposite edge and defers transition-owned edges", () => {
    const { doc, item } = fixture();
    item.fadeOutFrames = 12;
    expect(clampFadeFrames(item, "fadeInFrames", 99)).toBe(28);
    expect(clampFadeFrames(item, "fadeInFrames", -2)).toBe(0);

    doc.transitions = [
      {
        transitionId: "tr",
        fromItemId: item.itemId,
        toItemId: "next",
        kind: "crossfade",
        frames: 5,
      },
    ];
    expect(fadeDisabledReason(doc, item, "fadeOutFrames")).toBe("该边界由转场控制");
    expect(fadeDisabledReason(doc, item, "fadeInFrames")).toBeNull();
  });
});
