// 改写自后端 timeline/apply_test.go 中 M1 op 集合相关的场景，
// 保证 TS 版 ApplyOps 与 Go 版对同一批 ops 产出一致结果。
import { describe, expect, it } from "vitest";

import { applyOps, deriveSplitID, validateDoc } from "./applyOps";
import type { Item, Operation, TimelineDoc } from "./types";
import {
  MAX_OPS_PER_BATCH,
  OpError,
  docDurationInFrames,
  newDoc,
  sourceFramesConsumed,
} from "./types";

// baseDoc：1080p30，300 帧视频素材 + 600 帧音频素材 + 图片，
// 轨道 v1(视频)、a1(音频)、c1(字幕)、vlocked(锁定视频轨)。
function baseDoc(): TimelineDoc {
  const d = newDoc("test", 1920, 1080, 30);
  d.media = [
    { mediaId: "m_video", uri: "blob://v", kind: "video", durationInFrames: 300 },
    { mediaId: "m_audio", uri: "blob://a", kind: "audio", durationInFrames: 600 },
    { mediaId: "m_image", uri: "blob://i", kind: "image", durationInFrames: 0 },
  ];
  d.tracks = [
    { trackId: "v1", kind: "video" },
    { trackId: "a1", kind: "audio" },
    { trackId: "c1", kind: "caption" },
    { trackId: "vlocked", kind: "video", locked: true },
  ];
  return d;
}

// docWithClip：v1 轨上放一段 [100, 220)、trimStart=30 的视频片段。
function docWithClip(): TimelineDoc {
  const d = baseDoc();
  d.items = [
    {
      itemId: "it1",
      trackId: "v1",
      kind: "video",
      mediaId: "m_video",
      from: 100,
      durationInFrames: 120,
      trimStart: 30,
    },
  ];
  return d;
}

function mustFail(d: TimelineDoc, code: string, ...ops: Operation[]): OpError {
  try {
    applyOps(d, ops);
  } catch (err) {
    expect(err).toBeInstanceOf(OpError);
    const oe = err as OpError;
    expect(oe.code, `unexpected error: ${oe.message}`).toBe(code);
    return oe;
  }
  throw new Error(`expected ${code}, but applyOps succeeded`);
}

describe("batch guards", () => {
  it("rejects empty / oversized batches and bad schema", () => {
    const d = baseDoc();
    mustFail(d, "BAD_REQUEST"); // 空批
    const ops: Operation[] = Array.from({ length: MAX_OPS_PER_BATCH + 1 }, () => ({
      op: "setMetadata" as const,
      metadata: { name: "x" },
    }));
    mustFail(d, "LIMIT_EXCEEDED", ...ops);

    const bad = { ...d, schemaVersion: 99 };
    mustFail(bad, "UNSUPPORTED", { op: "setMetadata", metadata: { name: "x" } });
    mustFail(d, "UNSUPPORTED", { op: "noSuchOp" } as unknown as Operation);
  });

  it("rejects ops not ported to the frontend (server-side only)", () => {
    const d = baseDoc();
    // addMedia 只会由服务端（上传/Agent 工具）产生，前端不移植。
    mustFail(d, "UNSUPPORTED", {
      op: "addMedia",
      media: { mediaId: "m_new", uri: "blob://x", kind: "video", durationInFrames: 10 },
    } as Operation);
  });
});

describe("M3A item fields", () => {
  it("applies mute, fades, and transform through updateItem", () => {
    const d = docWithClip();
    const transform = {
      x: 0.55,
      y: 0.05,
      width: 0.4,
      height: 0.4,
      opacity: 0.8,
      fit: "cover" as const,
    };
    const { doc } = applyOps(d, [
      {
        op: "updateItem",
        itemId: "it1",
        patch: {
          speed: 1.5,
          volume: 0.5,
          muted: true,
          fadeInFrames: 10,
          fadeOutFrames: 20,
          transform,
        },
      },
    ]);
    expect(doc.items[0]).toMatchObject({
      speed: 1.5,
      volume: 0.5,
      muted: true,
      fadeInFrames: 10,
      fadeOutFrames: 20,
      transform,
    });
  });

  it("rejects invalid fades and transforms", () => {
    const d = docWithClip();
    mustFail(d, "OUT_OF_RANGE", {
      op: "updateItem",
      itemId: "it1",
      patch: { fadeInFrames: 100, fadeOutFrames: 30 },
    });
    mustFail(d, "OUT_OF_RANGE", {
      op: "updateItem",
      itemId: "it1",
      patch: {
        transform: { x: 0.8, y: 0, width: 0.3, height: 1, fit: "contain" },
      },
    });
  });
});

