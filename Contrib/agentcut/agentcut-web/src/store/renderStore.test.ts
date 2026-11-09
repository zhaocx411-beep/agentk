import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import { ApiError, api } from "@/remote";
import type { RenderStatus } from "@/remote";

import { useProjectStore } from "./projectStore";
import { useRenderStore } from "./renderStore";

const accepted = { renderKey: "render-same", revision: 7, status: "accepted" as const };

async function flushPromises(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.restoreAllMocks();
  useRenderStore.getState().reset();
});

afterEach(() => {
  useRenderStore.getState().reset();
  vi.useRealTimers();
});

describe("renderStore", () => {
  it("runs start → running → completed and preserves server fields", async () => {
    vi.spyOn(api, "startRender").mockResolvedValue(accepted);
    vi.spyOn(api, "getRenderStatus")
      .mockResolvedValueOnce({
        renderKey: accepted.renderKey,
        status: "running",
        progress: { done: 1, total: 3, current: "合成视频" },
      })
      .mockResolvedValueOnce({
        renderKey: accepted.renderKey,
        status: "completed",
        revision: 7,
        progress: { done: 3, total: 3, current: "完成" },
        outputUri: "/renders/out.mp4",
        warnings: ["字幕字体已替换"],
      });

    useRenderStore.getState().start("project-a");
    await flushPromises();
    expect(useRenderStore.getState()).toMatchObject({
      phase: "running",
      projectId: "project-a",
      renderKey: "render-same",
      revision: 7,
    });

    await vi.advanceTimersByTimeAsync(2500);
    expect(useRenderStore.getState().progress).toEqual({
      done: 1,
      total: 3,
      current: "合成视频",
    });

    await vi.advanceTimersByTimeAsync(2500);
    expect(useRenderStore.getState()).toMatchObject({
      phase: "completed",
      outputUri: "/renders/out.mp4",
      warnings: ["字幕字体已替换"],
    });
  });

  it("surfaces a backend 422 error from start", async () => {
    vi.spyOn(api, "startRender").mockRejectedValue(
      new ApiError(422, { error: "timeline has no renderable video", code: "BAD_REQUEST" }),
    );

    useRenderStore.getState().start("project-a");
    await flushPromises();

    expect(useRenderStore.getState()).toMatchObject({
      phase: "failed",
      serverError: "timeline has no renderable video",
      errorMessage: "timeline has no renderable video",
    });
  });

  it("turns a polling network failure into a stable failed state", async () => {
    vi.spyOn(api, "startRender").mockResolvedValue(accepted);
    vi.spyOn(api, "getRenderStatus").mockRejectedValue(new Error("network unavailable"));

    useRenderStore.getState().start("project-a");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2500);

    expect(useRenderStore.getState()).toMatchObject({
      phase: "failed",
      errorMessage: "network unavailable",
    });
  });

  it("prefers status.error and keeps workflowStatus and warnings", async () => {
    vi.spyOn(api, "startRender").mockResolvedValue(accepted);
    vi.spyOn(api, "getRenderStatus").mockResolvedValue({
      renderKey: accepted.renderKey,
      status: "failed",
      error: "ffmpeg exited with code 1",
      workflowStatus: "TIMED_OUT",
      warnings: ["音频轨已静音"],
    });

    useRenderStore.getState().start("project-a");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2500);

    expect(useRenderStore.getState()).toMatchObject({
      phase: "failed",
      serverError: "ffmpeg exited with code 1",
      errorMessage: "ffmpeg exited with code 1",
      workflowStatus: "TIMED_OUT",
      warnings: ["音频轨已静音"],
    });
  });

  it("does not overlap polling requests", async () => {
    let resolveFirst!: (status: RenderStatus) => void;
    const first = new Promise<RenderStatus>((resolve) => {
      resolveFirst = resolve;
    });
    vi.spyOn(api, "startRender").mockResolvedValue(accepted);
    const getStatus = vi
      .spyOn(api, "getRenderStatus")
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce({
        renderKey: accepted.renderKey,
        status: "completed",
        outputUri: "/renders/out.mp4",
      });

    useRenderStore.getState().start("project-a");
    await flushPromises();
    vi.advanceTimersByTime(2500);
    expect(getStatus).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(10_000);
    expect(getStatus).toHaveBeenCalledTimes(1);

    resolveFirst({
      renderKey: accepted.renderKey,
      status: "running",
      progress: { done: 1, total: 2 },
    });
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2500);
    expect(getStatus).toHaveBeenCalledTimes(2);
  });

  it("ignores a stale callback after project-switch reset", async () => {
    let resolveStatus!: (status: RenderStatus) => void;
    vi.spyOn(api, "startRender").mockResolvedValue(accepted);
    vi.spyOn(api, "getRenderStatus").mockReturnValue(
      new Promise<RenderStatus>((resolve) => {
        resolveStatus = resolve;
      }),
    );
    const nextDoc = newDoc("next", 1920, 1080, 30);
    vi.spyOn(api, "getTimeline").mockResolvedValue({
      doc: nextDoc,
      revision: 1,
      durationInFrames: 0,
    });
    const projectA = { projectId: "project-a", title: "A", createdAtUnixMs: 1 };
    const projectB = { projectId: "project-b", title: "B", createdAtUnixMs: 2 };
    useProjectStore.setState({
      project: projectA,
      projects: [projectA, projectB],
      loading: false,
      bootstrapError: null,
    });

    useRenderStore.getState().start("project-a");
    await flushPromises();
    vi.advanceTimersByTime(2500);
    await useProjectStore.getState().switchProject("project-b");

    resolveStatus({
      renderKey: accepted.renderKey,
      status: "completed",
      outputUri: "/renders/old-project.mp4",
    });
    await flushPromises();

    expect(useRenderStore.getState()).toMatchObject({
      phase: "idle",
      projectId: null,
      outputUri: null,
    });
    expect(useProjectStore.getState().project?.projectId).toBe("project-b");
  });

  it("retries with the same renderKey after a server failure", async () => {
    vi.spyOn(api, "startRender").mockResolvedValue(accepted);
    vi.spyOn(api, "getRenderStatus")
      .mockResolvedValueOnce({
        renderKey: accepted.renderKey,
        status: "failed",
        error: "temporary renderer failure",
        workflowStatus: "FAILED",
      })
      .mockResolvedValueOnce({
        renderKey: accepted.renderKey,
        status: "completed",
        outputUri: "/renders/reused.mp4",
      });

    useRenderStore.getState().start("project-a");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(2500);
    expect(useRenderStore.getState().phase).toBe("failed");

    useRenderStore.getState().start("project-a");
    await flushPromises();
    expect(useRenderStore.getState().renderKey).toBe(accepted.renderKey);
    await vi.advanceTimersByTimeAsync(2500);
    expect(useRenderStore.getState()).toMatchObject({
      phase: "completed",
      renderKey: accepted.renderKey,
      outputUri: "/renders/reused.mp4",
    });
  });
});
