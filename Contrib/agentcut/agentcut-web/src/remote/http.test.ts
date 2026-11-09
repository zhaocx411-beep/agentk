import { afterEach, describe, expect, it, vi } from "vitest";

import { unwrapAgentUserMessage } from "./client";
import { HttpCutApiClient } from "./http";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("HttpCutApiClient agent messages", () => {
  it("forwards editor context without changing existing message fields", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ executionId: "exe_1" }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new HttpCutApiClient({
      baseUrl: "https://agentcut.test",
      user: "alice",
    });

    await client.sendMessage("project/a", {
      text: "剪短一点",
      profileId: "agentcut-default",
      mediaIds: ["media-a"],
      editorContext: {
        timelineRevision: 3,
        selectedItemIds: ["clip-a"],
        playheadFrame: 25,
      },
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(
      "https://agentcut.test/v1/projects/project%2Fa/messages",
    );
    expect(JSON.parse(init.body as string)).toEqual({
      text: "剪短一点",
      profileId: "agentcut-default",
      mediaIds: ["media-a"],
      editorContext: {
        timelineRevision: 3,
        selectedItemIds: ["clip-a"],
        playheadFrame: 25,
      },
    });
    expect(new Headers(init.headers).get("X-AgentCut-User")).toBe("alice");
  });

  it("unwraps only a complete backend context envelope", () => {
    const message =
      '[AGENTCUT_SYSTEM_CONTEXT_V1]\n{"timelineRevision":3}\n' +
      "[/AGENTCUT_SYSTEM_CONTEXT_V1]\n[AGENTCUT_USER_MESSAGE]\n剪短一点\n" +
      "[/AGENTCUT_USER_MESSAGE]";

    expect(unwrapAgentUserMessage(message)).toBe("剪短一点");
    expect(unwrapAgentUserMessage("普通消息")).toBe("普通消息");
  });
});

describe("HttpCutApiClient timeline migration", () => {
  it("wraps a legacy single timeline response as 剪辑 1", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            revision: 4,
            durationInFrames: 90,
            doc: {
              schemaVersion: 1,
              metadata: { name: "旧项目", width: 1920, height: 1080, fps: 30 },
              media: [],
              tracks: [{ trackId: "v1", kind: "video" }],
              items: [],
            },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const snapshot = await new HttpCutApiClient().getTimeline("legacy-project");

    expect(snapshot.activeTimelineId).toBe("tl_legacy_legacy-project");
    expect(snapshot.timelines).toEqual([
      expect.objectContaining({
        timelineId: "tl_legacy_legacy-project",
        name: "剪辑 1",
        revision: 4,
        durationInFrames: 90,
      }),
    ]);
  });

  it("aborts a stalled timeline lifecycle request instead of locking the UI forever", async () => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((_url: string, init: RequestInit) => {
        return new Promise((_resolve, reject) => {
          init.signal?.addEventListener(
            "abort",
            () => reject(new DOMException("aborted", "AbortError")),
            { once: true },
          );
        });
      }),
    );
    const client = new HttpCutApiClient();
    const result = expect(
      client.deleteTimeline("project", "timeline", { requestId: "delete-1" }),
    ).rejects.toThrow("请求超时");

    await vi.advanceTimersByTimeAsync(10_000);
    await result;
  });
});
