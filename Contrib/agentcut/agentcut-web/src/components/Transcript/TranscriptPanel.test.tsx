// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { MediaLibrary } from "@/components/MediaLibrary/MediaLibrary";
import { autoCaptionTrackBaseId } from "@/domain/captionTracks";
import { newDoc, type TimelineDoc } from "@/domain/timeline/types";
import type { TranscriptEntry } from "@/remote";
import { usePlaybackStore } from "@/store/playbackStore";
import { useProjectStore } from "@/store/projectStore";
import { resetTranscriptStore, useTranscriptStore } from "@/store/transcriptStore";
import { useTimelineStore } from "@/store/timelineStore";

const submitOpsMock = vi.fn();
const submitTranscriptDeleteMock = vi.fn();
const getTranscriptViewMock = vi.fn();

vi.mock("@/store/submitOps", () => ({
  submitOps: (...args: unknown[]) => submitOpsMock(...args),
  submitTranscriptDelete: (...args: unknown[]) => submitTranscriptDeleteMock(...args),
}));

vi.mock("@/remote", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/remote")>();
  return {
    ...original,
    api: {
      getTranscriptView: (...args: unknown[]) => getTranscriptViewMock(...args),
    },
  };
});

import { TranscriptPanel } from "./TranscriptPanel";

function entry(
  occurrenceId: string,
  text: string,
  fromFrame: number,
  segmentId = "segment-1",
  mediaId = "media-1",
  itemId = "item-1",
): TranscriptEntry {
  return {
    occurrenceId,
    itemId,
    trackId: "v1",
    mediaId,
    mediaGeneration: 1,
    segmentId,
    wordId: `word-${occurrenceId}`,
    text,
    sourceStartMs: fromFrame * 10,
    sourceEndMs: (fromFrame + 10) * 10,
    fromFrame,
    toFrame: fromFrame + 10,
    granularity: "word",
  };
}

function timeline(): TimelineDoc {
  const doc = newDoc("test", 1920, 1080, 30);
  doc.tracks = [{ trackId: "v1", kind: "video" }];
  doc.media = [
    {
      mediaId: "media-1",
      displayName: "访谈素材",
      uri: "blob://media-1",
      kind: "video",
      durationInFrames: 300,
      transcript: {
        revision: 1,
        segments: [
          {
            segmentId: "segment-1",
            startMs: 0,
            endMs: 1000,
            text: "第一句 第二句",
          },
        ],
      },
    },
  ];
  doc.items = [
    {
      itemId: "item-1",
      trackId: "v1",
      kind: "video",
      mediaId: "media-1",
      from: 0,
      durationInFrames: 60,
    },
  ];
  return doc;
}

function setReady(entries: TranscriptEntry[]) {
  useTranscriptStore.setState({
    projectId: "p1",
    timelineRevision: 3,
    status: "ready",
    entries,
    warnings: [],
    error: null,
    selectedOccurrenceIds: new Set(),
    anchorOccurrenceId: null,
  });
}

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
  submitOpsMock.mockReset().mockResolvedValue(null);
  submitTranscriptDeleteMock.mockReset().mockResolvedValue(null);
  getTranscriptViewMock.mockReset().mockResolvedValue({
    timelineRevision: 3,
    entries: [],
    warnings: [],
  });
  const doc = timeline();
  useTimelineStore.setState({
    doc,
    localDoc: doc,
    confirmedRevision: 3,
    pendingOps: [],
    syncStatus: "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
  useProjectStore.setState({
    project: {
      projectId: "p1",
      title: "test",
      createdAtUnixMs: 1,
    },
    projects: [],
    loading: false,
    bootstrapError: null,
  });
  usePlaybackStore.setState({
    currentFrame: 0,
    isPlaying: false,
    sourcePreviewMediaId: null,
  });
  resetTranscriptStore("p1");
});

afterEach(() => cleanup());

