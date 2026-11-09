import { describe, expect, it, vi } from "vitest";

import type { Media } from "./timeline/types";
import {
  isMediaReadyForTimeline,
  mediaListNeedsProcessingPoll,
  mediaMenuAvailability,
  processingView,
  startMediaProcessingPoller,
} from "./mediaProcessing";

function media(overrides: Partial<Media> = {}): Media {
  return {
    mediaId: "m1",
    uri: "blob://m1",
    kind: "video",
    durationInFrames: 300,
    ...overrides,
  };
}

describe("media processing state", () => {
  it("keeps legacy media available and enables safe menu actions", () => {
    const legacy = media({
      transcript: { segments: [{ startMs: 0, endMs: 1000, text: "你好" }] },
      understandingText: "室内口播",
    });
    expect(isMediaReadyForTimeline(legacy)).toBe(true);
    expect(processingView(legacy)).toBeNull();
    expect(mediaMenuAvailability(legacy)).toEqual({
      addToTimeline: true,
      viewTranscript: true,
      generateCaptions: true,
      viewUnderstanding: true,
      retry: true,
    });
  });

  it("maps editable processing and partial failures to user-facing details", () => {
    const processing = media({
      processing: {
        status: "processing",
        readyForTimeline: true,
        probe: "succeeded",
        thumbnail: "succeeded",
        waveform: "running",
        transcript: "pending",
        understanding: "pending",
      },
    });
    expect(processingView(processing)).toMatchObject({
      label: "可编辑，文字稿生成中",
      stage: "正在提取波形",
    });
    expect(mediaMenuAvailability(processing).addToTimeline).toBe(true);

    const partial = media({
      processing: {
        status: "partial",
        readyForTimeline: true,
        probe: "succeeded",
        thumbnail: "failed",
        waveform: "succeeded",
        transcript: "failed",
        understanding: "succeeded",
        error: "capability timed out",
      },
    });
    expect(processingView(partial)).toMatchObject({
      label: "部分解析失败",
      failedStages: ["缩略图生成失败", "文字稿生成失败"],
      error: "capability timed out",
    });
    expect(mediaMenuAvailability(partial)).toMatchObject({
      addToTimeline: true,
      retry: true,
    });
  });

  it.each([
    ["processing", false, "解析中"],
    ["ready", true, "准备完成"],
    ["partial", true, "部分解析失败"],
    ["failed", false, "解析失败"],
  ] as const)("maps %s status", (status, readyForTimeline, label) => {
    const target = media({
      processing: {
        status,
        readyForTimeline,
        probe: status === "processing" ? "running" : status === "failed" ? "failed" : "succeeded",
        thumbnail: "skipped",
        waveform: "skipped",
        transcript: "skipped",
        understanding: "skipped",
      },
    });
    expect(processingView(target)?.label).toBe(label);
    expect(mediaMenuAvailability(target).addToTimeline).toBe(readyForTimeline);
  });

  it("polls only while at least one stage is pending or running", () => {
    expect(
      mediaListNeedsProcessingPoll([
        media({
          processing: {
            status: "partial",
            readyForTimeline: true,
            probe: "succeeded",
            thumbnail: "failed",
            waveform: "succeeded",
            transcript: "failed",
            understanding: "succeeded",
          },
        }),
      ]),
    ).toBe(false);
    expect(
      mediaListNeedsProcessingPoll([
        media({
          processing: {
            status: "partial",
            readyForTimeline: true,
            probe: "succeeded",
            thumbnail: "failed",
            waveform: "succeeded",
            transcript: "running",
            understanding: "succeeded",
          },
        }),
      ]),
    ).toBe(true);
  });
});

describe("media processing poller", () => {
  it("stops scheduling after refresh reaches terminal state", async () => {
    let current = [
      media({
        processing: {
          status: "processing",
          readyForTimeline: true,
          probe: "succeeded",
          thumbnail: "running",
          waveform: "pending",
          transcript: "pending",
          understanding: "pending",
        },
      }),
    ];
    const scheduled: (() => void)[] = [];
    const refresh = vi.fn(async () => {
      current = [
        media({
          processing: {
            status: "ready",
            readyForTimeline: true,
            probe: "succeeded",
            thumbnail: "succeeded",
            waveform: "succeeded",
            transcript: "succeeded",
            understanding: "succeeded",
          },
        }),
      ];
    });
    startMediaProcessingPoller({
      getMedia: () => current,
      refresh,
      schedule: (callback) => {
        scheduled.push(callback);
        return 1 as unknown as ReturnType<typeof setTimeout>;
      },
      cancel: vi.fn(),
    });

    expect(scheduled).toHaveLength(1);
    scheduled.shift()?.();
    await Promise.resolve();
    await Promise.resolve();
    expect(refresh).toHaveBeenCalledOnce();
    expect(scheduled).toHaveLength(0);
  });

  it("cancels the pending timer when its owner unmounts", () => {
    const cancel = vi.fn();
    const stop = startMediaProcessingPoller({
      getMedia: () => [
        media({
          processing: {
            status: "processing",
            readyForTimeline: false,
            probe: "running",
            thumbnail: "pending",
            waveform: "pending",
            transcript: "pending",
            understanding: "pending",
          },
        }),
      ],
      refresh: vi.fn(async () => {}),
      schedule: () => 9 as unknown as ReturnType<typeof setTimeout>,
      cancel,
    });
    stop();
    expect(cancel).toHaveBeenCalledWith(9);
  });

  it("supports a forced first poll while retry still exposes the old terminal state", async () => {
    let forced = true;
    const scheduled: (() => void)[] = [];
    const refresh = vi.fn(async () => {
      forced = false;
    });
    startMediaProcessingPoller({
      getMedia: () => [
        media({
          processing: {
            status: "partial",
            readyForTimeline: true,
            probe: "succeeded",
            thumbnail: "failed",
            waveform: "succeeded",
            transcript: "failed",
            understanding: "failed",
          },
        }),
      ],
      shouldPoll: () => forced,
      refresh,
      schedule: (callback) => {
        scheduled.push(callback);
        return 1 as unknown as ReturnType<typeof setTimeout>;
      },
      cancel: vi.fn(),
    });
    expect(scheduled).toHaveLength(1);
    scheduled.shift()?.();
    await Promise.resolve();
    await Promise.resolve();
    expect(refresh).toHaveBeenCalledOnce();
    expect(scheduled).toHaveLength(0);
  });
});
