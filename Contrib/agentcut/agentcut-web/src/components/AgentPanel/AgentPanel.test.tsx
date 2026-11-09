// @vitest-environment jsdom

import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { newDoc } from "@/domain/timeline/types";
import { useEditorContextStore } from "@/store/editorContextStore";
import { useProjectStore } from "@/store/projectStore";
import { useTimelineStore } from "@/store/timelineStore";

const remoteMocks = vi.hoisted(() => ({
  listProfiles: vi.fn(),
  listEvents: vi.fn(),
  sendMessage: vi.fn(),
  streamEvents: vi.fn(),
  answer: vi.fn(),
}));

vi.mock("@/remote", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/remote")>();
  return {
    ...actual,
    api: {
      listProfiles: remoteMocks.listProfiles,
      listEvents: remoteMocks.listEvents,
      sendMessage: remoteMocks.sendMessage,
      streamEvents: remoteMocks.streamEvents,
      answer: remoteMocks.answer,
    },
  };
});

import { AgentPanel } from "./AgentPanel";

const originalClipboard = navigator.clipboard;

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollTo", {
    configurable: true,
    value: vi.fn(),
  });
  remoteMocks.listProfiles.mockReset();
  remoteMocks.listProfiles.mockResolvedValue([]);
  remoteMocks.listEvents.mockReset();
  remoteMocks.listEvents.mockResolvedValue({ events: [], nextPageToken: "" });
  remoteMocks.sendMessage.mockReset();
  remoteMocks.sendMessage.mockResolvedValue({ executionId: "exe_1" });
  remoteMocks.streamEvents.mockReset();
  remoteMocks.streamEvents.mockReturnValue(() => {});
  remoteMocks.answer.mockReset();

  useProjectStore.setState({
    project: {
      projectId: "p1",
      title: "Project",
      createdAtUnixMs: 1,
      sessionId: "ses_1",
    },
    bootstrapError: null,
  });
  const doc = newDoc("test", 1920, 1080, 30);
  useTimelineStore.setState({ doc, localDoc: doc, confirmedRevision: 9 });
  useEditorContextStore.getState().reset();
  useEditorContextStore.setState({
    timelineRevision: 9,
    selectedItemIds: ["clip-a"],
    playheadFrame: 48,
    selectedMediaId: "media-a",
  });
});

afterEach(() => {
  cleanup();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: originalClipboard,
  });
});

describe("AgentPanel", () => {
  it("centers the empty welcome content vertically", async () => {
    render(<AgentPanel />);

    const title = await screen.findByText("今天想创作什么？");
    const scrollContent = title.parentElement?.parentElement?.parentElement;
    expect(scrollContent?.className).toContain("min-h-full");
    expect(scrollContent?.className).toContain("justify-center");
  });

  it("keeps assistant replies selectable and provides an explicit copy action", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    remoteMocks.listEvents.mockResolvedValue({
      events: [
        {
          eventId: "assistant-1",
          kind: "assistant.text",
          data: { text: "这段回复可以复制" },
        },
      ],
      nextPageToken: "",
    });

    render(<AgentPanel />);

    const message = await screen.findByText("这段回复可以复制");
    expect(message.closest(".group\\/message")?.className).toContain(
      "select-text",
    );
    fireEvent.click(screen.getByRole("button", { name: "复制回复" }));

    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith("这段回复可以复制"),
    );
    expect(screen.getByRole("button", { name: "已复制" })).toBeTruthy();
  });

  it("does not auto-scroll while assistant text is selected", async () => {
    remoteMocks.listEvents.mockResolvedValue({
      events: [
        {
          eventId: "assistant-selection",
          kind: "assistant.text",
          data: { text: "正在选择的历史回复" },
        },
      ],
      nextPageToken: "",
    });
    render(<AgentPanel />);
    const message = await screen.findByText("正在选择的历史回复");
    const viewport = document.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]',
    )!;
    const scrollTo = viewport.scrollTo as ReturnType<typeof vi.fn>;
    scrollTo.mockClear();

    const range = document.createRange();
    range.selectNodeContents(message);
    const selection = document.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));

    fireEvent.change(screen.getByPlaceholderText("告诉我你想怎么剪…"), {
      target: { value: "继续处理" },
    });
    fireEvent.click(screen.getByTitle("发送"));
    await screen.findByText("继续处理");

    expect(scrollTo).not.toHaveBeenCalled();
    selection.removeAllRanges();
    document.dispatchEvent(new Event("selectionchange"));
  });

  it("scrolls directly to the latest message after the user sends", async () => {
    render(<AgentPanel />);
    const viewport = document.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]',
    )!;
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 600 },
      scrollTop: { configurable: true, writable: true, value: 0 },
      clientHeight: { configurable: true, value: 200 },
    });
    fireEvent.scroll(viewport);
    const scrollTo = viewport.scrollTo as ReturnType<typeof vi.fn>;
    scrollTo.mockClear();

    fireEvent.change(screen.getByPlaceholderText("告诉我你想怎么剪…"), {
      target: { value: "发送后显示最新消息" },
    });
    fireEvent.click(screen.getByTitle("发送"));
    await screen.findByText("发送后显示最新消息");

    await waitFor(() =>
      expect(scrollTo).toHaveBeenCalledWith({ top: 600 }),
    );
  });

  it("snapshots structured editor context when sending", async () => {
    render(<AgentPanel />);
    fireEvent.change(screen.getByPlaceholderText("告诉我你想怎么剪…"), {
      target: { value: "调整选中片段" },
    });
    fireEvent.click(screen.getByTitle("发送"));

    const userMessage = await screen.findByText("调整选中片段");
    expect(userMessage.className).toContain("bg-[#ECEDEA]");
    expect(userMessage.className).toContain("text-[#303236]");
    expect(userMessage.className).toContain("border-[#DEE0DC]");

    await waitFor(() =>
      expect(remoteMocks.sendMessage).toHaveBeenCalledWith("p1", {
        text: "调整选中片段",
        profileId: "agentcut-default",
        mediaIds: [],
        editorContext: {
          timelineRevision: 9,
          selectedItemIds: ["clip-a"],
          playheadFrame: 48,
          selectedMediaId: "media-a",
        },
      }),
    );
  });

  it("uses the click-time context attached to a prefilled Agent prompt", async () => {
    render(<AgentPanel />);
    const context = {
      timelineRevision: 9,
      selectedItemIds: ["clip-a"],
      playheadFrame: 48,
      selectedMediaId: "media-a",
    };
    act(() => {
      useEditorContextStore
        .getState()
        .requestAgentPrompt(
          '精确调整\nselectedItemIds=["clip-a"]; playheadFrame=48',
          context,
        );
    });
    const prefilled = (
      screen.getByPlaceholderText("告诉我你想怎么剪…") as HTMLInputElement
    ).value;
    expect(prefilled).toContain('selectedItemIds=["clip-a"]');
    expect(prefilled).toContain("playheadFrame=48");

    act(() => {
      useEditorContextStore.getState().setSelectedItemIds(["clip-b"]);
      useEditorContextStore.getState().setPlayheadFrame(99);
    });
    fireEvent.click(screen.getByTitle("发送"));

    await waitFor(() =>
      expect(remoteMocks.sendMessage).toHaveBeenCalledWith(
        "p1",
        expect.objectContaining({ editorContext: context }),
      ),
    );
  });
});
