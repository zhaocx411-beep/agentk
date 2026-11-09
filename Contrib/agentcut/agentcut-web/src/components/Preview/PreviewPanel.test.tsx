// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import { usePlaybackStore } from "@/store/playbackStore";
import { useProjectStore } from "@/store/projectStore";
import { usePreviewOverridesStore } from "@/store/previewOverridesStore";
import { useTimelineStore } from "@/store/timelineStore";

const submitOpsMock = vi.fn();
vi.mock("@/store/submitOps", () => ({
  submitOps: (...args: unknown[]) => submitOpsMock(...args),
}));

import { PreviewPanel } from "./PreviewPanel";

const originalAudioContext = window.AudioContext;

afterEach(() => {
  cleanup();
  submitOpsMock.mockReset();
  usePreviewOverridesStore.getState().reset();
  vi.restoreAllMocks();
  Object.defineProperty(window, "AudioContext", {
    configurable: true,
    value: originalAudioContext,
  });
});

describe("PreviewPanel recovery states", () => {
  it("renders the cold-start preview as a neutral import prompt", () => {
    const doc = newDoc("empty-preview", 1920, 1080, 30);
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });

    render(<PreviewPanel />);

    const emptyState = screen.getByTestId("preview-empty-state");
    expect(emptyState.textContent).toContain("将素材添加到时间线后预览");
    expect(emptyState.className).toContain("text-[#9CA3AF]");
    expect(emptyState.parentElement?.className).toContain("bg-[#F2F2F0]");
    expect(emptyState.parentElement?.className).not.toContain("rounded-md");
    expect(emptyState.parentElement?.className).not.toContain("border-[#D8D6D2]");
  });

  it("renders a recoverable message instead of crashing for missing media", () => {
    const doc = newDoc("preview", 1920, 1080, 30);
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "missing",
      trackId: "v1",
      kind: "video",
      mediaId: "not-found",
      from: 0,
      durationInFrames: 30,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });

    render(<PreviewPanel />);

    expect(screen.getByText(/片段引用的媒体已缺失/)).toBeTruthy();
    expect(screen.getByText(/重新上传或替换素材/)).toBeTruthy();
  });

  it("renders normalized PiP geometry and preserves track layer order", () => {
    const doc = newDoc("preview", 1920, 1080, 30);
    doc.media.push(
      { mediaId: "m1", uri: "blob://one", kind: "image", durationInFrames: 0 },
      { mediaId: "m2", uri: "blob://two", kind: "image", durationInFrames: 0 },
    );
    doc.tracks.push(
      { trackId: "bottom", kind: "video" },
      { trackId: "top", kind: "video" },
    );
    doc.items.push(
      {
        itemId: "full",
        trackId: "bottom",
        kind: "image",
        mediaId: "m1",
        from: 0,
        durationInFrames: 30,
      },
      {
        itemId: "pip",
        trackId: "top",
        kind: "image",
        mediaId: "m2",
        from: 0,
        durationInFrames: 30,
        transform: {
          x: 0.55,
          y: 0.05,
          width: 0.4,
          height: 0.4,
          opacity: 0.8,
          fit: "cover",
        },
      },
    );
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    render(<PreviewPanel />);

    const full = screen.getByTestId("preview-layer-full");
    const pip = screen.getByTestId("preview-layer-pip");
    expect(full.style.left).toBe("0%");
    expect(full.style.width).toBe("100%");
    expect(Number.parseFloat(pip.style.left)).toBeCloseTo(55);
    expect(pip.style.top).toBe("5%");
    expect(pip.style.width).toBe("40%");
    expect(pip.style.height).toBe("40%");
    expect(pip.style.opacity).toBe("0.8");
    expect(full.compareDocumentPosition(pip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("routes managed preview media through the same-origin content endpoint", () => {
    const doc = newDoc("preview", 1920, 1080, 30);
    doc.media.push({
      mediaId: "managed media",
      uri: "https://cdn.example.invalid/video.mp4",
      blobKey: "agentcut/p/source/managed/video.mp4",
      kind: "video",
      durationInFrames: 30,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "managed media",
      from: 0,
      durationInFrames: 30,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    useProjectStore.setState({
      project: { projectId: "project id", title: "preview", createdAtUnixMs: 1 },
    });

    render(<PreviewPanel />);

    expect(document.querySelector("video")?.getAttribute("src")).toBe(
      "/v1/projects/project%20id/media/managed%20media/content",
    );
  });

  it("uses the browser-compatible playback endpoint for managed audio", () => {
    const doc = newDoc("preview", 1920, 1080, 30);
    doc.media.push({
      mediaId: "wav",
      uri: "https://cdn.example.invalid/audio.wav",
      blobKey: "agentcut/p/source/wav/audio.wav",
      kind: "audio",
      durationInFrames: 30,
    });
    doc.tracks.push({ trackId: "a1", kind: "audio" });
    doc.items.push({
      itemId: "a",
      trackId: "a1",
      kind: "audio",
      mediaId: "wav",
      from: 0,
      durationInFrames: 30,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    useProjectStore.setState({
      project: { projectId: "project", title: "preview", createdAtUnixMs: 1 },
    });

    render(<PreviewPanel />);

    expect(document.querySelector("audio")?.getAttribute("src")).toBe(
      "/v1/projects/project/media/wav/playback",
    );
  });

  it("mixes overlapping video sound and audio instead of muting either source", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    const doc = newDoc("mix", 1920, 1080, 30);
    doc.media.push(
      { mediaId: "video", uri: "blob://video", kind: "video", durationInFrames: 60 },
      { mediaId: "audio", uri: "blob://audio", kind: "audio", durationInFrames: 60 },
    );
    doc.tracks.push(
      { trackId: "v1", kind: "video", volume: 1 },
      { trackId: "a1", kind: "audio", volume: 1 },
    );
    doc.items.push(
      {
        itemId: "v",
        trackId: "v1",
        kind: "video",
        mediaId: "video",
        from: 0,
        durationInFrames: 60,
      },
      {
        itemId: "a",
        trackId: "a1",
        kind: "audio",
        mediaId: "audio",
        from: 0,
        durationInFrames: 60,
      },
    );
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    useProjectStore.setState({
      project: { projectId: "mix", title: "mix", createdAtUnixMs: 1 },
    });
    render(<PreviewPanel />);

    const button = screen.getByRole("button", { name: "播放时间线" });
    fireEvent.pointerDown(button);
    fireEvent.click(button);

    await waitFor(() => expect(play.mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(document.querySelector("video")?.muted).toBe(false);
    expect(document.querySelector("audio")?.muted).toBe(false);
  });

  it("shows a recoverable error when the browser rejects timeline playback", async () => {
    vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValue(
      new DOMException("playback blocked", "NotAllowedError"),
    );
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    const doc = newDoc("blocked", 1920, 1080, 30);
    doc.media.push({
      mediaId: "video",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 60,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "video",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });

    render(<PreviewPanel />);
    fireEvent.click(screen.getByRole("button", { name: "播放时间线" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "浏览器阻止了时间线媒体播放",
    );
  });

  it("does not report an outgoing clip AbortError as a browser playback block", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValue(
      new DOMException(
        "The play() request was interrupted because the media was removed from the document.",
        "AbortError",
      ),
    );
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    const doc = newDoc("clip boundary", 1920, 1080, 30);
    doc.media.push({
      mediaId: "video",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 60,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "video",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });

    render(<PreviewPanel />);
    fireEvent.click(screen.getByRole("button", { name: "播放时间线" }));

    await waitFor(() => expect(play).toHaveBeenCalled());
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("hides video-track pixels without removing its embedded-audio element", () => {
    const doc = newDoc("hidden video", 1920, 1080, 30);
    doc.media.push({
      mediaId: "video",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 60,
    });
    doc.tracks.push({ trackId: "v1", kind: "video", hidden: true });
    doc.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "video",
      from: 0,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });

    render(<PreviewPanel />);

    expect(screen.getByTestId("preview-layer-v").style.opacity).toBe("0");
    expect(document.querySelector("video")).toBeTruthy();
    expect(doc.tracks[0].muted).not.toBe(true);
  });

  it("disposes Web Audio nodes when switching projects", async () => {
    const close = vi.fn().mockResolvedValue(undefined);
    const disconnect = vi.fn();
    const createMediaElementSource = vi.fn(() => ({ connect: vi.fn(), disconnect }));
    class MockAudioContext {
      destination = {};
      state = "running";
      createMediaElementSource = createMediaElementSource;
      createGain() {
        return { connect: vi.fn(), disconnect, gain: { value: 1 } };
      }
      resume() {
        return Promise.resolve();
      }
      close = close;
    }
    Object.defineProperty(window, "AudioContext", {
      configurable: true,
      value: MockAudioContext,
    });
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    const doc = newDoc("preview", 1920, 1080, 30);
    doc.media.push({ mediaId: "m1", uri: "blob://video", kind: "video", durationInFrames: 90 });
    doc.tracks.push({ trackId: "v1", kind: "video", volume: 2 });
    doc.items.push({
      itemId: "v",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 30,
      volume: 1.5,
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    useProjectStore.setState({ project: { projectId: "p1", title: "one", createdAtUnixMs: 1 } });
    render(<PreviewPanel />);
    fireEvent.pointerDown(window);
    act(() => {
      usePlaybackStore.setState({ isPlaying: true });
    });
    await waitFor(() => expect(createMediaElementSource).toHaveBeenCalled());

    act(() => {
      useProjectStore.setState({ project: { projectId: "p2", title: "two", createdAtUnixMs: 2 } });
    });

    expect(disconnect).toHaveBeenCalled();
    expect(close).toHaveBeenCalled();
  });

  it("renders styled multiline captions at normalized coordinates", () => {
    const doc = newDoc("caption", 1920, 1080, 30);
    doc.tracks.push({ trackId: "c1", kind: "caption" });
    doc.items.push({
      itemId: "caption",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 30,
      text: "中文\n换行",
      captionStyle: {
        textColor: "#FF0000",
        backgroundColor: "#00000080",
        outlineColor: "#FFFFFF",
        outlineWidth: 3,
        align: "left",
        x: 0.2,
        y: 0.3,
        maxWidth: 0.5,
      },
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    render(<PreviewPanel />);

    const caption = screen.getByTestId("caption-layer-caption");
    expect(caption.textContent).toBe("中文\n换行");
    expect(caption.style.left).toBe("20%");
    expect(caption.style.top).toBe("30%");
    expect(caption.style.width).toBe("50%");
    expect(caption.style.whiteSpace).toBe("pre-wrap");
    expect(caption.style.webkitTextStroke).toContain("3px");
  });

  it("does not preview captions from a muted caption track", () => {
    const doc = newDoc("muted caption", 1920, 1080, 30);
    doc.tracks.push({ trackId: "c1", kind: "caption", muted: true });
    doc.items.push({
      itemId: "hidden-caption",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 30,
      text: "不应显示",
    });
    useTimelineStore.setState({
      doc,
      localDoc: doc,
      confirmedRevision: 1,
    });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });

    render(<PreviewPanel />);

    expect(screen.queryByTestId("caption-layer-hidden-caption")).toBeNull();
  });

  it("updates crop override during drag and commits once on pointer-up", async () => {
    submitOpsMock.mockResolvedValue(null);
    const doc = newDoc("crop", 1920, 1080, 30);
    doc.media.push({ mediaId: "image", uri: "blob://image", kind: "image", durationInFrames: 0 });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "image",
      trackId: "v1",
      kind: "image",
      mediaId: "image",
      from: 0,
      durationInFrames: 30,
      transform: { x: 0, y: 0, width: 1, height: 1, fit: "contain" },
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({ currentFrame: 0, isPlaying: false, sourcePreviewMediaId: null });
    useProjectStore.setState({ project: { projectId: "p1", title: "crop", createdAtUnixMs: 1 } });
    usePreviewOverridesStore.getState().setCropEditingItem("image");
    render(<PreviewPanel />);

    const overlay = screen.getByTestId("crop-overlay-image");
    Object.defineProperty(overlay.parentElement!, "getBoundingClientRect", {
      configurable: true,
      value: () => ({ left: 0, top: 0, width: 200, height: 100, right: 200, bottom: 100 }),
    });
    fireEvent.pointerDown(screen.getByTestId("crop-handle-se"), {
      button: 0,
      clientX: 100,
      clientY: 50,
    });
    fireEvent.pointerMove(window, { clientX: 80, clientY: 40 });
    expect(
      usePreviewOverridesStore.getState().itemPatches.image.transform?.crop,
    ).toEqual({ x: 0, y: 0, width: 0.9, height: 0.9 });
    expect(submitOpsMock).not.toHaveBeenCalled();
    fireEvent.pointerUp(window, { clientX: 80, clientY: 40 });
    fireEvent.pointerUp(window, { clientX: 70, clientY: 30 });

    await waitFor(() => expect(submitOpsMock).toHaveBeenCalledTimes(1));
    expect(submitOpsMock.mock.calls[0][1]).toEqual([
      {
        op: "updateItem",
        itemId: "image",
        patch: {
          transform: {
            x: 0,
            y: 0,
            width: 1,
            height: 1,
            fit: "contain",
            crop: { x: 0, y: 0, width: 0.9, height: 0.9 },
          },
        },
      },
    ]);
    expect(usePreviewOverridesStore.getState().cropEditingItemId).toBe("image");
    fireEvent.click(screen.getByRole("button", { name: "退出裁剪" }));
    expect(usePreviewOverridesStore.getState().cropEditingItemId).toBeNull();
  });

  it("cancels an active crop gesture without submitting", () => {
    const doc = newDoc("crop", 1920, 1080, 30);
    doc.media.push({
      mediaId: "image",
      uri: "blob://image",
      kind: "image",
      durationInFrames: 0,
    });
    doc.tracks.push({ trackId: "v1", kind: "video" });
    doc.items.push({
      itemId: "image",
      trackId: "v1",
      kind: "image",
      mediaId: "image",
      from: 0,
      durationInFrames: 30,
      transform: { x: 0, y: 0, width: 1, height: 1, fit: "contain" },
    });
    useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 1 });
    usePlaybackStore.setState({
      currentFrame: 0,
      isPlaying: false,
      sourcePreviewMediaId: null,
    });
    useProjectStore.setState({
      project: { projectId: "p1", title: "crop", createdAtUnixMs: 1 },
    });
    usePreviewOverridesStore.getState().setCropEditingItem("image");
    render(<PreviewPanel />);

    const overlay = screen.getByTestId("crop-overlay-image");
    Object.defineProperty(overlay.parentElement!, "getBoundingClientRect", {
      configurable: true,
      value: () => ({
        left: 0,
        top: 0,
        width: 200,
        height: 100,
        right: 200,
        bottom: 100,
      }),
    });
    fireEvent.pointerDown(screen.getByTestId("crop-handle-se"), {
      button: 0,
      clientX: 100,
      clientY: 50,
    });
    fireEvent.pointerMove(window, { clientX: 80, clientY: 40 });
    expect(useTimelineStore.getState().gestureActive).toBe(true);

    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.pointerUp(window, { clientX: 80, clientY: 40 });

    expect(usePreviewOverridesStore.getState().cropEditingItemId).toBeNull();
    expect(usePreviewOverridesStore.getState().itemPatches.image).toBeUndefined();
    expect(useTimelineStore.getState().gestureActive).toBe(false);
    expect(submitOpsMock).not.toHaveBeenCalled();
  });
});
