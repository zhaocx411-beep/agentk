// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TopBar } from "@/components/TopBar/TopBar";
import { newDoc, type TimelineDoc } from "@/domain/timeline/types";
import { api } from "@/remote";
import { useProjectStore } from "@/store/projectStore";
import { useRenderStore } from "@/store/renderStore";
import { useTimelineStore } from "@/store/timelineStore";

import { ExportDialog } from "./ExportDialog";
import { validateExport } from "./exportValidation";

function validDoc(): TimelineDoc {
  const doc = newDoc("测试项目", 1920, 1080, 30);
  doc.tracks.push({ trackId: "video-1", kind: "video" });
  doc.media.push({
    mediaId: "media-1",
    kind: "video",
    uri: "/media/source.mp4",
    durationInFrames: 300,
  });
  doc.items.push({
    itemId: "clip-1",
    trackId: "video-1",
    mediaId: "media-1",
    kind: "video",
    from: 0,
    durationInFrames: 300,
  });
  return doc;
}

function setTimeline(doc: TimelineDoc, pending = false): void {
  useTimelineStore.setState({
    doc,
    localDoc: doc,
    confirmedRevision: 12,
    pendingOps: pending
      ? [
          {
            requestId: "pending-1",
            baseRevision: 12,
            operations: [],
            status: "sending",
          },
        ]
      : [],
    syncStatus: pending ? "pending" : "synced",
    gestureActive: false,
    agentHighlightItemIds: [],
  });
}

beforeEach(() => {
  vi.restoreAllMocks();
  useRenderStore.getState().reset();
  const project = {
    projectId: "project-a",
    title: "测试项目",
    createdAtUnixMs: 1,
  };
  useProjectStore.setState({
    project,
    projects: [project],
    loading: false,
    bootstrapError: null,
  });
  setTimeline(validDoc());
});

afterEach(() => {
  cleanup();
  useRenderStore.getState().reset();
  vi.useRealTimers();
});

describe("export validation", () => {
  it("rejects an empty timeline, a timeline without video, and invalid metadata", () => {
    const empty = newDoc("空项目", 1920, 1080, 30);
    expect(validateExport(empty).errors).toEqual(
      expect.arrayContaining(["时间线为空，请先添加片段", "时间线缺少视频片段"]),
    );

    const audioOnly = newDoc("音频项目", 1920, 1080, 30);
    audioOnly.tracks.push({ trackId: "audio-1", kind: "audio" });
    audioOnly.items.push({
      itemId: "audio-clip",
      trackId: "audio-1",
      mediaId: "audio-media",
      kind: "audio",
      from: 0,
      durationInFrames: 60,
    });
    expect(validateExport(audioOnly).errors).toContain("时间线缺少视频片段");
    expect(validateExport(audioOnly).errors).not.toContain("时间线为空，请先添加片段");

    const badMetadata = validDoc();
    badMetadata.metadata.width = 0;
    badMetadata.metadata.fps = 29;
    expect(validateExport(badMetadata).errors).toContain(
      "项目 metadata 无效，请检查画幅和 fps",
    );
  });

  it("rejects invalid M3A values and explains executor/preview approximations", () => {
    const doc = validDoc();
    doc.items[0] = {
      ...doc.items[0],
      volume: 1.5,
      fadeInFrames: 200,
      fadeOutFrames: 200,
      transform: { x: 0.8, y: 0, width: 0.4, height: 1, fit: "contain" },
    };
    const result = validateExport(doc);
    expect(result.errors).toEqual(
      expect.arrayContaining([
        expect.stringContaining("淡入淡出参数无效"),
        expect.stringContaining("画中画参数无效"),
      ]),
    );
    expect(result.warnings.join(" ")).toContain("预览会近似");
    expect(result.warnings.join(" ")).toContain("Agent-K Executor");
  });

  it("uses shared M3B validation and prompts for Local Executor", () => {
    const doc = validDoc();
    doc.items[0].transform = {
      x: 0,
      y: 0,
      width: 1,
      height: 1,
      fit: "contain",
      crop: { x: 0.8, y: 0, width: 0.4, height: 1 },
      rotationDegrees: 45,
    };
    doc.items.push({
      itemId: "caption",
      trackId: "v1",
      kind: "text",
      from: 0,
      durationInFrames: 30,
      text: "字幕",
      captionStyle: { fontFamily: "/tmp/font.ttf" as "sans" },
    });

    const result = validateExport(doc);

    expect(result.errors.join(" ")).toContain("裁剪区域无效");
    expect(result.errors.join(" ")).toContain("字幕字体");
    expect(result.warnings.join(" ")).toContain("Local Executor");
  });
});

