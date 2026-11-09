// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Item, Media } from "@/domain/timeline/types";

import { TimelineClip } from "./TimelineClip";
import {
  resampleWaveformPeaks,
  visibleSourceRange,
  waveformDisplayGain,
} from "./timelineWaveform";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TimelineClip", () => {
  it("renders clip state and delegates existing gestures", () => {
    const item: Item = {
      itemId: "caption-a",
      trackId: "c1",
      kind: "text",
      from: 10,
      durationInFrames: 20,
      text: "字幕 A",
    };
    const onPointerDown = vi.fn();
    const onTrimPointerDown = vi.fn();
    const props = {
      item,
      trackKind: "caption" as const,
      trackLocked: false,
      editMode: "select" as const,
      left: 10,
      width: 20,
      pxPerFrame: 1,
      selected: true,
      dragging: false,
      agentTouched: false,
      trimTargetFrame: null,
      fps: 30,
      onKeyDown: vi.fn(),
      onContextMenu: vi.fn(),
      onPointerDown,
      onTrimPointerDown,
      onFadePointerDown: vi.fn(),
    };
    const { rerender } = render(<TimelineClip {...props} />);

    const clip = screen.getByRole("button", { name: "字幕 A，片段" });
    expect(clip.getAttribute("aria-pressed")).toBe("true");
    fireEvent.pointerDown(screen.getByTestId("trim-start-caption-a"));
    expect(onTrimPointerDown).toHaveBeenCalledWith(
      expect.anything(),
      "trimStart",
    );

    onTrimPointerDown.mockClear();
    rerender(<TimelineClip {...props} editMode="blade" />);
    fireEvent.pointerDown(screen.getByTestId("trim-start-caption-a"));
    expect(onTrimPointerDown).not.toHaveBeenCalled();
    expect(onPointerDown).toHaveBeenCalled();
  });

  it("positions ChatCut-style fade handles and renders a compact tooltip", () => {
    const onFadePointerDown = vi.fn();
    const item: Item = {
      itemId: "video-a",
      trackId: "v1",
      kind: "video",
      mediaId: "m1",
      from: 0,
      durationInFrames: 60,
      fadeInFrames: 15,
    };
    render(
      <TimelineClip
        item={item}
        trackKind="video"
        trackLocked={false}
        editMode="select"
        left={0}
        width={60}
        pxPerFrame={1}
        selected
        dragging={false}
        agentTouched={false}
        trimTargetFrame={null}
        fps={30}
        fadePreviewEdge="fadeInFrames"
        fadeOutDisabledReason="该边界由转场控制"
        onKeyDown={vi.fn()}
        onContextMenu={vi.fn()}
        onPointerDown={vi.fn()}
        onTrimPointerDown={vi.fn()}
        onFadePointerDown={onFadePointerDown}
      />,
    );

    const fadeHandle = screen.getByTestId("fade-handle-in-video-a");
    fireEvent.pointerDown(fadeHandle);
    expect(onFadePointerDown).toHaveBeenCalledWith(
      expect.anything(),
      "fadeInFrames",
    );
    expect(fadeHandle.style.left).toBe("15px");
    expect(fadeHandle.className).toContain("h-3");
    expect(fadeHandle.className).toContain("w-2");
    expect(fadeHandle.className).toContain("opacity-100");
    const fadeCurve = screen
      .getByTestId("fade-in-video-a")
      .querySelectorAll("path")[1];
    expect(fadeCurve?.getAttribute("d")).toContain("C");
    expect(screen.getByTestId("fade-tooltip-video-a").textContent).toContain(
      "淡入: 0.5s",
    );
    expect(
      (screen.getByTestId("fade-handle-out-video-a") as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("renders an uncropped filmstrip sequence and bottom metadata bar", () => {
    const item: Item = {
      itemId: "video-filmstrip",
      trackId: "v1",
      kind: "video",
      mediaId: "media-video",
      from: 0,
      durationInFrames: 120,
    };
    const media: Media = {
      mediaId: "media-video",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 120,
      displayName: "专业视频素材",
      thumbnailUri: "/sprite.jpg",
    };
    render(
      <TimelineClip
        item={item}
        media={media}
        trackKind="video"
        trackLocked={false}
        editMode="select"
        left={0}
        width={180}
        pxPerFrame={1}
        selected={false}
        dragging={false}
        agentTouched={false}
        trimTargetFrame={null}
        fps={30}
        onKeyDown={vi.fn()}
        onContextMenu={vi.fn()}
        onPointerDown={vi.fn()}
        onTrimPointerDown={vi.fn()}
        onFadePointerDown={vi.fn()}
      />,
    );

    const filmstrip = screen.getByTestId("video-filmstrip");
    const frames = filmstrip.querySelectorAll("img");
    expect(frames.length).toBe(3);
    expect(frames[0].className).toContain("object-cover");
    expect(frames[0].style.width).toBe("1000%");
    expect(screen.getByText("专业视频素材").parentElement?.style.bottom).toBe(
      "0px",
    );
    expect(screen.getByTestId("trim-start-video-filmstrip").className).toContain(
      "opacity-0",
    );
  });

  it("renders source-audio peaks as a third video layer with trim-aware data", () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      setTransform: vi.fn(),
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      fillStyle: "",
    } as unknown as CanvasRenderingContext2D);
    const item: Item = {
      itemId: "video-wave",
      trackId: "v1",
      kind: "video",
      mediaId: "media-video",
      from: 30,
      durationInFrames: 40,
      trimStart: 20,
      muted: true,
    };
    const media: Media = {
      mediaId: "media-video",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 100,
      displayName: "采访视频",
      thumbnailUri: "/sprite.jpg",
      probe: { hasAudio: true },
      waveformPeaks: [0.1, 0.2, 0.8, 0.4, 1, 0.3, 0.2, 0.7, 0.1, 0.5],
    };
    render(
      <TimelineClip
        item={item}
        media={media}
        trackKind="video"
        trackLocked={false}
        editMode="select"
        left={30}
        width={120}
        pxPerFrame={3}
        selected={false}
        dragging={false}
        agentTouched={false}
        trimTargetFrame={null}
        fps={30}
        onKeyDown={vi.fn()}
        onContextMenu={vi.fn()}
        onPointerDown={vi.fn()}
        onTrimPointerDown={vi.fn()}
        onFadePointerDown={vi.fn()}
      />,
    );

    expect(visibleSourceRange(item, media)).toEqual([0.2, 0.6]);
    expect(screen.getByTestId("video-filmstrip").style.bottom).toBe("48px");
    expect(screen.getByText("采访视频").parentElement?.style.bottom).toBe("28px");
    expect(screen.getByTestId("video-embedded-waveform").dataset.muted).toBe(
      "true",
    );
    expect(screen.getByTestId("audio-waveform")).toBeTruthy();
  });

  it("adapts waveform density while preserving the selected source interval", () => {
    const peaks = [0.1, 0.2, 0.9, 0.4, 0.7, 0.3, 0.8, 0.2];
    const compact = resampleWaveformPeaks(peaks, 0.25, 0.75, 12);
    const expanded = resampleWaveformPeaks(peaks, 0.25, 0.75, 120);

    expect(compact).toHaveLength(4);
    expect(Math.max(...compact)).toBe(0.9);
    expect(expanded).toHaveLength(40);
    expect(expanded.every((peak) => peak >= 0.2 && peak <= 0.9)).toBe(true);
  });

  it("amplifies quiet waveform data for display without changing its source peaks", () => {
    const quietPeaks = [0.007, 0.015, 0.034];

    expect(waveformDisplayGain(quietPeaks)).toBeCloseTo(0.92 / 0.034);
    expect(waveformDisplayGain([0, 0])).toBe(1);
    expect(waveformDisplayGain([0.5, 1])).toBe(1);
    expect(quietPeaks).toEqual([0.007, 0.015, 0.034]);
  });

  it("shows a non-blocking loading state while video peaks are generated", () => {
    const item: Item = {
      itemId: "video-loading-wave",
      trackId: "v1",
      kind: "video",
      mediaId: "media-video",
      from: 0,
      durationInFrames: 60,
    };
    const media: Media = {
      mediaId: "media-video",
      uri: "blob://video",
      kind: "video",
      durationInFrames: 60,
      probe: { hasAudio: true },
      processing: {
        status: "processing",
        readyForTimeline: true,
        probe: "succeeded",
        thumbnail: "running",
        waveform: "running",
        transcript: "pending",
        understanding: "pending",
      },
    };
    render(
      <TimelineClip
        item={item}
        media={media}
        trackKind="video"
        trackLocked={false}
        editMode="select"
        left={0}
        width={120}
        pxPerFrame={2}
        selected={false}
        dragging={false}
        agentTouched={false}
        trimTargetFrame={null}
        fps={30}
        onKeyDown={vi.fn()}
        onContextMenu={vi.fn()}
        onPointerDown={vi.fn()}
        onTrimPointerDown={vi.fn()}
        onFadePointerDown={vi.fn()}
      />,
    );

    expect(screen.getByTestId("video-embedded-waveform")).toBeTruthy();
    expect(screen.getByText("波形生成中…")).toBeTruthy();
  });

  it("dims real audio peaks when its audio track is muted", () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      setTransform: vi.fn(),
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      fillStyle: "",
    } as unknown as CanvasRenderingContext2D);
    const item: Item = {
      itemId: "audio-wave",
      trackId: "a1",
      kind: "audio",
      mediaId: "media-audio",
      from: 0,
      durationInFrames: 90,
    };
    const media: Media = {
      mediaId: "media-audio",
      uri: "blob://audio",
      kind: "audio",
      durationInFrames: 90,
      displayName: "背景音乐",
      waveformPeaks: [0.2, 0.8, 0.45, 1],
    };
    render(
      <TimelineClip
        item={item}
        media={media}
        trackKind="audio"
        trackLocked={false}
        trackMuted
        editMode="select"
        left={0}
        width={180}
        pxPerFrame={1}
        selected
        dragging={false}
        agentTouched={false}
        trimTargetFrame={null}
        fps={30}
        onKeyDown={vi.fn()}
        onContextMenu={vi.fn()}
        onPointerDown={vi.fn()}
        onTrimPointerDown={vi.fn()}
        onFadePointerDown={vi.fn()}
      />,
    );

    expect(screen.getByTestId("audio-waveform")).toBeTruthy();
    expect(screen.getByText("背景音乐")).toBeTruthy();
    expect(screen.getByTestId("timeline-clip-audio-wave").dataset.muted).toBe(
      "true",
    );
    expect(screen.getByTestId("timeline-clip-audio-wave").className).toContain(
      "bg-emerald-950/35",
    );
    expect(screen.getByText("背景音乐").parentElement?.className).toContain(
      "bg-emerald-950/60",
    );
  });
});
