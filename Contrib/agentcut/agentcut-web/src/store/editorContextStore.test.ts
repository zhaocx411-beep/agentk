import { beforeEach, describe, expect, it } from "vitest";

import {
  snapshotEditorContext,
  useEditorContextStore,
} from "./editorContextStore";

beforeEach(() => useEditorContextStore.getState().reset());

describe("editorContextStore", () => {
  it("captures an immutable send-time snapshot", () => {
    const state = useEditorContextStore.getState();
    state.setTimelineRevision(7);
    state.setSelectedItemIds(["clip-a"]);
    state.setPlayheadFrame(42);
    state.setSelectedMediaId("media-a");
    state.setSelectedCaptionContext({
      itemId: "caption-a",
      trackId: "cauto-a",
      mediaId: "media-a",
      videoTrackId: "v1",
    });

    const snapshot = snapshotEditorContext();
    state.setSelectedItemIds((ids) => [...ids, "clip-b"]);

    expect(snapshot).toEqual({
      timelineRevision: 7,
      selectedItemIds: ["clip-a"],
      playheadFrame: 42,
      selectedMediaId: "media-a",
      selectedCaptionItemId: "caption-a",
      selectedCaptionTrackId: "cauto-a",
      selectedCaptionMediaId: "media-a",
      selectedCaptionVideoTrackId: "v1",
    });
    expect(useEditorContextStore.getState().selectedItemIds).toEqual([
      "clip-a",
      "clip-b",
    ]);
  });

  it("resets optional selection details", () => {
    const state = useEditorContextStore.getState();
    state.setSelectedCaptionItemId("caption-a");
    state.requestMediaReveal("media-a");
    state.reset();

    expect(snapshotEditorContext()).toEqual({
      timelineRevision: 0,
      selectedItemIds: [],
      playheadFrame: 0,
    });
    expect(useEditorContextStore.getState().mediaRevealRequest).toBeNull();
  });

  it("creates repeatable transient media reveal requests outside snapshots", () => {
    const state = useEditorContextStore.getState();
    state.requestMediaReveal("media-a");
    state.requestMediaReveal("media-a");

    expect(useEditorContextStore.getState().mediaRevealRequest).toEqual({
      mediaId: "media-a",
      nonce: 2,
    });
    expect(snapshotEditorContext()).not.toHaveProperty("mediaRevealRequest");
  });

  it("keeps an immutable Agent prompt context outside send-time snapshots", () => {
    const context = {
      timelineRevision: 3,
      selectedItemIds: ["video"],
      playheadFrame: 18,
    };
    const state = useEditorContextStore.getState();
    state.requestAgentPrompt("调整精确参数", context);
    context.selectedItemIds.push("later");

    expect(useEditorContextStore.getState().agentPromptRequest).toEqual({
      text: "调整精确参数",
      editorContext: {
        timelineRevision: 3,
        selectedItemIds: ["video"],
        playheadFrame: 18,
      },
      nonce: 1,
    });
    expect(snapshotEditorContext()).not.toHaveProperty("agentPromptRequest");
  });
});