describe("purity and determinism", () => {
  it("does not mutate the input doc on success or failure", () => {
    const d = docWithClip();
    const before = JSON.stringify(d);

    applyOps(d, [{ op: "split", itemId: "it1", frame: 160 }]);
    expect(JSON.stringify(d)).toBe(before);

    expect(() =>
      applyOps(d, [{ op: "removeItems", itemIds: ["nope"] }]),
    ).toThrow(OpError);
    expect(JSON.stringify(d)).toBe(before);
  });

  it("is deterministic", () => {
    const d = docWithClip();
    const ops: Operation[] = [
      { op: "split", itemId: "it1", frame: 150 },
      { op: "moveItem", itemId: "it1~150", from: 400 },
    ];
    const a = applyOps(d, ops);
    const b = applyOps(d, ops);
    expect(JSON.stringify(a.doc)).toBe(JSON.stringify(b.doc));
  });
});

describe("addItem", () => {
  it("adds a valid video item", () => {
    const d = baseDoc();
    const { doc, summary } = applyOps(d, [
      {
        op: "addItem",
        item: {
          itemId: "it_a",
          trackId: "v1",
          kind: "video",
          mediaId: "m_video",
          from: 0,
          durationInFrames: 300,
        },
      },
    ]);
    expect(doc.items).toHaveLength(1);
    expect(summary.addedItemIds).toEqual(["it_a"]);
  });

  it("rejects invalid items with the right codes", () => {
    const d = baseDoc();
    const mk = (mut: (it: Item) => void): Operation => {
      const it: Item = {
        itemId: "it_x",
        trackId: "v1",
        kind: "video",
        mediaId: "m_video",
        from: 0,
        durationInFrames: 100,
      };
      mut(it);
      return { op: "addItem", item: it };
    };
    mustFail(d, "NOT_FOUND", mk((it) => (it.trackId = "nope")));
    mustFail(d, "LOCKED_TRACK", mk((it) => (it.trackId = "vlocked")));
    mustFail(d, "KIND_MISMATCH", mk((it) => (it.trackId = "a1"))); // 视频进音轨
    mustFail(d, "KIND_MISMATCH", mk((it) => (it.mediaId = "m_audio"))); // 引音频素材
    mustFail(d, "NOT_FOUND", mk((it) => (it.mediaId = "nope")));
    mustFail(d, "OUT_OF_RANGE", mk((it) => (it.durationInFrames = 301))); // 超源长
    mustFail(
      d,
      "OUT_OF_RANGE",
      mk((it) => {
        it.trimStart = 250;
        it.durationInFrames = 100;
      }),
    );
    mustFail(d, "OUT_OF_RANGE", mk((it) => (it.from = -1)));
    mustFail(d, "BAD_REQUEST", mk((it) => (it.speed = 20)));
    mustFail(d, "BAD_REQUEST", mk((it) => (it.volume = 3)));
  });

  it("accounts speed into source consumption", () => {
    const d = baseDoc();
    const mk = (dur: number): Operation => ({
      op: "addItem",
      item: {
        itemId: "it_x",
        trackId: "v1",
        kind: "video",
        mediaId: "m_video",
        from: 0,
        durationInFrames: dur,
        speed: 2,
      },
    });
    applyOps(d, [mk(150)]); // 2x 速播 150 帧消耗 300 源帧，正好
    mustFail(d, "OUT_OF_RANGE", mk(151));
  });

  it("allows unbounded image duration and validates text items", () => {
    const d = baseDoc();
    applyOps(d, [
      {
        op: "addItem",
        item: {
          itemId: "it_img",
          trackId: "v1",
          kind: "image",
          mediaId: "m_image",
          from: 0,
          durationInFrames: 9999,
        },
      },
    ]);
    applyOps(d, [
      {
        op: "addItem",
        item: {
          itemId: "it_t",
          trackId: "c1",
          kind: "text",
          from: 0,
          durationInFrames: 60,
          text: "你好",
        },
      },
    ]);
    mustFail(d, "BAD_REQUEST", {
      op: "addItem",
      item: { itemId: "it_t2", trackId: "c1", kind: "text", from: 0, durationInFrames: 60, text: "  " },
    });
    mustFail(d, "BAD_REQUEST", {
      op: "addItem",
      item: {
        itemId: "it_t3",
        trackId: "c1",
        kind: "text",
        mediaId: "m_video",
        from: 0,
        durationInFrames: 60,
        text: "x",
      },
    });
    mustFail(docWithClip(), "CONFLICTING_ID", {
      op: "addItem",
      item: { itemId: "it1", trackId: "v1", kind: "video", mediaId: "m_video", from: 500, durationInFrames: 10 },
    });
  });
});