describe("TranscriptPanel", () => {
  it("shows loading, processing, failure, and empty states", () => {
    useTranscriptStore.setState({ status: "loading" });
    const { unmount } = render(<TranscriptPanel />);
    expect(screen.getByText("正在加载项目文字稿…")).toBeTruthy();
    unmount();

    const doc = timeline();
    doc.media[0].processing = {
      status: "processing",
      readyForTimeline: true,
      probe: "succeeded",
      thumbnail: "succeeded",
      waveform: "succeeded",
      transcript: "running",
      understanding: "pending",
    };
    useTimelineStore.setState({ doc, localDoc: doc });
    setReady([]);
    const processing = render(<TranscriptPanel />);
    expect(screen.getByText(/正在处理 1 个素材的文字稿/)).toBeTruthy();
    processing.unmount();

    useTranscriptStore.setState({ status: "error", error: "network failed" });
    const failed = render(<TranscriptPanel />);
    expect(screen.getByRole("alert").textContent).toContain("network failed");
    failed.unmount();

    useTimelineStore.setState({ doc: timeline(), localDoc: timeline() });
    setReady([]);
    render(<TranscriptPanel />);
    expect(screen.getByText("当前视频在时间线上没有可见文字稿")).toBeTruthy();
  });

  it("seeks and highlights through the shared playback frame", () => {
    setReady([entry("occ-1", "第一词", 10), entry("occ-2", "第二词", 20)]);
    render(<TranscriptPanel />);
    fireEvent.click(screen.getByRole("option", { name: "第二词" }));
    expect(usePlaybackStore.getState().currentFrame).toBe(20);
    expect(screen.getByRole("option", { name: "第二词" }).className).toContain(
      "bg-emerald-500/20",
    );
  });

  it("supports shift selection across segments and deletes by occurrenceId only", async () => {
    setReady([
      entry("occ-1", "第一词", 10, "segment-1"),
      entry("occ-2", "第二词", 20, "segment-2"),
    ]);
    render(<TranscriptPanel />);
    fireEvent.pointerDown(screen.getByRole("option", { name: "第一词" }), {
      button: 0,
      buttons: 1,
    });
    fireEvent.pointerDown(screen.getByRole("option", { name: "第二词" }), {
      button: 0,
      buttons: 1,
      shiftKey: true,
    });

    expect(screen.getByRole("option", { name: "第一词" }).getAttribute("aria-selected")).toBe(
      "true",
    );
    expect(screen.getByRole("option", { name: "第二词" }).getAttribute("aria-selected")).toBe(
      "true",
    );
    fireEvent.click(screen.getByRole("button", { name: "删字剪视频" }));
    expect(screen.getByText(/全局 ripple/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(submitTranscriptDeleteMock).toHaveBeenCalledWith("p1", [
      { fromOccurrenceId: "occ-1", toOccurrenceId: "occ-2" },
    ]);
    const payload = JSON.stringify(submitTranscriptDeleteMock.mock.calls[0]);
    expect(payload).not.toContain("fromFrame");
    expect(payload).not.toContain("toFrame");
  });

  it("uses existing Timeline operations for automatic captions", async () => {
    setReady([entry("occ-1", "第一句", 10)]);
    render(<TranscriptPanel />);
    fireEvent.click(screen.getByRole("button", { name: "生成或更新自动字幕" }));
    await Promise.resolve();
    expect(submitOpsMock).toHaveBeenCalledOnce();
    const operations = submitOpsMock.mock.calls[0][1];
    expect(operations.some((operation: { op: string }) => operation.op === "setCaptions")).toBe(
      true,
    );
    expect(operations).toContainEqual(
      expect.objectContaining({
        op: "addTrack",
        track: expect.objectContaining({
          trackId: autoCaptionTrackBaseId("media-1"),
          kind: "caption",
        }),
      }),
    );
  });

  it("renders a bounded chunk for very large transcripts", () => {
    setReady(
      Array.from({ length: 5000 }, (_, index) =>
        entry(`occ-${index}`, `word-${index}`, index * 10),
      ),
    );
    render(<TranscriptPanel />);
    expect(within(screen.getByRole("listbox")).getAllByRole("option").length).toBeLessThanOrEqual(300);
    expect(screen.getByRole("button", { name: /加载更多/ })).toBeTruthy();
  });

  it("shows one selected video transcript instead of mixing all media", () => {
    const doc = timeline();
    doc.media.push({
      mediaId: "media-2",
      displayName: "第二个视频",
      uri: "blob://media-2",
      kind: "video",
      durationInFrames: 300,
    });
    doc.items.push({
      itemId: "item-2",
      trackId: "v1",
      kind: "video",
      mediaId: "media-2",
      from: 60,
      durationInFrames: 60,
    });
    useTimelineStore.setState({ doc, localDoc: doc });
    setReady([
      entry("occ-1", "第一个视频文字", 10),
      entry("occ-2", "第二个视频文字", 70, "segment-2", "media-2", "item-2"),
    ]);

    render(<TranscriptPanel />);
    expect(screen.getByText("第一个视频文字")).toBeTruthy();
    expect(screen.queryByText("第二个视频文字")).toBeNull();

    fireEvent.change(screen.getByRole("combobox", { name: "选择视频文字稿素材" }), {
      target: { value: "media-2" },
    });
    expect(screen.queryByText("第一个视频文字")).toBeNull();
    expect(screen.getByText("第二个视频文字")).toBeTruthy();
  });

  it("adds an accessible transcript tab without changing the column layout", () => {
    setReady([]);
    render(<MediaLibrary />);
    expect(screen.getByRole("tab", { name: "素材" })).toBeTruthy();
    fireEvent.mouseDown(screen.getByRole("tab", { name: "文字稿" }), {
      button: 0,
      ctrlKey: false,
    });
    expect(screen.getByRole("textbox", { name: "搜索文字稿" })).toBeTruthy();
  });
});
