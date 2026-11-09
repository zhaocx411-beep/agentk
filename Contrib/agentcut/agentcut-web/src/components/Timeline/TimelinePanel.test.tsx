// @vitest-environment jsdom

import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import {
  TRANSITION_DND_KIND_TYPE,
  TRANSITION_DND_TYPE,
  type TransitionDragPayload,
} from "@/lib/dnd";
import { useEditorContextStore } from "@/store/editorContextStore";
import { usePlaybackStore } from "@/store/playbackStore";
import { useProjectStore } from "@/store/projectStore";
import { usePreviewOverridesStore } from "@/store/previewOverridesStore";
import { useTimelineStore } from "@/store/timelineStore";
import { useSequenceStore } from "@/store/sequenceStore";

const submitOpsMock = vi.fn();
const separateAudioMock = vi.fn();
const createSequenceMock = vi.fn();
const switchSequenceMock = vi.fn();
const renameSequenceMock = vi.fn();
const deleteSequenceMock = vi.fn();
vi.mock("@/store/submitOps", () => ({
  submitOps: (...args: unknown[]) => submitOpsMock(...args),
}));
vi.mock("@/store/separateAudio", () => ({
  separateAudioItem: (...args: unknown[]) => separateAudioMock(...args),
}));
vi.mock("@/store/sequenceActions", () => ({
  createSequence: (...args: unknown[]) => createSequenceMock(...args),
  switchSequence: (...args: unknown[]) => switchSequenceMock(...args),
  renameSequence: (...args: unknown[]) => renameSequenceMock(...args),
  deleteSequence: (...args: unknown[]) => deleteSequenceMock(...args),
}));

import {
  calculateTrackVerticalLayout,
  TimelinePanel,
} from "./TimelinePanel";

function transitionDataTransfer(
  kind: TransitionDragPayload["kind"],
): DataTransfer {
  return {
    types: [TRANSITION_DND_TYPE, TRANSITION_DND_KIND_TYPE[kind]],
    dropEffect: "none",
    effectAllowed: "copy",
    getData: (type: string) =>
      type === TRANSITION_DND_TYPE ? JSON.stringify({ kind }) : "",
  } as unknown as DataTransfer;
}