describe("removeItems", () => {
  it("removes items and reports them", () => {
    const d = docWithClip();
    const { doc, summary } = applyOps(d, [{ op: "removeItems", itemIds: ["it1"] }]);
    expect(doc.items).toHaveLength(0);
    expect(summary.removedItemIds).toEqual(["it1"]);

    mustFail(d, "NOT_FOUND", { op: "removeItems", itemIds: ["it1", "nope"] });
    mustFail(d, "BAD_REQUEST", { op: "removeItems", itemIds: [] });
  });

  it("refuses to remove items on locked tracks", () => {
    const locked = docWithClip();
    locked.items[0].trackId = "vlocked";
    mustFail(locked, "LOCKED_TRACK", { op: "removeItems", itemIds: ["it1"] });
  });
});

describe("split", () => {
  it("conserves the source interval", () => {
    const d = docWithClip(); // it1: from=100 dur=120 trim=30，源区间 [30, 150)
    const { doc, summary } = applyOps(d, [{ op: "split", itemId: "it1", frame: 160 }]);
    expect(doc.items).toHaveLength(2);

    const [left, right] = doc.items;
    expect(left.itemId).toBe("it1");
    expect(left.from).toBe(100);
    expect(left.durationInFrames).toBe(60);
    expect(left.trimStart).toBe(30);

    expect(right.itemId).toBe("it1~160");
    expect(right.from).toBe(160);
    expect(right.durationInFrames).toBe(60);
    expect(right.trimStart).toBe(90); // 30 + 60

    expect((left.trimStart ?? 0) + sourceFramesConsumed(left)).toBe(right.trimStart);
    expect(sourceFramesConsumed(left) + sourceFramesConsumed(right)).toBe(120);
    expect(summary.addedItemIds).toContain("it1~160");
  });

  it("requires the split point strictly inside", () => {
    const d = docWithClip();
    mustFail(d, "OUT_OF_RANGE", { op: "split", itemId: "it1", frame: 100 });
    mustFail(d, "OUT_OF_RANGE", { op: "split", itemId: "it1", frame: 220 });
    mustFail(d, "BAD_REQUEST", {
      op: "split",
      itemId: "it1",
    } as unknown as Operation);
  });

  it("supports custom right-half id and speed", () => {
    const d = docWithClip();
    const { doc } = applyOps(d, [
      { op: "split", itemId: "it1", frame: 160, newItemId: "custom" },
    ]);
    expect(doc.items[1].itemId).toBe("custom");

    const fast = baseDoc();
    fast.items = [
      {
        itemId: "fast",
        trackId: "v1",
        kind: "video",
        mediaId: "m_video",
        from: 0,
        durationInFrames: 100,
        speed: 2, // 消耗源 [0, 200)
      },
    ];
    const { doc: doc2 } = applyOps(fast, [{ op: "split", itemId: "fast", frame: 40 }]);
    expect(doc2.items[1].trimStart).toBe(80); // 40 帧 × 2x
    expect(doc2.items[1].durationInFrames).toBe(60);
    expect(deriveSplitID("fast", 40)).toBe("fast~40");
  });
});

describe("trimStart / trimEnd", () => {
  // it1: [100,220) trim=30，源 [30,150)/300
  it("narrows and extends the left edge", () => {
    const d = docWithClip();
    let { doc } = applyOps(d, [{ op: "trimStart", itemId: "it1", frame: 130 }]);
    expect(doc.items[0].from).toBe(130);
    expect(doc.items[0].durationInFrames).toBe(90);
    expect(doc.items[0].trimStart).toBe(60);

    ({ doc } = applyOps(d, [{ op: "trimStart", itemId: "it1", frame: 70 }]));
    expect(doc.items[0].from).toBe(70);
    expect(doc.items[0].durationInFrames).toBe(150);
    expect(doc.items[0].trimStart).toBe(0);

    mustFail(d, "OUT_OF_RANGE", { op: "trimStart", itemId: "it1", frame: 69 });
  });

  it("narrows and extends the right edge", () => {
    const d = docWithClip();
    let { doc } = applyOps(d, [{ op: "trimEnd", itemId: "it1", frame: 370 }]);
    expect(doc.items[0].durationInFrames).toBe(270);
    mustFail(d, "OUT_OF_RANGE", { op: "trimEnd", itemId: "it1", frame: 371 });

    ({ doc } = applyOps(d, [{ op: "trimEnd", itemId: "it1", frame: 150 }]));
    expect(doc.items[0].durationInFrames).toBe(50);
    mustFail(d, "OUT_OF_RANGE", { op: "trimEnd", itemId: "it1", frame: 100 });
  });
});

