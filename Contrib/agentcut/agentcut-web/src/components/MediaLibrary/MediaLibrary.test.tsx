// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import {
  MEDIA_DND_TYPE,
  TRANSITION_DND_KIND_TYPE,
  TRANSITION_DND_TYPE,
} from "@/lib/dnd";
import { useEditorContextStore } from "@/store/editorContextStore";
import { useProjectStore } from "@/store/projectStore";
import { useTimelineStore } from "@/store/timelineStore";

import { MediaLibrary } from "./MediaLibrary";

beforeEach(() => {
  const doc = newDoc("library", 1920, 1080, 30);
  useTimelineStore.setState({
    doc,
    localDoc: doc,
    confirmedRevision: 1,
    pendingOps: [],
    syncStatus: "synced",
  });
  useProjectStore.setState({
    project: {
      projectId: "p1",
      title: "Library",
      createdAtUnixMs: 1,
    },
  });
  useEditorContextStore.getState().reset();
});

afterEach(() => cleanup());

describe("MediaLibrary transitions", () => {
  it("exposes transition resources with a non-media DnD payload", async () => {
    render(<MediaLibrary />);
    const tab = screen.getByRole("tab", { name: "转场" });
    fireEvent.mouseDown(tab, { button: 0, ctrlKey: false });

    await waitFor(() =>
      expect(screen.getByTestId("transition-resource-crossfade")).toBeTruthy(),
    );
    expect(screen.getByTestId("transition-resource-dipToBlack")).toBeTruthy();
    expect(screen.getByTestId("transition-resource-none")).toBeTruthy();

    const setData = vi.fn();
    fireEvent.dragStart(screen.getByTestId("transition-resource-crossfade"), {
      dataTransfer: {
        effectAllowed: "none",
        setData,
      },
    });

    expect(setData).toHaveBeenCalledWith(
      TRANSITION_DND_TYPE,
      JSON.stringify({ kind: "crossfade" }),
    );
    expect(setData).toHaveBeenCalledWith(
      TRANSITION_DND_KIND_TYPE.crossfade,
      "crossfade",
    );
    expect(setData).not.toHaveBeenCalledWith(MEDIA_DND_TYPE, expect.anything());
  });
});
