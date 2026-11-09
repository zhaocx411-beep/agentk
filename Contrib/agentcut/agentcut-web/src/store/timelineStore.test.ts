import { beforeEach, describe, expect, it } from "vitest";

import { newDoc } from "@/domain/timeline/types";

import { usePreviewOverridesStore } from "./previewOverridesStore";
import { useTimelineStore } from "./timelineStore";

beforeEach(() => {
  const doc = newDoc("x", 1920, 1080, 30);
  doc.tracks.push({ trackId: "c1", kind: "caption" });
  useTimelineStore.setState({
    doc,
    confirmedRevision: 0,
    pendingOps: [],
    localDoc: doc,
    syncStatus: "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
  usePreviewOverridesStore.getState().reset();
});

describe("timelineStore optimistic queue", () => {
  it("clears temporary M3B overrides when an authoritative snapshot is applied", () => {
    usePreviewOverridesStore.getState().setItemPatch("caption", {
      captionStyle: { bold: true },
    });
    usePreviewOverridesStore.getState().setTrackPatch("a1", {
      ducking: {
        sidechainTrackIds: ["v1"],
        gain: 0.2,
        attackFrames: 3,
        releaseFrames: 5,
      },
    });
    usePreviewOverridesStore.getState().setCropEditingItem("visual");
    const next = newDoc("next", 1920, 1080, 30);

    expect(
      useTimelineStore.getState().applyPollingSnapshot(next, 1),
    ).toBe("applied");
    expect(usePreviewOverridesStore.getState()).toMatchObject({
      itemPatches: {},
      trackPatches: {},
      cropEditingItemId: null,
    });
  });

  it("queues high-level transcript mutations without client-side frame changes", () => {
    const before = useTimelineStore.getState().localDoc;
    const error = useTimelineStore.getState().enqueueBatch({
      requestId: "transcript-delete",
      baseRevision: 0,
      operations: [],
      optimistic: false,
      status: "queued",
    });
    expect(error).toBeNull();
    expect(useTimelineStore.getState().localDoc).toBe(before);
    expect(useTimelineStore.getState().pendingOps).toHaveLength(1);
  });

  it("advances confirmed doc when a batch succeeds instead of snapping back", () => {
    const store = useTimelineStore.getState();
    const error = store.enqueueBatch({
      requestId: "r1",
      baseRevision: 0,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "cap1",
            trackId: "c1",
            kind: "text",
            from: 0,
            durationInFrames: 30,
            text: "hello",
          },
        },
      ],
      status: "queued",
    });
    expect(error).toBeNull();
    expect(useTimelineStore.getState().localDoc?.items).toHaveLength(1);

    useTimelineStore.getState().confirmBatch("r1", 1);
    const confirmed = useTimelineStore.getState();
    expect(confirmed.doc?.items).toHaveLength(1);
    expect(confirmed.localDoc?.items).toHaveLength(1);
    expect(confirmed.pendingOps).toHaveLength(0);
    expect(confirmed.confirmedRevision).toBe(1);
    expect(confirmed.syncStatus).toBe("synced");
  });

  it("replays later optimistic batches over the newly confirmed doc", () => {
    const store = useTimelineStore.getState();
    store.enqueueBatch({
      requestId: "r1",
      baseRevision: 0,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "cap1",
            trackId: "c1",
            kind: "text",
            from: 0,
            durationInFrames: 30,
            text: "one",
          },
        },
      ],
      status: "queued",
    });
    useTimelineStore.getState().enqueueBatch({
      requestId: "r2",
      baseRevision: 0,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "cap2",
            trackId: "c1",
            kind: "text",
            from: 40,
            durationInFrames: 30,
            text: "two",
          },
        },
      ],
      status: "queued",
    });
    useTimelineStore.getState().confirmBatch("r1", 1);
    const state = useTimelineStore.getState();
    expect(state.doc?.items.map((item) => item.itemId)).toEqual(["cap1"]);
    expect(state.localDoc?.items.map((item) => item.itemId)).toEqual(["cap1", "cap2"]);
  });

  it("enters explicit conflict recovery when a queued replay no longer applies", () => {
    useTimelineStore.getState().enqueueBatch({
      requestId: "r1",
      baseRevision: 0,
      operations: [{ op: "setMetadata", metadata: { name: "first" } }],
      status: "sending",
    });
    useTimelineStore.getState().enqueueBatch({
      requestId: "r2",
      baseRevision: 0,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "server-item",
            trackId: "c1",
            kind: "text",
            from: 40,
            durationInFrames: 20,
            text: "local",
          },
        },
      ],
      status: "queued",
    });
    const authoritative = newDoc("server", 1920, 1080, 30);
    authoritative.tracks.push({ trackId: "c1", kind: "caption" });
    authoritative.items.push({
      itemId: "server-item",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 20,
      text: "server",
    });

    const replayError = useTimelineStore
      .getState()
      .confirmBatch("r1", 1, authoritative);

    expect(replayError?.code).toBe("CONFLICTING_ID");
    const state = useTimelineStore.getState();
    expect(state.pendingOps).toHaveLength(0);
    expect(state.syncStatus).toBe("conflict");
    expect(state.doc).toEqual(authoritative);
    expect(state.localDoc).toEqual(authoritative);
  });

  it("ignores stale polling snapshots and defers polling during pending ops or gestures", () => {
    const current = newDoc("current", 1920, 1080, 30);
    current.tracks.push({ trackId: "c1", kind: "caption" });
    useTimelineStore.getState().setConfirmed(current, 5);
    const stale = newDoc("stale", 1920, 1080, 30);
    expect(useTimelineStore.getState().applyPollingSnapshot(stale, 4)).toBe("stale");
    expect(useTimelineStore.getState().doc?.metadata.name).toBe("current");

    useTimelineStore.getState().enqueueBatch({
      requestId: "pending",
      baseRevision: 5,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "cap-pending",
            trackId: "c1",
            kind: "text",
            from: 0,
            durationInFrames: 10,
            text: "pending",
          },
        },
      ],
      status: "queued",
    });
    expect(useTimelineStore.getState().applyPollingSnapshot(stale, 6)).toBe("deferred");
    expect(useTimelineStore.getState().confirmedRevision).toBe(5);

    useTimelineStore.getState().dropPendingOnConflict();
    useTimelineStore.getState().setGestureActive(true);
    expect(useTimelineStore.getState().applyPollingSnapshot(stale, 6)).toBe("deferred");
    expect(useTimelineStore.getState().confirmedRevision).toBe(5);
  });

  it("rebases all pending batches on a newer compatible server document", () => {
    useTimelineStore.getState().enqueueBatch({
      requestId: "local",
      baseRevision: 0,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "local-caption",
            trackId: "c1",
            kind: "text",
            from: 40,
            durationInFrames: 20,
            text: "local",
          },
        },
      ],
      status: "sending",
    });
    const fresh = newDoc("server", 1920, 1080, 30);
    fresh.tracks.push({ trackId: "c1", kind: "caption" });
    fresh.items.push({
      itemId: "server-caption",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 20,
      text: "server",
    });
    expect(useTimelineStore.getState().rebasePending(fresh, 9)).toEqual({
      applied: true,
      stale: false,
      error: null,
    });
    const state = useTimelineStore.getState();
    expect(state.confirmedRevision).toBe(9);
    expect(state.doc?.items.map((item) => item.itemId)).toEqual(["server-caption"]);
    expect(state.localDoc?.items.map((item) => item.itemId)).toEqual([
      "server-caption",
      "local-caption",
    ]);
  });

  it("leaves local state untouched when rebase hits a domain conflict", () => {
    useTimelineStore.getState().enqueueBatch({
      requestId: "local",
      baseRevision: 0,
      operations: [
        {
          op: "addItem",
          item: {
            itemId: "same-id",
            trackId: "c1",
            kind: "text",
            from: 40,
            durationInFrames: 20,
            text: "local",
          },
        },
      ],
      status: "sending",
    });
    const fresh = newDoc("server", 1920, 1080, 30);
    fresh.tracks.push({ trackId: "c1", kind: "caption" });
    fresh.items.push({
      itemId: "same-id",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 20,
      text: "server",
    });
    const before = useTimelineStore.getState().localDoc;
    const result = useTimelineStore.getState().rebasePending(fresh, 9);
    expect(result.applied).toBe(false);
    expect(result.error?.code).toBe("CONFLICTING_ID");
    expect(useTimelineStore.getState().confirmedRevision).toBe(0);
    expect(useTimelineStore.getState().localDoc).toBe(before);
  });
});
