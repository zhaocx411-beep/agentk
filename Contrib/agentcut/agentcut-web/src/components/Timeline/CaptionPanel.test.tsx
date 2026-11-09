// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import { usePreviewOverridesStore } from "@/store/previewOverridesStore";

import { CaptionPanel } from "./CaptionPanel";

afterEach(() => {
  cleanup();
  usePreviewOverridesStore.getState().reset();
});

function fixture() {
  const doc = newDoc("captions", 1920, 1080, 30);
  doc.tracks.push({ trackId: "c1", kind: "caption" });
  doc.items.push(
    {
      itemId: "first",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 30,
      text: "第一句",
    },
    {
      itemId: "second",
      trackId: "c1",
      kind: "text",
      from: 60,
      durationInFrames: 30,
      text: "第二句",
    },
  );
  return doc;
}

describe("CaptionPanel", () => {
  it("adds a caption and creates its caption track when needed", () => {
    const doc = newDoc("captions", 1920, 1080, 30);
    const onSubmit = vi.fn();
    render(
      <CaptionPanel
        doc={doc}
        item={null}
        newCaptionTrackId="c1"
        initialFrame={45}
        onClose={vi.fn()}
        onSubmit={onSubmit}
      />,
    );

    expect(screen.getByRole("dialog", { name: "新增字幕" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("字幕文字"), {
      target: { value: "手动新增" },
    });
    fireEvent.click(screen.getByRole("button", { name: "新增字幕" }));

    expect(onSubmit).toHaveBeenCalledWith([
      {
        op: "addTrack",
        track: { trackId: "c1", kind: "caption", name: "字幕轨" },
      },
      {
        op: "addItem",
        item: expect.objectContaining({
          itemId: expect.stringMatching(/^it_/),
          trackId: "c1",
          kind: "text",
          from: 45,
          durationInFrames: 60,
          text: "手动新增",
        }),
      },
    ]);
  });

  it("previews and saves one caption with text, timing and style", () => {
    const doc = fixture();
    const onSubmit = vi.fn();
    render(
      <CaptionPanel
        doc={doc}
        item={doc.items[0]}
        onClose={vi.fn()}
        onSubmit={onSubmit}
      />,
    );

    fireEvent.change(screen.getByLabelText("字幕文字"), {
      target: { value: "更新文字" },
    });
    fireEvent.change(screen.getByLabelText("起点（秒）"), {
      target: { value: "1" },
    });
    fireEvent.change(screen.getByLabelText("时长（秒）"), {
      target: { value: "2" },
    });
    fireEvent.change(screen.getByLabelText("字幕模板"), {
      target: { value: "subtitle-outline" },
    });
    fireEvent.change(screen.getByLabelText("字号"), {
      target: { value: "48" },
    });

    expect(usePreviewOverridesStore.getState().itemPatches.first).toMatchObject({
      text: "更新文字",
      stylePresetId: "subtitle-outline",
      captionStyle: { fontSize: 48 },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存当前字幕" }));

    expect(onSubmit).toHaveBeenCalledWith([
      { op: "moveItem", itemId: "first", from: 30 },
      { op: "trimEnd", itemId: "first", frame: 90 },
      {
        op: "updateItem",
        itemId: "first",
        patch: {
          text: "更新文字",
          stylePresetId: "subtitle-outline",
          captionStyle: { fontSize: 48 },
        },
      },
    ]);
  });

  it("applies preset and style to the whole caption track", () => {
    const doc = fixture();
    const onSubmit = vi.fn();
    render(
      <CaptionPanel
        doc={doc}
        item={doc.items[0]}
        onClose={vi.fn()}
        onSubmit={onSubmit}
      />,
    );

    fireEvent.change(screen.getByLabelText("字幕模板"), {
      target: { value: "subtitle-bold" },
    });
    fireEvent.change(screen.getByLabelText("字幕对齐"), {
      target: { value: "left" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "应用到全部字幕" }),
    );

    expect(onSubmit).toHaveBeenCalledWith([
      {
        op: "setCaptions",
        trackId: "c1",
        captions: [
          expect.objectContaining({
            text: "第一句",
            stylePresetId: "subtitle-bold",
            captionStyle: { align: "left" },
          }),
          expect.objectContaining({
            text: "第二句",
            stylePresetId: "subtitle-bold",
            captionStyle: { align: "left" },
          }),
        ],
      },
    ]);
  });
});