describe("moveItem", () => {
  it("moves within and across tracks with kind checks", () => {
    const d = docWithClip();
    const { doc } = applyOps(d, [{ op: "moveItem", itemId: "it1", from: 500 }]);
    expect(doc.items[0].from).toBe(500);

    mustFail(d, "KIND_MISMATCH", { op: "moveItem", itemId: "it1", trackId: "a1", from: 0 });
    mustFail(d, "LOCKED_TRACK", { op: "moveItem", itemId: "it1", trackId: "vlocked", from: 0 });
    mustFail(d, "OUT_OF_RANGE", { op: "moveItem", itemId: "it1", from: -1 });

    const d2 = docWithClip();
    d2.tracks.push({ trackId: "v2", kind: "video" });
    const { doc: doc2 } = applyOps(d2, [
      { op: "moveItem", itemId: "it1", trackId: "v2", from: 0 },
    ]);
    expect(doc2.items[0].trackId).toBe("v2");
  });
});

describe("rippleDelete", () => {
  it("handles before/inside/after/head-cut items", () => {
    const d = baseDoc();
    d.items = [
      { itemId: "before", trackId: "v1", kind: "video", mediaId: "m_video", from: 0, durationInFrames: 50 },
      { itemId: "inside", trackId: "v1", kind: "video", mediaId: "m_video", from: 100, durationInFrames: 50 },
      { itemId: "after", trackId: "v1", kind: "video", mediaId: "m_video", from: 200, durationInFrames: 50 },
      { itemId: "aud", trackId: "a1", kind: "audio", mediaId: "m_audio", from: 120, durationInFrames: 300 },
    ];
    // 删除 [100, 150)：inside 整段消失，after 左移 50，aud 头部被裁
    const { doc, summary } = applyOps(d, [
      { op: "rippleDelete", fromFrame: 100, toFrame: 150 },
    ]);
    const byId = new Map(doc.items.map((it) => [it.itemId, it]));
    expect(doc.items).toHaveLength(3);
    expect(byId.get("before")!.from).toBe(0);
    expect(byId.has("inside")).toBe(false);
    expect(byId.get("after")!.from).toBe(150);
    expect(byId.get("aud")!.from).toBe(100);
    expect(byId.get("aud")!.durationInFrames).toBe(270);
    expect(byId.get("aud")!.trimStart).toBe(30);
    expect(summary.removedItemIds).toContain("inside");
  });

  it("splits an item spanning the whole range", () => {
    const d = baseDoc();
    d.items = [
      { itemId: "span", trackId: "v1", kind: "video", mediaId: "m_video", from: 0, durationInFrames: 300 },
    ];
    const { doc } = applyOps(d, [{ op: "rippleDelete", fromFrame: 100, toFrame: 150 }]);
    expect(doc.items).toHaveLength(2);
    const [l, r] = doc.items;
    expect(l.durationInFrames).toBe(100);
    expect(r.itemId).toBe("span~150");
    expect(r.from).toBe(100);
    expect(r.durationInFrames).toBe(150);
    expect(r.trimStart).toBe(150);
  });

  it("truncates a tail overlapping the range", () => {
    const d = baseDoc();
    d.items = [
      { itemId: "tail", trackId: "v1", kind: "video", mediaId: "m_video", from: 50, durationInFrames: 100 },
    ];
    const { doc } = applyOps(d, [{ op: "rippleDelete", fromFrame: 100, toFrame: 200 }]);
    expect(doc.items[0].durationInFrames).toBe(50);
  });

  it("respects locked tracks", () => {
    const d = baseDoc();
    mustFail(d, "LOCKED_TRACK", {
      op: "rippleDelete",
      fromFrame: 0,
      toFrame: 10,
      trackIds: ["vlocked"],
    });
    const dl = baseDoc();
    dl.items = [
      { itemId: "lk", trackId: "vlocked", kind: "video", mediaId: "m_video", from: 200, durationInFrames: 50 },
    ];
    const { doc } = applyOps(dl, [{ op: "rippleDelete", fromFrame: 0, toFrame: 100 }]);
    expect(doc.items[0].from).toBe(200); // 锁定轨不左移
  });

  it("validates the range", () => {
    const d = baseDoc();
    mustFail(d, "OUT_OF_RANGE", { op: "rippleDelete", fromFrame: 100, toFrame: 100 });
    mustFail(d, "BAD_REQUEST", {
      op: "rippleDelete",
      fromFrame: 100,
    } as unknown as Operation);
  });
});

describe("removeTrack", () => {
  it("删除轨道并连带删除轨上 item", () => {
    const { doc, summary } = applyOps(docWithClip(), [
      { op: "removeTrack", trackId: "v1" },
    ]);
    expect(doc.tracks.map((t) => t.trackId)).toEqual(["a1", "c1", "vlocked"]);
    expect(doc.items).toHaveLength(0);
    expect(summary.removedTrackIds).toEqual(["v1"]);
    expect(summary.removedItemIds).toEqual(["it1"]);
  });

  it("锁定轨不能删", () => {
    mustFail(baseDoc(), "LOCKED_TRACK", { op: "removeTrack", trackId: "vlocked" });
  });

  it("不存在的轨道报 NOT_FOUND", () => {
    mustFail(baseDoc(), "NOT_FOUND", { op: "removeTrack", trackId: "nope" });
  });
});