beforeEach(() => {
  submitOpsMock.mockReset();
  submitOpsMock.mockResolvedValue(null);
  separateAudioMock.mockReset();
  separateAudioMock.mockResolvedValue(null);
  for (const mock of [
    createSequenceMock,
    switchSequenceMock,
    renameSequenceMock,
    deleteSequenceMock,
  ]) {
    mock.mockReset();
    mock.mockResolvedValue(null);
  }
  const doc = newDoc("test", 1920, 1080, 30);
  doc.media.push({
    mediaId: "source",
    uri: "blob://source",
    kind: "video",
    durationInFrames: 120,
  });
  doc.tracks.push(
    { trackId: "v1", kind: "video" },
    { trackId: "c1", kind: "caption" },
  );
  doc.items.push(
    {
      itemId: "source-video",
      trackId: "v1",
      kind: "video",
      mediaId: "source",
      from: 0,
      durationInFrames: 60,
    },
    {
      itemId: "a",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 20,
      text: "A",
    },
    {
      itemId: "b",
      trackId: "c1",
      kind: "text",
      from: 30,
      durationInFrames: 20,
      text: "B",
    },
  );
  useTimelineStore.setState({
    doc,
    confirmedRevision: 1,
    pendingOps: [],
    localDoc: doc,
    syncStatus: "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
  useProjectStore.setState({
    project: {
      projectId: "p1",
      title: "Project",
      createdAtUnixMs: 1,
    },
  });
  usePlaybackStore.setState({ currentFrame: 0, isPlaying: false });
  useSequenceStore.setState({
    activeTimelineId: "tl_1",
    timelines: [{
      timelineId: "tl_1",
      name: "剪辑 1",
      revision: 1,
      durationInFrames: 60,
      currentFrame: 0,
      zoom: 1,
      createdAtUnixMs: 1,
      updatedAtUnixMs: 1,
    }],
    zoom: 1,
  });
  useEditorContextStore.getState().reset();
  usePreviewOverridesStore.getState().reset();
});

afterEach(() => cleanup());

describe("track vertical layout", () => {
  it("centers the V/A divider without creating overflow for sparse tracks", () => {
    expect(calculateTrackVerticalLayout(300, 44, 0)).toEqual({
      contentHeight: 300,
      dividerY: 150,
      initialScrollTop: 0,
      overflow: false,
      topOffset: 106,
    });
    expect(calculateTrackVerticalLayout(300, 88, 88)).toEqual({
      contentHeight: 300,
      dividerY: 150,
      initialScrollTop: 0,
      overflow: false,
      topOffset: 62,
    });
    expect(calculateTrackVerticalLayout(300, 36 + 94, 0)).toEqual({
      contentHeight: 300,
      dividerY: 150,
      initialScrollTop: 0,
      overflow: false,
      topOffset: 20,
    });
  });

  it("uses only real track height when tracks overflow", () => {
    expect(calculateTrackVerticalLayout(180, 200, 100)).toEqual({
      contentHeight: 300,
      dividerY: 200,
      initialScrollTop: 110,
      overflow: true,
      topOffset: 0,
    });
  });
});

describe("TimelinePanel interactions", () => {
  it("extends the playhead through all vertically overflowing tracks", async () => {
    const doc = useTimelineStore.getState().localDoc!;
    doc.media.push(
      {
        mediaId: "audio-1",
        uri: "blob://audio-1",
        kind: "audio",
        durationInFrames: 60,
        waveformPeaks: [0.2, 0.5],
      },
      {
        mediaId: "audio-2",
        uri: "blob://audio-2",
        kind: "audio",
        durationInFrames: 60,
        waveformPeaks: [0.3, 0.6],
      },
    );
    doc.tracks.push(
      { trackId: "a1", kind: "audio" },
      { trackId: "a2", kind: "audio" },
    );
    doc.items.push(
      {
        itemId: "audio-item-1",
        trackId: "a1",
        kind: "audio",
        mediaId: "audio-1",
        from: 0,
        durationInFrames: 60,
      },
      {
        itemId: "audio-item-2",
        trackId: "a2",
        kind: "audio",
        mediaId: "audio-2",
        from: 0,
        durationInFrames: 60,
      },
    );
    useTimelineStore.setState({ doc, localDoc: doc });

    const clientHeightDescriptor = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "clientHeight",
    );
    Object.defineProperty(HTMLElement.prototype, "clientHeight", {
      configurable: true,
      get() {
        return this.hasAttribute("data-track-overflow") ? 212 : 0;
      },
    });
    try {
      render(<TimelinePanel />);

      await waitFor(() =>
        expect(screen.getByTestId("timeline-playhead").style.height).toBe("264px"),
      );
    } finally {
      if (clientHeightDescriptor) {
        Object.defineProperty(
          HTMLElement.prototype,
          "clientHeight",
          clientHeightDescriptor,
        );
      } else {
        Reflect.deleteProperty(HTMLElement.prototype, "clientHeight");
      }
    }
  });

  it("synchronizes revision, selection and playhead into editor context", async () => {
    render(<TimelinePanel />);

    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
      ctrlKey: true,
    });
    act(() => usePlaybackStore.getState().setCurrentFrame(12));

    await waitFor(() =>
      expect(useEditorContextStore.getState()).toMatchObject({
        timelineRevision: 1,
        selectedItemIds: ["a"],
        selectedCaptionItemId: "a",
        selectedCaptionTrackId: "c1",
        selectedCaptionVideoTrackId: "v1",
        playheadFrame: 12,
      }),
    );
  });

  it("multi-selects clips and submits one removeItems operation", async () => {
    render(<TimelinePanel />);

    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
      ctrlKey: true,
    });
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-b"), {
      button: 0,
      ctrlKey: true,
    });
    expect(screen.getByTestId("timeline-clip-a").getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(screen.getByTestId("timeline-clip-b").getAttribute("aria-pressed")).toBe(
      "true",
    );

    fireEvent.keyDown(window, { key: "Delete" });
    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        { op: "removeItems", itemIds: ["a", "b"] },
      ]),
    );
  });

  it("uses Space for playback and Escape to clear selection", () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
      metaKey: true,
    });

    fireEvent.keyDown(window, { key: " " });
    expect(usePlaybackStore.getState().isPlaying).toBe(true);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.getByTestId("timeline-clip-a").getAttribute("aria-pressed")).toBe(
      "false",
    );
  });

  it("rewinds before Space starts playback at the timeline end", () => {
    usePlaybackStore.setState({ currentFrame: 60, isPlaying: false });
    render(<TimelinePanel />);

    fireEvent.keyDown(window, { key: " " });

    expect(usePlaybackStore.getState().currentFrame).toBe(0);
    expect(usePlaybackStore.getState().isPlaying).toBe(true);
  });

  it("keeps caption-track clips out of cross-track drag and trim", () => {
    render(<TimelinePanel />);
    expect(screen.getByTestId("track-header-c1")).toBeTruthy();
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
      clientX: 100,
      clientY: 20,
    });
    fireEvent.pointerMove(window, { clientX: 130, clientY: 80 });
    fireEvent.pointerUp(window, { clientX: 130, clientY: 80 });

    expect(submitOpsMock).not.toHaveBeenCalled();
    expect(screen.queryByTestId("trim-start-a")).toBeNull();
  });

  it("keeps a click as selection until movement exceeds the drag threshold", () => {
    render(<TimelinePanel />);

    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
      clientX: 100,
      clientY: 20,
    });
    expect(screen.getByTestId("timeline-clip-a").getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(useTimelineStore.getState().gestureActive).toBe(false);
    fireEvent.pointerMove(window, { clientX: 104, clientY: 20 });
    fireEvent.pointerUp(window, { clientX: 104, clientY: 20 });

    expect(submitOpsMock).not.toHaveBeenCalled();
    expect(useTimelineStore.getState().gestureActive).toBe(false);
  });

  it("copies and pastes multiple clips at the playhead with relative timing", async () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
      ctrlKey: true,
    });
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-b"), {
      button: 0,
      ctrlKey: true,
    });
    act(() => usePlaybackStore.getState().setCurrentFrame(60));

    fireEvent.keyDown(window, { key: "c", ctrlKey: true });
    fireEvent.keyDown(window, { key: "v", ctrlKey: true });

    await waitFor(() => expect(submitOpsMock).toHaveBeenCalledTimes(1));
    const operations = submitOpsMock.mock.calls[0][1];
    expect(operations).toEqual([
      {
        op: "addItem",
        item: expect.objectContaining({
          itemId: expect.not.stringMatching(/^(a|b)$/),
          from: 60,
          trackId: "c1",
          text: "A",
        }),
      },
      {
        op: "addItem",
        item: expect.objectContaining({
          itemId: expect.not.stringMatching(/^(a|b)$/),
          from: 90,
          trackId: "c1",
          text: "B",
        }),
      },
    ]);
  });

  it("reports a same-track overlap instead of submitting paste", () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
    });
    act(() => usePlaybackStore.getState().setCurrentFrame(10));

    fireEvent.keyDown(window, { key: "c", metaKey: true });
    fireEvent.keyDown(window, { key: "v", metaKey: true });

    expect(submitOpsMock).not.toHaveBeenCalled();
    expect(screen.getByText(/粘贴位置.*重叠/)).toBeTruthy();
  });

  it("does not hijack native copy for selected Agent reply text", () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), {
      button: 0,
    });
    const chat = document.createElement("div");
    chat.dataset.agentChat = "";
    chat.textContent = "需要复制的 Agent 回复";
    document.body.append(chat);
    const range = document.createRange();
    range.selectNodeContents(chat);
    const selection = document.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);

    const event = new KeyboardEvent("keydown", {
      key: "c",
      metaKey: true,
      bubbles: true,
      cancelable: true,
    });
    chat.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(false);
    selection.removeAllRanges();
    chat.remove();
  });

  it("duplicates the selection after its original range", async () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-b"), { button: 0 });

    fireEvent.keyDown(window, { key: "d", ctrlKey: true });

    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        {
          op: "addItem",
          item: expect.objectContaining({
            itemId: expect.not.stringMatching(/^b$/),
            from: 50,
            trackId: "c1",
            text: "B",
          }),
        },
      ]),
    );
  });

  it("marquee-selects intersecting clips without seeking", () => {
    render(<TimelinePanel />);
    const lane = document.querySelector<HTMLElement>('[data-track-id="v1"]')!;
    const lanes = screen.getByTestId("timeline-lanes");
    const clipA = screen.getByTestId("timeline-clip-a");
    const clipB = screen.getByTestId("timeline-clip-b");
    Object.defineProperty(lane, "getBoundingClientRect", {
      configurable: true,
      value: () => ({ left: 0, top: 0, right: 200, bottom: 60, width: 200, height: 60 }),
    });
    Object.defineProperty(lanes, "getBoundingClientRect", {
      configurable: true,
      value: () => ({ left: 0, top: 0, right: 200, bottom: 60, width: 200, height: 60 }),
    });
    Object.defineProperty(clipA, "getBoundingClientRect", {
      configurable: true,
      value: () => ({ left: 5, top: 5, right: 25, bottom: 25, width: 20, height: 20 }),
    });
    Object.defineProperty(clipB, "getBoundingClientRect", {
      configurable: true,
      value: () => ({ left: 50, top: 5, right: 70, bottom: 25, width: 20, height: 20 }),
    });

    fireEvent.pointerDown(lane, { button: 0, clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 30, clientY: 30 });
    expect(screen.getByTestId("timeline-marquee")).toBeTruthy();
    expect(clipA.getAttribute("aria-pressed")).toBe("true");
    expect(clipB.getAttribute("aria-pressed")).toBe("false");
    expect(usePlaybackStore.getState().currentFrame).toBe(0);
    fireEvent.pointerUp(window, { clientX: 30, clientY: 30 });
    expect(screen.queryByTestId("timeline-marquee")).toBeNull();
    expect(useTimelineStore.getState().gestureActive).toBe(false);
  });

  it("shows typed caption context actions", async () => {
    render(<TimelinePanel />);

    fireEvent.contextMenu(screen.getByTestId("timeline-clip-a"));

    expect(await screen.findByRole("menuitem", { name: "编辑字幕" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: /复制/ })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "删除" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "分离音频" })).toBeNull();
  });

  it("renders one caption track header above V1 without a child lane", () => {
    render(<TimelinePanel />);

    const captionHeader = screen.getByTestId("track-header-c1");
    const videoHeader = screen.getByTestId("track-header-v1");
    expect(captionHeader.textContent).toContain("C1");
    expect(captionHeader.textContent).toContain("字幕轨");
    expect(captionHeader.parentElement?.style.height).toBe("36px");
    expect(videoHeader.style.width).toBe("208px");
    expect(videoHeader.parentElement?.dataset.trackLayout).toBe("video");
    expect(captionHeader.compareDocumentPosition(videoHeader) & Node.DOCUMENT_POSITION_FOLLOWING)
      .toBeTruthy();
    expect(screen.queryByTestId("caption-sublane-v1")).toBeNull();
    expect(screen.getByTestId("timeline-clip-a")).toBeTruthy();
    expect(screen.getByTestId("timeline-clip-a").textContent).not.toContain("CC");
  });

  it("keeps the video header aligned with a three-layer embedded-audio clip", () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      setTransform: vi.fn(),
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      fillStyle: "",
    } as unknown as CanvasRenderingContext2D);
    const doc = useTimelineStore.getState().localDoc!;
    doc.media[0] = {
      ...doc.media[0],
      displayName: "带原声视频",
      thumbnailUri: "/thumbnail.jpg",
      probe: { hasAudio: true },
      waveformPeaks: [0.1, 0.7, 0.3, 0.9],
    };
    useTimelineStore.setState({ doc, localDoc: doc });

    render(<TimelinePanel />);

    const videoHeader = screen.getByTestId("track-header-v1");
    expect(videoHeader.parentElement?.style.height).toBe("94px");
    expect(videoHeader.parentElement?.dataset.trackLayout).toBe(
      "video-with-embedded-audio",
    );
    expect(screen.getByTestId("video-embedded-waveform")).toBeTruthy();
    expect(screen.getByText("带原声视频").parentElement?.style.bottom).toBe(
      "28px",
    );
  });

  it("creates a real sequence from the timeline tab bar", async () => {
    render(<TimelinePanel />);

    fireEvent.click(screen.getByRole("button", { name: "新建时间线" }));

    await waitFor(() =>
      expect(createSequenceMock).toHaveBeenCalledWith("p1"),
    );
    expect(screen.queryByRole("button", { name: "新增轨道" })).toBeNull();
  });

  it("renames the active sequence from its tab", async () => {
    useSequenceStore.setState({
      activeTimelineId: "tl_2",
      timelines: [
        ...useSequenceStore.getState().timelines,
        {
          timelineId: "tl_2",
          name: "剪辑 2",
          revision: 0,
          durationInFrames: 0,
          currentFrame: 0,
          zoom: 1,
          createdAtUnixMs: 2,
          updatedAtUnixMs: 2,
        },
      ],
    });
    render(<TimelinePanel />);

    fireEvent.doubleClick(screen.getByRole("button", { name: "切换到 剪辑 2" }));
    const input = screen.getByRole("textbox", { name: "重命名 剪辑 2" });
    fireEvent.change(input, { target: { value: "导演版" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() =>
      expect(renameSequenceMock).toHaveBeenCalledWith("p1", "tl_2", "导演版"),
    );
  });

  it("does not allocate a caption sublane when the video has no captions", () => {
    const current = useTimelineStore.getState().localDoc!;
    const doc = {
      ...current,
      items: current.items.filter((item) => item.kind !== "text"),
    };
    useTimelineStore.setState({ doc, localDoc: doc });

    render(<TimelinePanel />);

    expect(screen.queryByTestId("caption-sublane-v1")).toBeNull();
  });

  it("uses V-track CC to semantically mute linked caption tracks", async () => {
    render(<TimelinePanel />);

    fireEvent.pointerDown(
      screen.getByRole("button", { name: "V1 更多操作" }),
      { button: 0, ctrlKey: false },
    );
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "隐藏字幕" }),
    );

    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        {
          op: "updateTrack",
          trackId: "c1",
          trackPatch: { muted: true },
        },
      ]),
    );
  });

  it("shows video context actions and disables separation without probed audio", async () => {
    const doc = newDoc("menu", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 120,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "video",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    usePlaybackStore.setState({ currentFrame: 15 });
    render(<TimelinePanel />);

    fireEvent.contextMenu(screen.getByTestId("timeline-clip-video"));

    expect(await screen.findByRole("menuitem", { name: "分割" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: /复制/ })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: /粘贴/ })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "删除" })).toBeTruthy();
    const reveal = screen.getByRole("menuitem", { name: "在素材中查找" });
    expect(reveal).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "进入画面裁剪" })).toBeTruthy();
    expect(
      screen
        .getByRole("menuitem", { name: "分离音频" })
        .getAttribute("aria-disabled"),
    ).toBe("true");
    fireEvent.click(reveal);
    expect(useEditorContextStore.getState().mediaRevealRequest?.mediaId).toBe(
      "m1",
    );

    fireEvent.contextMenu(screen.getByTestId("timeline-clip-video"));
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "进入画面裁剪" }),
    );
    expect(usePreviewOverridesStore.getState().cropEditingItemId).toBe("video");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("enables audio separation only for an unlocked video with audio and shows progress", async () => {
    const doc = newDoc("menu", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 120,
      probe: { hasAudio: true, durationMs: 4000 },
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "video",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    separateAudioMock.mockImplementation(
      async (
        _projectId: string,
        _itemId: string,
        onPhase: (phase: string, message?: string) => void,
      ) => {
        onPhase("loading");
        await Promise.resolve();
        onPhase("success", "音频分离完成");
        return null;
      },
    );
    render(<TimelinePanel />);

    fireEvent.contextMenu(screen.getByTestId("timeline-clip-video"));
    const separate = await screen.findByRole("menuitem", { name: "分离音频" });
    expect(separate.getAttribute("aria-disabled")).not.toBe("true");
    fireEvent.click(separate);
    await waitFor(() =>
      expect(separateAudioMock).toHaveBeenCalledWith(
        "p1",
        "video",
        expect.any(Function),
      ),
    );
    fireEvent.contextMenu(screen.getByTestId("timeline-clip-video"));
    expect(await screen.findByRole("menuitem", { name: "音频已分离" })).toBeTruthy();
  });

  it("previews fade dragging and commits one updateItem on pointer-up", async () => {
    const doc = newDoc("fade", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 120,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "video",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);

    fireEvent.pointerDown(screen.getByTestId("fade-handle-in-video"), {
      button: 0,
      clientX: 10,
    });
    fireEvent.pointerMove(window, { clientX: 25 });
    expect(
      usePreviewOverridesStore.getState().itemPatches.video?.fadeInFrames,
    ).toBe(15);
    expect(screen.getByTestId("fade-tooltip-video").textContent).toContain(
      "淡入: 0.5s",
    );
    expect(submitOpsMock).not.toHaveBeenCalled();
    fireEvent.pointerUp(window, { clientX: 25 });
    fireEvent.pointerUp(window, { clientX: 30 });

    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        {
          op: "updateItem",
          itemId: "video",
          patch: { fadeInFrames: 15 },
        },
      ]),
    );
    expect(submitOpsMock).toHaveBeenCalledTimes(1);
    expect(usePreviewOverridesStore.getState().itemPatches.video).toBeUndefined();
  });

  it("protects focused text inputs from timeline shortcuts", () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), { button: 0 });
    const input = document.createElement("input");
    document.body.append(input);
    input.focus();

    fireEvent.keyDown(input, { key: "Delete" });
    fireEvent.keyDown(input, { key: "d", ctrlKey: true });
    fireEvent.keyDown(input, { key: " " });

    expect(submitOpsMock).not.toHaveBeenCalled();
    expect(usePlaybackStore.getState().isPlaying).toBe(false);
    expect(screen.getByTestId("timeline-clip-a").getAttribute("aria-pressed")).toBe(
      "true",
    );
    input.remove();
  });

  it("protects an open caption dialog from timeline delete", () => {
    render(<TimelinePanel />);
    fireEvent.pointerDown(screen.getByTestId("timeline-clip-a"), { button: 0 });
    fireEvent.doubleClick(screen.getByTestId("timeline-clip-a"));
    const dialog = screen.getByRole("dialog");

    fireEvent.keyDown(dialog, { key: "Delete" });

    expect(submitOpsMock).not.toHaveBeenCalled();
    expect(screen.getByTestId("timeline-clip-a").getAttribute("aria-pressed")).toBe(
      "true",
    );
  });

  it.each(["video", "audio", "image"] as const)(
    "does nothing when a %s clip is double-clicked",
    (kind) => {
      const doc = newDoc("test", 1920, 1080, 30);
      doc.media.push({
        mediaId: "m1",
        uri: "blob://media",
        kind,
        durationInFrames: kind === "image" ? 0 : 90,
      });
      doc.tracks.push({
        trackId: "t1",
        kind: kind === "audio" ? "audio" : "video",
      });
      doc.items.push({
        itemId: "media",
        trackId: "t1",
        kind,
        mediaId: "m1",
        from: 0,
        durationInFrames: 30,
      });
      useTimelineStore.setState({ doc, localDoc: doc });
      render(<TimelinePanel />);

      fireEvent.doubleClick(screen.getByTestId("timeline-clip-media"));

      expect(screen.queryByRole("dialog")).toBeNull();
      expect(screen.queryByText("编辑片段")).toBeNull();
    },
  );

  it("prefills an Agent adjustment prompt with a stable editor snapshot", async () => {
    const doc = newDoc("agent", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 120,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "video",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    usePlaybackStore.setState({ currentFrame: 15 });
    render(<TimelinePanel />);

    fireEvent.contextMenu(screen.getByTestId("timeline-clip-video"));
    fireEvent.click(
      await screen.findByRole("menuitem", {
        name: "让 Agent 调整精确参数…",
      }),
    );

    const request = useEditorContextStore.getState().agentPromptRequest;
    expect(request?.editorContext).toMatchObject({
      selectedItemIds: ["video"],
      playheadFrame: 15,
    });
    expect(request?.text).toContain('selectedItemIds=["video"]');
    expect(request?.text).toContain("playheadFrame=15");
  });

  it("keeps the toolbar focused on direct timeline tools", () => {
    render(<TimelinePanel />);

    expect(screen.getByTitle("选择模式 (V)")).toBeTruthy();
    expect(screen.getByTitle(/刀片编辑模式/)).toBeTruthy();
    expect(screen.getByTitle(/分割/)).toBeTruthy();
    expect(screen.getByTitle("关闭吸附")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "播放时间线" }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "适配全部内容" })).toBeTruthy();
    expect(screen.getByTitle("适配全部内容")).toBeTruthy();
    expect(screen.getByRole("button", { name: "字幕设置" })).toBeTruthy();
    expect(screen.getByText("00:00:02")).toBeTruthy();
    expect(screen.queryByTitle(/变速/)).toBeNull();
    expect(screen.queryByLabelText(/片段静音/)).toBeNull();
  });

  it("fits the active timeline's latest item into the lane viewport and scrolls home", () => {
    const doc = useTimelineStore.getState().localDoc!;
    doc.items.push({
      itemId: "late-title",
      trackId: "c1",
      kind: "text",
      from: 300,
      durationInFrames: 100,
      text: "片尾标题",
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    useSequenceStore.setState((state) => ({
      zoom: 3,
      timelines: [
        { ...state.timelines[0], zoom: 3 },
        {
          timelineId: "tl_2",
          name: "剪辑 2",
          revision: 1,
          durationInFrames: 120,
          currentFrame: 0,
          zoom: 2.5,
          createdAtUnixMs: 2,
          updatedAtUnixMs: 2,
        },
      ],
    }));
    render(<TimelinePanel />);
    const scroller = document.querySelector<HTMLElement>(
      "[data-track-overflow]",
    )!;
    Object.defineProperty(scroller, "clientWidth", {
      configurable: true,
      value: 1000,
    });
    scroller.scrollLeft = 240;

    fireEvent.click(screen.getByRole("button", { name: "适配全部内容" }));

    expect(useSequenceStore.getState().zoom).toBeCloseTo(1.88);
    expect(
      useSequenceStore.getState().timelines.find((timeline) => timeline.timelineId === "tl_1")
        ?.zoom,
    ).toBeCloseTo(1.88);
    expect(
      useSequenceStore.getState().timelines.find((timeline) => timeline.timelineId === "tl_2")
        ?.zoom,
    ).toBe(2.5);
    expect((screen.getByLabelText("时间线缩放") as HTMLInputElement).value).toBe(
      "1.88",
    );
    expect(scroller.scrollLeft).toBe(0);
    expect(doc.items.find((item) => item.itemId === "late-title")).toMatchObject({
      from: 300,
      durationInFrames: 100,
    });
  });

  it.each([
    { label: "极短", durationInFrames: 1, expectedZoom: 4 },
    { label: "超长", durationInFrames: 10_000, expectedZoom: 0.25 },
  ])("keeps $label content within zoom limits", ({ durationInFrames, expectedZoom }) => {
    const doc = newDoc("fit-limit", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "video",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames,
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);
    const scroller = document.querySelector<HTMLElement>(
      "[data-track-overflow]",
    )!;
    Object.defineProperty(scroller, "clientWidth", {
      configurable: true,
      value: 1000,
    });

    fireEvent.click(screen.getByRole("button", { name: "适配全部内容" }));

    expect(useSequenceStore.getState().zoom).toBe(expectedZoom);
  });

  it("renders the real V1 lane only for a cold-start timeline", () => {
    const doc = newDoc("cold-start", 1920, 1080, 30);
    doc.tracks.push({
      trackId: "v1",
      kind: "video",
      name: "视频轨",
      createdBy: "auto",
    });
    useTimelineStore.setState({ doc, localDoc: doc });

    render(<TimelinePanel />);

    expect(screen.getByTestId("track-header-v1").textContent).toContain(
      "V1视频轨",
    );
    expect(screen.queryByText("A1")).toBeNull();
    expect(
      (screen.getByRole("button", {
        name: "适配全部内容",
      }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.getByTestId("cold-start-drop-hint").textContent).toBe(
      "将素材拖到这里开始剪辑",
    );
    expect(submitOpsMock).not.toHaveBeenCalled();
  });

  it("opens CaptionPanel from Captions and adds a manual caption", async () => {
    render(<TimelinePanel />);
    fireEvent.click(screen.getByRole("button", { name: "字幕设置" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "新增字幕" }),
    );
    fireEvent.change(screen.getByLabelText("字幕文字"), {
      target: { value: "新增一句" },
    });
    fireEvent.click(
      within(screen.getByRole("dialog", { name: "新增字幕" })).getByRole(
        "button",
        { name: "新增字幕" },
      ),
    );

    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        {
          op: "addItem",
          item: expect.objectContaining({
            trackId: "c1",
            kind: "text",
            from: 0,
            durationInFrames: 60,
            text: "新增一句",
          }),
        },
      ]),
    );
  });

  it("keeps track heads compact and separates video visibility from mute", async () => {
    const doc = newDoc("tracks", 1920, 1080, 30);
    doc.tracks.push(
      { trackId: "v1", kind: "video" },
      { trackId: "a1", kind: "audio" },
    );
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);

    expect(screen.queryByLabelText(/轨道音量/)).toBeNull();
    expect(screen.queryByText("闪避")).toBeNull();
    expect(screen.getByRole("button", { name: "隐藏视频轨画面" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "静音视频轨内嵌音频" }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "静音轨道" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "显示音频轨画面" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "隐藏视频轨画面" }));
    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        { op: "updateTrack", trackId: "v1", trackPatch: { hidden: true } },
      ]),
    );
    expect(submitOpsMock.mock.calls[0][1][0].trackPatch).not.toHaveProperty(
      "muted",
    );

    submitOpsMock.mockClear();
    fireEvent.click(
      screen.getByRole("button", { name: "静音视频轨内嵌音频" }),
    );
    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        { op: "updateTrack", trackId: "v1", trackPatch: { muted: true } },
      ]),
    );
    expect(submitOpsMock.mock.calls[0][1][0].trackPatch).not.toHaveProperty(
      "hidden",
    );
  });

  it("prefills the track Agent entry for rename, volume and ducking", async () => {
    const doc = newDoc("tracks", 1920, 1080, 30);
    doc.tracks.push({
      trackId: "a1",
      kind: "audio",
      name: "BGM",
      volume: 0.8,
      ducking: {
        sidechainTrackIds: [],
        gain: 0.3,
        attackFrames: 6,
        releaseFrames: 15,
      },
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);

    fireEvent.pointerDown(
      screen.getByRole("button", { name: "A1 更多操作" }),
      { button: 0, ctrlKey: false },
    );
    fireEvent.click(
      await screen.findByRole("menuitem", {
        name: "让 Agent 调整轨道…",
      }),
    );

    const request = useEditorContextStore.getState().agentPromptRequest;
    expect(request?.text).toContain("轨道 a1");
    expect(request?.text).toContain("重命名");
    expect(request?.text).toContain("轨道音量");
    expect(request?.text).toContain("duck/sidechain");
    expect(request?.editorContext.timelineRevision).toBe(1);
  });

  it("adds, replaces and removes transitions by dropping Library payloads", async () => {
    const doc = newDoc("transition", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 180,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push(
      {
        itemId: "from",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "to",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 30,
        durationInFrames: 30,
        trimStart: 90,
      },
    );
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);

    const lane = document.querySelector<HTMLElement>('[data-track-id="v1"]')!;
    const target = screen.getByTestId("transition-drop-target-from-to");
    const crossfade = transitionDataTransfer("crossfade");
    fireEvent.dragOver(lane, { dataTransfer: crossfade });
    expect(target.dataset.dropState).toBe("enabled");
    fireEvent.drop(target, { dataTransfer: crossfade });

    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        expect.objectContaining({
          op: "addTransition",
          transition: expect.objectContaining({
            fromItemId: "from",
            toItemId: "to",
            kind: "crossfade",
            frames: 30,
          }),
        }),
      ]),
    );

    submitOpsMock.mockClear();
    doc.transitions = [{
      transitionId: "existing",
      fromItemId: "from",
      toItemId: "to",
      kind: "crossfade",
      frames: 15,
    }];
    useTimelineStore.setState({ doc, localDoc: doc });
    const dip = transitionDataTransfer("dipToBlack");
    fireEvent.dragOver(lane, { dataTransfer: dip });
    fireEvent.drop(target, { dataTransfer: dip });
    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        {
          op: "updateTransition",
          transitionId: "existing",
          transitionPatch: { kind: "dipToBlack", frames: 15 },
        },
      ]),
    );

    submitOpsMock.mockClear();
    const none = transitionDataTransfer("none");
    fireEvent.dragOver(lane, { dataTransfer: none });
    fireEvent.drop(target, { dataTransfer: none });
    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        { op: "removeTransition", transitionId: "existing" },
      ]),
    );
    expect(screen.queryByTestId("transition-entry-from-to")).toBeNull();
    expect(screen.queryByLabelText("转场类型")).toBeNull();
  });

  it("automatically shortens a dropped transition to the available source handle", async () => {
    const doc = newDoc("short transition handle", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 31,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push(
      {
        itemId: "from",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "to",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 30,
        durationInFrames: 30,
      },
    );
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);

    const lane = document.querySelector<HTMLElement>('[data-track-id="v1"]')!;
    const target = screen.getByTestId("transition-drop-target-from-to");
    const crossfade = transitionDataTransfer("crossfade");
    fireEvent.dragOver(lane, { dataTransfer: crossfade });

    expect(target.dataset.dropState).toBe("enabled");
    expect(target.dataset.transitionFrames).toBe("1");
    expect(target.getAttribute("title")).toContain("已按素材余量缩短");
    fireEvent.drop(target, { dataTransfer: crossfade });

    await waitFor(() =>
      expect(submitOpsMock).toHaveBeenCalledWith("p1", [
        expect.objectContaining({
          op: "addTransition",
          transition: expect.objectContaining({ frames: 1 }),
        }),
      ]),
    );
  });

  it("shows disabled transition targets with the exact boundary reason", () => {
    const doc = newDoc("locked transition", 1920, 1080, 30);
    doc.media.push({
      mediaId: "m1",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 60,
    });
    doc.tracks.push({ trackId: "v1", kind: "video", locked: true });
    doc.items.push(
      {
        itemId: "from",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "to",
        trackId: "v1",
        kind: "video",
        mediaId: "m1",
        from: 30,
        durationInFrames: 30,
      },
    );
    useTimelineStore.setState({ doc, localDoc: doc });
    render(<TimelinePanel />);

    const lane = document.querySelector<HTMLElement>('[data-track-id="v1"]')!;
    const transfer = transitionDataTransfer("crossfade");
    fireEvent.dragOver(lane, { dataTransfer: transfer });
    const target = screen.getByTestId("transition-drop-target-from-to");
    expect(target.dataset.dropState).toBe("disabled");
    expect(target.getAttribute("title")).toBe("轨道已锁定");
    fireEvent.drop(target, { dataTransfer: transfer });
    expect(submitOpsMock).not.toHaveBeenCalled();
    expect(screen.getByText("轨道已锁定")).toBeTruthy();
  });
});
