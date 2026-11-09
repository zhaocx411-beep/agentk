import { describe, expect, it } from "vitest";

import { applyOps } from "@/domain/timeline/applyOps";
import { newDoc } from "@/domain/timeline/types";
import type { Operation } from "@/domain/timeline/types";

import { invertOperations } from "./historyOps";

describe("undo inverse operations", () => {
  it("restores move, trim and item property edits", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.tracks.push({ trackId: "c1", kind: "caption" });
    before.items.push({
      itemId: "cap1",
      trackId: "c1",
      kind: "text",
      from: 10,
      durationInFrames: 30,
      text: "old",
    });
    const forward: Operation[] = [
      { op: "moveItem", itemId: "cap1", from: 50 },
      { op: "trimEnd", itemId: "cap1", frame: 100 },
      { op: "updateItem", itemId: "cap1", patch: { text: "new" } },
    ];
    const changed = applyOps(before, [...forward]).doc;
    const restored = applyOps(changed, invertOperations(before, [...forward])).doc;
    expect(restored.items[0]).toMatchObject({
      from: 10,
      durationInFrames: 30,
      text: "old",
    });
  });

  it("restores a media rename, including back to the unset state", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.media.push({ mediaId: "m1", uri: "blob://m1", kind: "video", durationInFrames: 180 });
    const forward: Operation[] = [
      { op: "updateMedia", mediaId: "m1", mediaPatch: { displayName: "开场镜头" } },
    ];
    const changed = applyOps(before, [...forward]).doc;
    expect(changed.media[0].displayName).toBe("开场镜头");
    const restored = applyOps(changed, invertOperations(before, [...forward])).doc;
    expect(restored.media[0].displayName).toBeFalsy();
  });

  it("restores a removed track and its clips", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.tracks.push({ trackId: "c1", kind: "caption" });
    before.items.push({
      itemId: "cap1",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 30,
      text: "caption",
    });
    const forward = [{ op: "removeTrack", trackId: "c1" }] as const;
    const changed = applyOps(before, [...forward]).doc;
    const restored = applyOps(changed, invertOperations(before, [...forward])).doc;
    expect(restored.tracks).toEqual(before.tracks);
    expect(restored.items).toEqual(before.items);
  });

  it("restores M3A item properties", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.media.push({ mediaId: "m1", uri: "blob://m1", kind: "video", durationInFrames: 180 });
    before.tracks.push({ trackId: "v1", kind: "video" });
    before.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
    });
    const forward: Operation[] = [
      {
        op: "updateItem",
        itemId: "v",
        patch: {
          speed: 2,
          volume: 1.5,
          muted: true,
          fadeInFrames: 5,
          fadeOutFrames: 10,
          transform: { x: 0.55, y: 0.05, width: 0.4, height: 0.4, opacity: 0.7, fit: "cover" },
        },
      },
    ];
    const changed = applyOps(before, [...forward]).doc;
    const restored = applyOps(changed, invertOperations(before, [...forward])).doc;
    expect(restored.items[0]).toMatchObject({
      speed: 1,
      volume: 1,
      muted: false,
      fadeInFrames: 0,
      fadeOutFrames: 0,
      transform: { x: 0, y: 0, width: 1, height: 1, opacity: 1, fit: "contain" },
    });
  });

  it("restores video visibility independently from track mute", () => {
    const before = newDoc("visibility", 1920, 1080, 30);
    before.tracks.push({ trackId: "v1", kind: "video", muted: true });
    const forward: Operation[] = [
      {
        op: "updateTrack",
        trackId: "v1",
        trackPatch: { hidden: true, muted: false },
      },
    ];

    const changed = applyOps(before, forward).doc;
    expect(changed.tracks[0]).toMatchObject({ hidden: true, muted: false });
    const restored = applyOps(changed, invertOperations(before, forward)).doc;
    expect(restored.tracks[0]).toMatchObject({ hidden: false, muted: true });
  });

  it("restores transition CRUD and geometry cascades", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.media.push({
      mediaId: "m1",
      uri: "blob://m1",
      kind: "video",
      durationInFrames: 300,
    });
    before.tracks.push({ trackId: "v1", kind: "video" });
    before.items.push(
      {
        itemId: "out",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 100,
      },
      {
        itemId: "in",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 100,
        durationInFrames: 60,
        trimStart: 100,
      },
    );
    before.transitions = [
      {
        transitionId: "tr",
        fromItemId: "out",
        toItemId: "in",
        kind: "crossfade",
        frames: 10,
      },
    ];

    const geometry = [{ op: "trimEnd", itemId: "out", frame: 90 }] as const;
    const changed = applyOps(before, [...geometry]).doc;
    expect(changed.transitions).toHaveLength(0);
    const restored = applyOps(
      changed,
      invertOperations(before, [...geometry]),
    ).doc;
    expect(restored.items).toMatchObject(before.items);
    expect(restored.transitions).toEqual(before.transitions);

    const batchMove = [
      { op: "moveItem", itemId: "out", from: 10 },
      { op: "moveItem", itemId: "in", from: 110 },
    ] as const;
    const moved = applyOps(before, [...batchMove]).doc;
    const moveRestored = applyOps(
      moved,
      invertOperations(before, [...batchMove]),
    ).doc;
    expect(moveRestored.items).toMatchObject(before.items);
    expect(moveRestored.transitions).toEqual(before.transitions);

    const updatedOps = [
      {
        op: "updateTransition",
        transitionId: "tr",
        transitionPatch: { kind: "dipToBlack", frames: 20 },
      },
    ] as const;
    const updated = applyOps(before, [...updatedOps]).doc;
    expect(applyOps(updated, invertOperations(before, [...updatedOps])).doc.transitions)
      .toEqual(before.transitions);

    const removedOps = [{ op: "removeTransition", transitionId: "tr" }] as const;
    const removed = applyOps(before, [...removedOps]).doc;
    expect(applyOps(removed, invertOperations(before, [...removedOps])).doc.transitions)
      .toEqual(before.transitions);

    const withoutTransition = structuredClone(before);
    withoutTransition.transitions = [];
    const addOps = [
      { op: "addTransition", transition: structuredClone(before.transitions![0]) },
    ] as const;
    const added = applyOps(withoutTransition, [...addOps]).doc;
    expect(
      applyOps(added, invertOperations(withoutTransition, [...addOps])).doc
        .transitions,
    ).toHaveLength(0);
  });

  it("restores track-removal ducking cleanup and cascaded transitions", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.media.push({
      mediaId: "m1",
      uri: "blob://m1",
      kind: "video",
      durationInFrames: 300,
    });
    before.tracks.push(
      { trackId: "v1", kind: "video" },
      {
        trackId: "a1",
        kind: "audio",
        ducking: {
          sidechainTrackIds: ["v1"],
          gain: 0.25,
          attackFrames: 10,
          releaseFrames: 20,
        },
      },
    );
    before.items.push(
      {
        itemId: "out",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 100,
      },
      {
        itemId: "in",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 100,
        durationInFrames: 60,
        trimStart: 100,
      },
    );
    before.transitions = [
      {
        transitionId: "tr",
        fromItemId: "out",
        toItemId: "in",
        kind: "crossfade",
        frames: 10,
      },
    ];
    const forward = [{ op: "removeTrack", trackId: "v1" }] as const;
    const changed = applyOps(before, [...forward]).doc;
    expect(changed.tracks[0].ducking).toBeUndefined();
    const restored = applyOps(
      changed,
      invertOperations(before, [...forward]),
    ).doc;
    expect(restored.tracks).toEqual(before.tracks);
    expect(restored.items).toEqual(before.items);
    expect(restored.transitions).toEqual(before.transitions);
  });

  it("restores ducking, caption-style merges, and setCaptions styles", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "a1", kind: "audio" },
      { trackId: "c1", kind: "caption" },
    );
    before.items.push({
      itemId: "cap",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 20,
      text: "old",
      captionStyle: { fontFamily: "mono", bold: false },
    });
    const forward: Operation[] = [
      {
        op: "updateTrack",
        trackId: "a1",
        trackPatch: {
          ducking: {
            sidechainTrackIds: ["v1"],
            gain: 0.5,
            attackFrames: 5,
            releaseFrames: 5,
          },
        },
      },
      {
        op: "updateItem",
        itemId: "cap",
        patch: { captionStyle: { bold: true, textColor: "#FF0000" } },
      },
    ];
    const changed = applyOps(before, [...forward]).doc;
    const restored = applyOps(
      changed,
      invertOperations(before, [...forward]),
    ).doc;
    expect(restored.tracks[1].ducking).toBeUndefined();
    expect(restored.items[0].captionStyle).toEqual(before.items[0].captionStyle);

    const replace: Operation[] = [
      {
        op: "setCaptions",
        trackId: "c1",
        captions: [
          {
            from: 0,
            durationInFrames: 20,
            text: "new",
            captionStyle: { fontFamily: "serif" },
          },
        ],
      },
    ];
    const replaced = applyOps(before, [...replace]).doc;
    const captionsRestored = applyOps(
      replaced,
      invertOperations(before, [...replace]),
    ).doc;
    expect(captionsRestored.items[0].captionStyle)
      .toEqual(before.items[0].captionStyle);
  });

  it("restores crop and rotation transform fields", () => {
    const before = newDoc("x", 1920, 1080, 30);
    before.media.push({
      mediaId: "m1",
      uri: "blob://m1",
      kind: "video",
      durationInFrames: 180,
    });
    before.tracks.push({ trackId: "v1", kind: "video" });
    before.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
      transform: {
        x: 0,
        y: 0,
        width: 1,
        height: 1,
        fit: "cover",
        rotationDegrees: -30,
        crop: { x: 0.1, y: 0.2, width: 0.7, height: 0.6 },
      },
    });
    const forward = [
      {
        op: "updateItem",
        itemId: "v",
        patch: {
          transform: {
            x: 0,
            y: 0,
            width: 1,
            height: 1,
            fit: "contain",
            rotationDegrees: 45,
            crop: { x: 0.2, y: 0.1, width: 0.5, height: 0.8 },
          },
        },
      },
    ] as const;
    const changed = applyOps(before, [...forward]).doc;
    const restored = applyOps(
      changed,
      invertOperations(before, [...forward]),
    ).doc;
    expect(restored.items[0].transform).toEqual(before.items[0].transform);
  });
});