describe("updateTrack", () => {
  it("白名单字段生效", () => {
    const { doc, summary } = applyOps(baseDoc(), [
      {
        op: "updateTrack",
        trackId: "v1",
        trackPatch: { name: "主轨", muted: true, hidden: true, volume: 1.5 },
      },
    ]);
    const t = doc.tracks.find((t) => t.trackId === "v1")!;
    expect(t.name).toBe("主轨");
    expect(t.muted).toBe(true);
    expect(t.hidden).toBe(true);
    expect(t.volume).toBe(1.5);
    expect(summary.updatedTrackIds).toEqual(["v1"]);
  });

  it("锁定轨只允许改 locked 本身", () => {
    mustFail(baseDoc(), "LOCKED_TRACK", {
      op: "updateTrack",
      trackId: "vlocked",
      trackPatch: { muted: true },
    });
    const { doc } = applyOps(baseDoc(), [
      { op: "updateTrack", trackId: "vlocked", trackPatch: { locked: false } },
    ]);
    expect(doc.tracks.find((t) => t.trackId === "vlocked")!.locked).toBe(false);
  });

  it("volume 超界报 BAD_REQUEST", () => {
    mustFail(baseDoc(), "BAD_REQUEST", {
      op: "updateTrack",
      trackId: "v1",
      trackPatch: { volume: 2.5 },
    });
  });

  it("keeps visual hidden separate from mute and rejects hidden on audio tracks", () => {
    const { doc } = applyOps(baseDoc(), [
      {
        op: "updateTrack",
        trackId: "v1",
        trackPatch: { hidden: true },
      },
    ]);
    const video = doc.tracks.find((track) => track.trackId === "v1")!;
    expect(video.hidden).toBe(true);
    expect(video.muted).not.toBe(true);
    mustFail(baseDoc(), "BAD_REQUEST", {
      op: "updateTrack",
      trackId: "a1",
      trackPatch: { hidden: true },
    });
  });
});

describe("removeMedia", () => {
  it("未被引用的素材可删", () => {
    const { doc, summary } = applyOps(baseDoc(), [
      { op: "removeMedia", mediaId: "m_image" },
    ]);
    expect(doc.media.map((m) => m.mediaId)).toEqual(["m_video", "m_audio"]);
    expect(summary.removedMediaIds).toEqual(["m_image"]);
  });

  it("仍被 item 引用时报 STILL_REFERENCED", () => {
    mustFail(docWithClip(), "STILL_REFERENCED", { op: "removeMedia", mediaId: "m_video" });
  });

  it("同批先删 item 再删素材可以通过", () => {
    const { doc } = applyOps(docWithClip(), [
      { op: "removeItems", itemIds: ["it1"] },
      { op: "removeMedia", mediaId: "m_video" },
    ]);
    expect(doc.media.map((m) => m.mediaId)).toEqual(["m_audio", "m_image"]);
  });
});

describe("updateMedia", () => {
  it("重命名素材写入 displayName", () => {
    const { doc, summary } = applyOps(baseDoc(), [
      { op: "updateMedia", mediaId: "m_video", mediaPatch: { displayName: "开场镜头" } },
    ]);
    expect(doc.media[0].displayName).toBe("开场镜头");
    expect(summary.updatedMediaIds).toEqual(["m_video"]);
  });

  it("素材不存在报 NOT_FOUND", () => {
    mustFail(baseDoc(), "NOT_FOUND", {
      op: "updateMedia",
      mediaId: "nope",
      mediaPatch: { displayName: "x" },
    });
  });

  it("空白 displayName 清空覆盖名（撤销首次重命名要能精确恢复）", () => {
    const { doc } = applyOps(baseDoc(), [
      { op: "updateMedia", mediaId: "m_video", mediaPatch: { displayName: "   " } },
    ]);
    expect(doc.media[0].displayName).toBe("");
  });

  it("超长 displayName 报 BAD_REQUEST", () => {
    mustFail(baseDoc(), "BAD_REQUEST", {
      op: "updateMedia",
      mediaId: "m_video",
      mediaPatch: { displayName: "a".repeat(257) },
    });
  });
});