describe("ExportDialog", () => {
  it("blocks start while pending ops are syncing", () => {
    setTimeline(validDoc(), true);
    render(<ExportDialog open onOpenChange={() => {}} />);

    expect(screen.getByText("等待修改同步完成")).toBeTruthy();
    expect(screen.getByRole("button", { name: "开始导出" }).hasAttribute("disabled")).toBe(true);
  });

  it("shows completed video, download link, warnings, and accessible icon tooltip", () => {
    useRenderStore.setState({
      phase: "completed",
      projectId: "project-a",
      renderKey: "render-1",
      revision: 12,
      progress: { done: 3, total: 3 },
      outputUri: "/renders/result.mp4",
      warnings: ["字幕字体已替换"],
      workflowStatus: null,
      serverError: null,
      errorMessage: null,
    });

    render(<ExportDialog open onOpenChange={() => {}} />);

    const video = document.querySelector("video");
    expect(video?.getAttribute("src")).toBe("/renders/result.mp4");
    expect(video?.hasAttribute("controls")).toBe(true);
    expect(video?.hasAttribute("playsinline")).toBe(true);
    expect(screen.getByRole("link", { name: "下载成片" }).getAttribute("href")).toBe(
      "/renders/result.mp4?download=1",
    );
    expect(screen.getByText("字幕字体已替换")).toBeTruthy();

    const close = screen.getByRole("button", { name: "关闭导出窗口" });
    expect(close.getAttribute("title")).toBe("关闭导出窗口");
    expect(close.getAttribute("aria-label")).toBe("关闭导出窗口");
  });

  it("shows server error/workflowStatus and retries", async () => {
    const startRender = vi.spyOn(api, "startRender").mockResolvedValue({
      renderKey: "render-1",
      revision: 12,
      status: "accepted",
    });
    useRenderStore.setState({
      phase: "failed",
      projectId: "project-a",
      renderKey: "render-1",
      revision: 12,
      progress: null,
      outputUri: null,
      warnings: ["音频峰值过高"],
      workflowStatus: "FAILED",
      serverError: "ffmpeg exited with code 1",
      errorMessage: "fallback should not win",
    });

    render(<ExportDialog open onOpenChange={() => {}} />);

    expect(screen.getByText("ffmpeg exited with code 1")).toBeTruthy();
    expect(screen.getByText("Workflow 状态：FAILED")).toBeTruthy();
    expect(screen.getByText("音频峰值过高")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await Promise.resolve();
    expect(startRender).toHaveBeenCalledWith("project-a");
  });

  it("keeps polling after the dialog closes", async () => {
    vi.useFakeTimers();
    vi.spyOn(api, "startRender").mockResolvedValue({
      renderKey: "render-1",
      revision: 12,
      status: "accepted",
    });
    vi.spyOn(api, "getRenderStatus").mockResolvedValue({
      renderKey: "render-1",
      status: "completed",
      outputUri: "/renders/result.mp4",
    });
    useRenderStore.getState().start("project-a");
    await Promise.resolve();
    await Promise.resolve();

    function Harness() {
      const [open, setOpen] = useState(true);
      return <ExportDialog open={open} onOpenChange={setOpen} />;
    }

    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "关闭导出窗口" }));
    expect(screen.queryByRole("dialog")).toBeNull();

    await vi.advanceTimersByTimeAsync(2500);
    expect(useRenderStore.getState()).toMatchObject({
      phase: "completed",
      outputUri: "/renders/result.mp4",
    });
  });

  it("clears failed UI state when closed", () => {
    useRenderStore.setState({
      phase: "failed",
      projectId: "project-a",
      serverError: "boom",
      errorMessage: "boom",
    });
    const onOpenChange = vi.fn();
    render(<ExportDialog open onOpenChange={onOpenChange} />);

    fireEvent.click(screen.getByRole("button", { name: "关闭" }));

    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(useRenderStore.getState().phase).toBe("idle");
  });

  it("gives TopBar icon-only controls tooltips and aria labels", () => {
    render(<TopBar />);

    for (const label of ["重命名项目", "新建项目", "撤销（Cmd+Z）", "重做（Cmd+Shift+Z）"]) {
      const button = screen.getByRole("button", { name: label });
      expect(button.getAttribute("title")).toBe(label);
      expect(button.getAttribute("aria-label")).toBe(label);
    }
  });
});