describe("addTrack", () => {
  it("inserts at index and validates", () => {
    const d = baseDoc();
    const { doc, summary } = applyOps(d, [
      { op: "addTrack", track: { trackId: "v2", kind: "video" }, index: 1 },
    ]);
    expect(doc.tracks[1].trackId).toBe("v2");
    expect(summary.addedTrackIds).toEqual(["v2"]);

    mustFail(d, "CONFLICTING_ID", { op: "addTrack", track: { trackId: "v1", kind: "video" } });
    mustFail(d, "BAD_REQUEST", {
      op: "addTrack",
      track: { trackId: "x", kind: "weird" as never },
    });
    mustFail(d, "OUT_OF_RANGE", {
      op: "addTrack",
      track: { trackId: "x", kind: "video" },
      index: 99,
    });
  });
});

describe("setCaptions", () => {
  it("replaces the whole caption track", () => {
    const d = baseDoc();
    d.items = [
      { itemId: "old_cap", trackId: "c1", kind: "text", from: 0, durationInFrames: 30, text: "旧字幕" },
    ];
    const { doc, summary } = applyOps(d, [
      {
        op: "setCaptions",
        trackId: "c1",
        captions: [
          { from: 0, durationInFrames: 60, text: "第一句" },
          { from: 60, durationInFrames: 45, text: "第二句", stylePresetId: "style-1" },
        ],
      },
    ]);
    expect(doc.items).toHaveLength(2);
    expect(doc.items[0].itemId).toBe("c1_cap0");
    expect(doc.items[0].text).toBe("第一句");
    expect(doc.items[1].stylePresetId).toBe("style-1");
    expect(summary.removedItemIds).toContain("old_cap");

    // 清空整轨
    const { doc: cleared } = applyOps(d, [{ op: "setCaptions", trackId: "c1", captions: [] }]);
    expect(cleared.items).toHaveLength(0);

    mustFail(d, "KIND_MISMATCH", { op: "setCaptions", trackId: "v1", captions: [] });
    mustFail(d, "BAD_REQUEST", {
      op: "setCaptions",
      trackId: "c1",
      captions: [{ from: 0, durationInFrames: 10, text: " " }],
    });
    mustFail(d, "INVALID_CAPTION_STYLE", {
      op: "setCaptions",
      trackId: "c1",
      captions: [
        {
          from: 0,
          durationInFrames: 10,
          text: "bad style",
          captionStyle: { fontSize: 7 },
        },
      ],
    });
    mustFail(d, "OVERLAP", {
      op: "setCaptions",
      trackId: "c1",
      captions: [
        { from: 0, durationInFrames: 60, text: "a" },
        { from: 30, durationInFrames: 60, text: "b" },
      ],
    });
  });
});

describe("setMetadata", () => {
  it("allows fps change only on empty docs", () => {
    const empty = newDoc("x", 1920, 1080, 30);
    const { doc, summary } = applyOps(empty, [
      { op: "setMetadata", metadata: { fps: 60, name: "新名字" } },
    ]);
    expect(doc.metadata.fps).toBe(60);
    expect(summary.metadataChanged).toBe(true);

    mustFail(empty, "BAD_REQUEST", { op: "setMetadata", metadata: { fps: 23 } });

    const d = docWithClip();
    mustFail(d, "FPS_LOCKED", { op: "setMetadata", metadata: { fps: 60 } });
    const { doc: doc2 } = applyOps(d, [
      { op: "setMetadata", metadata: { width: 1080, height: 1920 } },
    ]);
    expect(doc2.metadata.width).toBe(1080);
    mustFail(d, "BAD_REQUEST", { op: "setMetadata", metadata: { width: 4 } });
    // 传相同 fps 不算修改
    applyOps(d, [{ op: "setMetadata", metadata: { fps: 30, name: "ok" } }]);
  });
});

describe("updateItem", () => {
  it("updates audio volume and validates its range", () => {
    const d = newDoc("x", 1920, 1080, 30);
    d.tracks.push({ trackId: "a1", kind: "audio" });
    d.media.push({
      mediaId: "m_audio",
      uri: "audio.mp3",
      kind: "audio",
      durationInFrames: 300,
    });
    d.items.push({
      itemId: "audio1",
      trackId: "a1",
      kind: "audio",
      mediaId: "m_audio",
      from: 0,
      durationInFrames: 100,
    });
    const { doc, summary } = applyOps(d, [
      { op: "updateItem", itemId: "audio1", patch: { volume: 0.5, speed: 2 } },
    ]);
    expect(doc.items[0]).toMatchObject({ volume: 0.5, speed: 2 });
    expect(summary.updatedItemIds).toEqual(["audio1"]);
    mustFail(d, "BAD_REQUEST", {
      op: "updateItem",
      itemId: "audio1",
      patch: { volume: 3 },
    });
  });

  it("updates caption text/style and rejects empty text", () => {
    const d = newDoc("x", 1920, 1080, 30);
    d.tracks.push({ trackId: "c1", kind: "caption" });
    d.items.push({
      itemId: "cap1",
      trackId: "c1",
      kind: "text",
      from: 0,
      durationInFrames: 60,
      text: "旧字幕",
    });
    const { doc } = applyOps(d, [
      {
        op: "updateItem",
        itemId: "cap1",
        patch: { text: "新字幕", stylePresetId: "subtitle-bold" },
      },
    ]);
    expect(doc.items[0]).toMatchObject({
      text: "新字幕",
      stylePresetId: "subtitle-bold",
    });
    mustFail(d, "BAD_REQUEST", {
      op: "updateItem",
      itemId: "cap1",
      patch: { text: "  " },
    });
  });
});

describe("batch semantics and whole-doc validation", () => {
  it("allows transient overlap resolved within the batch", () => {
    const d = docWithClip(); // it1: [100, 220)
    applyOps(d, [
      {
        op: "addItem",
        item: { itemId: "it2", trackId: "v1", kind: "video", mediaId: "m_video", from: 150, durationInFrames: 50 },
      },
      { op: "moveItem", itemId: "it2", from: 300 },
    ]);
  });

  it("fails the whole batch on final overlap", () => {
    const d = docWithClip();
    const err = mustFail(d, "OVERLAP", {
      op: "addItem",
      item: { itemId: "it2", trackId: "v1", kind: "video", mediaId: "m_video", from: 150, durationInFrames: 50 },
    });
    expect(err.index).toBe(-1);
  });

  it("keeps atomicity when a later op fails", () => {
    const d = docWithClip();
    expect(() =>
      applyOps(d, [
        { op: "moveItem", itemId: "it1", from: 0 },
        { op: "removeItems", itemIds: ["nope"] },
      ]),
    ).toThrow(OpError);
    expect(d.items[0].from).toBe(100);
  });

  it("supports split-then-edit-right-half in one batch", () => {
    const d = docWithClip();
    const { doc } = applyOps(d, [
      { op: "split", itemId: "it1", frame: 150 },
      { op: "removeItems", itemIds: ["it1~150"] },
    ]);
    expect(doc.items).toHaveLength(1);
    expect(doc.items[0].durationInFrames).toBe(50);
  });

  it("validateDoc rejects transitions and computes duration", () => {
    const d = docWithClip();
    expect(docDurationInFrames(d)).toBe(220);
    expect(() => validateDoc(d)).not.toThrow();

    const bad = structuredClone(d);
    bad.transitions = [
      {
        transitionId: "t1",
        fromItemId: "",
        toItemId: "",
        kind: "crossfade",
        frames: 0,
      },
    ];
    expect(() => validateDoc(bad)).toThrow(OpError);
  });
});

function transitionDoc(): TimelineDoc {
  const d = baseDoc();
  d.items = [
    {
      itemId: "out",
      trackId: "v1",
      kind: "video",
      mediaId: "m_video",
      from: 0,
      durationInFrames: 100,
    },
    {
      itemId: "in",
      trackId: "v1",
      kind: "video",
      mediaId: "m_video",
      from: 100,
      durationInFrames: 60,
      trimStart: 100,
    },
  ];
  return d;
}

describe("M3B transitions", () => {
  const addTransition: Operation = {
    op: "addTransition",
    transition: {
      transitionId: "tr",
      fromItemId: "out",
      toItemId: "in",
      kind: "crossfade",
      frames: 12,
    },
  };

  it("supports CRUD, summaries, locking, and boundary validation", () => {
    const added = applyOps(transitionDoc(), [addTransition]);
    expect(added.summary.addedTransitionIds).toEqual(["tr"]);
    const updated = applyOps(added.doc, [
      {
        op: "updateTransition",
        transitionId: "tr",
        transitionPatch: { kind: "dipToBlack", frames: 20 },
      },
    ]);
    expect(updated.doc.transitions?.[0]).toMatchObject({
      kind: "dipToBlack",
      frames: 20,
    });
    expect(updated.summary.updatedTransitionIds).toEqual(["tr"]);
    const removed = applyOps(updated.doc, [
      { op: "removeTransition", transitionId: "tr" },
    ]);
    expect(removed.doc.transitions).toHaveLength(0);
    expect(removed.summary.removedTransitionIds).toEqual(["tr"]);

    const nonAdjacent = transitionDoc();
    nonAdjacent.items[1].from = 101;
    mustFail(nonAdjacent, "INVALID_TRANSITION", addTransition);
    const noHandle = transitionDoc();
    noHandle.items[0].durationInFrames = 298;
    noHandle.items[1].from = 298;
    mustFail(noHandle, "INSUFFICIENT_HANDLE", {
      ...addTransition,
      transition: { ...addTransition.transition, frames: 3 },
    });
    const locked = transitionDoc();
    locked.tracks[0].locked = true;
    mustFail(locked, "LOCKED_TRACK", addTransition);
  });

  it("cascades invalid transitions only after final batch geometry", () => {
    const withTransition = applyOps(transitionDoc(), [addTransition]).doc;
    const preserved = applyOps(withTransition, [
      { op: "moveItem", itemId: "out", from: 10 },
      { op: "moveItem", itemId: "in", from: 110 },
    ]);
    expect(preserved.doc.transitions).toHaveLength(1);
    expect(preserved.summary.removedTransitionIds).toBeUndefined();

    const cascaded = applyOps(withTransition, [
      { op: "trimEnd", itemId: "out", frame: 90 },
    ]);
    expect(cascaded.doc.transitions).toHaveLength(0);
    expect(cascaded.summary.removedTransitionIds).toEqual(["tr"]);

    const directlyRemoved = applyOps(withTransition, [
      { op: "removeItems", itemIds: ["in"] },
    ]);
    expect(directlyRemoved.summary.removedTransitionIds).toEqual(["tr"]);
  });
});

describe("M3B ducking and styles", () => {
  it("validates, clones, clears, and cleans up ducking sidechains", () => {
    const d = baseDoc();
    d.tracks.push({ trackId: "a2", kind: "audio" });
    const config = {
      sidechainTrackIds: ["v1", "a2"],
      gain: 0.25,
      attackFrames: 30,
      releaseFrames: 45,
    };
    const configured = applyOps(d, [
      { op: "updateTrack", trackId: "a1", trackPatch: { ducking: config } },
    ]).doc;
    config.sidechainTrackIds[0] = "mutated";
    expect(configured.tracks.find((track) => track.trackId === "a1")?.ducking
      ?.sidechainTrackIds).toEqual(["v1", "a2"]);

    const cleaned = applyOps(configured, [
      { op: "removeTrack", trackId: "v1" },
    ]);
    expect(cleaned.doc.tracks.find((track) => track.trackId === "a1")?.ducking
      ?.sidechainTrackIds).toEqual(["a2"]);
    expect(cleaned.summary.updatedTrackIds).toContain("a1");

    const cleared = applyOps(configured, [
      {
        op: "updateTrack",
        trackId: "a1",
        trackPatch: {
          ducking: {
            sidechainTrackIds: [],
            gain: 0,
            attackFrames: 0,
            releaseFrames: 0,
          },
        },
      },
    ]).doc;
    expect(cleared.tracks.find((track) => track.trackId === "a1")?.ducking)
      .toBeUndefined();
    mustFail(d, "INVALID_DUCKING", {
      op: "updateTrack",
      trackId: "v1",
      trackPatch: { ducking: { ...config, sidechainTrackIds: ["a2"] } },
    });
  });

  it("validates crop/rotation and merges or clears caption styles", () => {
    const transformed = applyOps(docWithClip(), [
      {
        op: "updateItem",
        itemId: "it1",
        patch: {
          transform: {
            x: 0,
            y: 0,
            width: 1,
            height: 1,
            fit: "contain",
            rotationDegrees: 45,
            crop: { x: 0.1, y: 0.2, width: 0.6, height: 0.7 },
          },
        },
      },
    ]).doc;
    expect(transformed.items[0].transform?.crop?.x).toBe(0.1);
    mustFail(docWithClip(), "OUT_OF_RANGE", {
      op: "updateItem",
      itemId: "it1",
      patch: {
        transform: {
          x: 0,
          y: 0,
          width: 1,
          height: 1,
          fit: "contain",
          rotationDegrees: 181,
        },
      },
    });

    const captions = applyOps(baseDoc(), [
      {
        op: "setCaptions",
        trackId: "c1",
        captions: [
          {
            from: 0,
            durationInFrames: 20,
            text: "styled",
            captionStyle: { fontFamily: "mono", bold: false, x: 0.25 },
          },
        ],
      },
    ]).doc;
    const merged = applyOps(captions, [
      {
        op: "updateItem",
        itemId: "c1_cap0",
        patch: { captionStyle: { bold: true, textColor: "#FF0000" } },
      },
    ]).doc;
    expect(merged.items[0].captionStyle).toMatchObject({
      fontFamily: "mono",
      bold: true,
      textColor: "#FF0000",
    });
    const cleared = applyOps(merged, [
      { op: "updateItem", itemId: "c1_cap0", patch: { captionStyle: {} } },
    ]).doc;
    expect(cleared.items[0].captionStyle).toBeUndefined();
    mustFail(captions, "INVALID_CAPTION_STYLE", {
      op: "updateItem",
      itemId: "c1_cap0",
      patch: { captionStyle: { fontSize: 7 } },
    });
  });
});
