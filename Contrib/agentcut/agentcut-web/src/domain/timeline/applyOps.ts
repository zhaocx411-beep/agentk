// ApplyOps 纯函数的 TypeScript 移植，逻辑逐行对齐后端 timeline/apply.go +
// timeline/validate.go。任何行为差异都算 bug（前后端必须对同一批 ops 算出
// 相同的文档，否则乐观更新会漂移）。
//
// 覆盖所有前端手动编辑会产生的 op；addMedia/setTranscript 仍只由上传链路
// 与 Agent 触发，Agent 改动走全量拉取而不是本地重放。

import type {
  CaptionStyle,
  ChangeSummary,
  ErrorCode,
  Item,
  Operation,
  TimelineDoc,
  Track,
  Transition,
} from "./types";
import {
  ALLOWED_FPS,
  MAX_DOC_BYTES,
  MAX_ITEMS,
  MAX_MEDIA,
  MAX_OPS_PER_BATCH,
  MAX_TRACKS,
  MAX_TRANSITIONS,
  OpError,
  SCHEMA_VERSION,
  cloneDoc,
  effectiveSpeed,
  itemEnd,
  itemKindAllowedOn,
  mediaKindForItem,
  roundFrames,
  sourceFramesConsumed,
} from "./types";

export interface ApplyResult {
  doc: TimelineDoc;
  summary: ChangeSummary;
}

function opErr(
  index: number,
  op: string,
  code: ErrorCode,
  message: string,
): OpError {
  return new OpError(index, op, code, message);
}

function batchErr(code: ErrorCode, message: string): OpError {
  return new OpError(-1, "", code, message);
}

// ---- recorder（apply.go 的 recorder） ----

class Recorder {
  addedItems: string[] = [];
  removedItems: string[] = [];
  updatedItems: string[] = [];
  addedTracks: string[] = [];
  removedTracks: string[] = [];
  updatedTracks: string[] = [];
  addedMedia: string[] = [];
  removedMedia: string[] = [];
  updatedMedia: string[] = [];
  addedTransitions: string[] = [];
  removedTransitions: string[] = [];
  updatedTransitions: string[] = [];
  metadataChanged = false;

  summary(): ChangeSummary {
    // 同批内先加后删的 ID 两边都会出现，保持真实轨迹，不做互相抵消。
    const out: ChangeSummary = {};
    const put = (key: keyof ChangeSummary, ids: string[]) => {
      const d = dedupe(ids);
      if (d) (out as Record<string, unknown>)[key] = d;
    };
    put("addedItemIds", this.addedItems);
    put("removedItemIds", this.removedItems);
    put("updatedItemIds", this.updatedItems);
    put("addedTrackIds", this.addedTracks);
    put("removedTrackIds", this.removedTracks);
    put("updatedTrackIds", this.updatedTracks);
    put("addedMediaIds", this.addedMedia);
    put("removedMediaIds", this.removedMedia);
    put("updatedMediaIds", this.updatedMedia);
    put("addedTransitionIds", this.addedTransitions);
    put("removedTransitionIds", this.removedTransitions);
    put("updatedTransitionIds", this.updatedTransitions);
    if (this.metadataChanged) out.metadataChanged = true;
    return out;
  }
}

function dedupe(input: string[]): string[] | undefined {
  if (input.length === 0) return undefined;
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of input) {
    if (!seen.has(s)) {
      seen.add(s);
      out.push(s);
    }
  }
  return out;
}

// ---- doc.go 的索引查找 ----

function mediaIndexByID(d: TimelineDoc, id: string): number {
  return d.media.findIndex((m) => m.mediaId === id);
}

function trackIndexByID(d: TimelineDoc, id: string): number {
  return d.tracks.findIndex((t) => t.trackId === id);
}

function itemIndexByID(d: TimelineDoc, id: string): number {
  return d.items.findIndex((it) => it.itemId === id);
}

function transitionIndexByID(d: TimelineDoc, id: string): number {
  return (d.transitions ?? []).findIndex((transition) => transition.transitionId === id);
}

// ---- apply.go 的公共校验 ----

/** 校验轨道存在且未锁定，返回轨道下标（requireUnlockedTrack）。 */
function requireUnlockedTrack(
  d: TimelineDoc,
  i: number,
  opName: string,
  trackId: string,
): number {
  const ti = trackIndexByID(d, trackId);
  if (ti < 0) {
    throw opErr(i, opName, "NOT_FOUND", `track "${trackId}" not found`);
  }
  if (d.tracks[ti].locked) {
    throw opErr(i, opName, "LOCKED_TRACK", `track "${trackId}" is locked`);
  }
  return ti;
}

/** 校验 item 存在且其所在轨道未锁定，返回 item 下标（requireUnlockedItem）。 */
function requireUnlockedItem(
  d: TimelineDoc,
  i: number,
  opName: string,
  itemId: string,
): number {
  const ii = itemIndexByID(d, itemId);
  if (ii < 0) {
    throw opErr(i, opName, "NOT_FOUND", `item "${itemId}" not found`);
  }
  const ti = trackIndexByID(d, d.items[ii].trackId);
  if (ti >= 0 && d.tracks[ti].locked) {
    throw opErr(
      i,
      opName,
      "LOCKED_TRACK",
      `track "${d.items[ii].trackId}" is locked`,
    );
  }
  return ii;
}

/** 校验 item 自身字段（validateItemShape：引用与几何，重叠在批后整体校验）。 */
function validateItemShape(
  d: TimelineDoc,
  i: number,
  opName: string,
  it: Item,
): void {
  if (!it.itemId) {
    throw opErr(i, opName, "BAD_REQUEST", "itemId is required");
  }
  const ti = trackIndexByID(d, it.trackId);
  if (ti < 0) {
    throw opErr(i, opName, "NOT_FOUND", `track "${it.trackId}" not found`);
  }
  const track = d.tracks[ti];
  if (!itemKindAllowedOn(it.kind, track.kind)) {
    throw opErr(
      i,
      opName,
      "KIND_MISMATCH",
      `${it.kind} item not allowed on ${track.kind} track`,
    );
  }
  const trimStart = it.trimStart ?? 0;
  if (it.from < 0 || it.durationInFrames <= 0 || trimStart < 0) {
    throw opErr(
      i,
      opName,
      "OUT_OF_RANGE",
      `item "${it.itemId}" has invalid geometry (from=${it.from} dur=${it.durationInFrames} trimStart=${trimStart})`,
    );
  }
  const speed = it.speed ?? 0;
  if (!Number.isFinite(speed) || speed < 0 || (speed !== 0 && (speed < 0.1 || speed > 10))) {
    throw opErr(i, opName, "BAD_REQUEST", "speed must be within [0.1, 10]");
  }
  const volume = it.volume ?? 0;
  if (!Number.isFinite(volume) || volume < 0 || volume > 2) {
    throw opErr(i, opName, "BAD_REQUEST", "volume must be within [0, 2]");
  }
  const fadeIn = it.fadeInFrames ?? 0;
  const fadeOut = it.fadeOutFrames ?? 0;
  if (fadeIn < 0 || fadeOut < 0 || !Number.isInteger(fadeIn) || !Number.isInteger(fadeOut)) {
    throw opErr(i, opName, "BAD_REQUEST", "fadeInFrames and fadeOutFrames must be non-negative integers");
  }
  if (fadeIn + fadeOut > it.durationInFrames) {
    throw opErr(i, opName, "OUT_OF_RANGE", "fadeInFrames + fadeOutFrames must not exceed durationInFrames");
  }
  if ((it.muted || fadeIn || fadeOut) && it.kind !== "video" && it.kind !== "audio") {
    throw opErr(i, opName, "BAD_REQUEST", "muted and fades only apply to video/audio items");
  }
  if (it.transform) {
    if (it.kind !== "video" && it.kind !== "image") {
      throw opErr(i, opName, "BAD_REQUEST", "transform only applies to video/image items");
    }
    const t = it.transform;
    const values = [t.x, t.y, t.width, t.height, t.opacity ?? 0, t.rotationDegrees ?? 0];
    if (values.some((value) => !Number.isFinite(value))) {
      throw opErr(i, opName, "BAD_REQUEST", "transform values must be finite");
    }
    if (
      t.x < 0 ||
      t.y < 0 ||
      t.width <= 0 ||
      t.height <= 0 ||
      t.x > 1 ||
      t.y > 1 ||
      t.width > 1 ||
      t.height > 1 ||
      t.x + t.width > 1 ||
      t.y + t.height > 1
    ) {
      throw opErr(i, opName, "OUT_OF_RANGE", "transform rectangle must fit within normalized canvas bounds");
    }
    if ((t.opacity ?? 0) < 0 || (t.opacity ?? 0) > 1) {
      throw opErr(i, opName, "OUT_OF_RANGE", "transform opacity must be within [0, 1]");
    }
    if (t.fit && t.fit !== "contain" && t.fit !== "cover") {
      throw opErr(i, opName, "BAD_REQUEST", "transform fit must be contain or cover");
    }
    if ((t.rotationDegrees ?? 0) < -180 || (t.rotationDegrees ?? 0) > 180) {
      throw opErr(i, opName, "OUT_OF_RANGE", "rotationDegrees must be within [-180, 180]");
    }
    if (t.crop) {
      const crop = t.crop;
      const cropValues = [crop.x, crop.y, crop.width, crop.height];
      if (cropValues.some((value) => !Number.isFinite(value))) {
        throw opErr(i, opName, "BAD_REQUEST", "crop values must be finite");
      }
      if (
        crop.x < 0 ||
        crop.y < 0 ||
        crop.width <= 0 ||
        crop.height <= 0 ||
        crop.x > 1 ||
        crop.y > 1 ||
        crop.width > 1 ||
        crop.height > 1 ||
        crop.x + crop.width > 1 + 1e-12 ||
        crop.y + crop.height > 1 + 1e-12
      ) {
        throw opErr(i, opName, "OUT_OF_RANGE", "crop rectangle must fit within normalized source bounds");
      }
    }
  }
  if (it.kind === "text") {
    if (it.mediaId) {
      throw opErr(i, opName, "BAD_REQUEST", "text item must not reference media");
    }
    if (!(it.text ?? "").trim()) {
      throw opErr(i, opName, "BAD_REQUEST", "text item requires non-empty text");
    }
    validateCaptionStyle(i, opName, it.captionStyle);
    return;
  }
  if (it.captionStyle !== undefined) {
    throw opErr(i, opName, "INVALID_CAPTION_STYLE", "captionStyle only applies to text items");
  }
  // media item
  const wantKind = mediaKindForItem(it.kind);
  const mi = mediaIndexByID(d, it.mediaId ?? "");
  if (mi < 0) {
    throw opErr(i, opName, "NOT_FOUND", `media "${it.mediaId ?? ""}" not found`);
  }
  const m = d.media[mi];
  if (m.kind !== wantKind) {
    throw opErr(
      i,
      opName,
      "KIND_MISMATCH",
      `item kind ${it.kind} requires ${wantKind} media, got ${m.kind}`,
    );
  }
  // 图片无源长限制；视频/音频要求消耗的源区间不越界。
  if (m.kind !== "image") {
    const consumed = sourceFramesConsumed(it);
    if (trimStart + consumed > m.durationInFrames) {
      throw opErr(
        i,
        opName,
        "OUT_OF_RANGE",
        `item "${it.itemId}" consumes source frames [${trimStart}, ${trimStart + consumed}) beyond media duration ${m.durationInFrames}`,
      );
    }
  }
}

function validHexColor(value: string, bytes: number): boolean {
  return value.length === 1 + bytes * 2 && value[0] === "#" &&
    /^[0-9a-f]+$/i.test(value.slice(1));
}

function validateCaptionStyle(
  i: number,
  opName: string,
  style: CaptionStyle | undefined,
): void {
  if (!style) return;
  if (
    style.fontFamily !== undefined &&
    !["default", "sans", "serif", "mono"].includes(style.fontFamily)
  ) {
    throw opErr(
      i,
      opName,
      "INVALID_CAPTION_STYLE",
      "fontFamily must be default, sans, serif, or mono",
    );
  }
  const checkRange = (name: string, value: number | undefined, min: number, max: number) => {
    if (value !== undefined && (!Number.isFinite(value) || value < min || value > max)) {
      throw opErr(
        i,
        opName,
        "INVALID_CAPTION_STYLE",
        `${name} must be finite and within [${min}, ${max}]`,
      );
    }
  };
  checkRange("fontSize", style.fontSize, 8, 200);
  if (style.textColor !== undefined && !validHexColor(style.textColor, 3)) {
    throw opErr(i, opName, "INVALID_CAPTION_STYLE", "textColor must be #RRGGBB");
  }
  if (
    style.backgroundColor !== undefined &&
    style.backgroundColor !== "" &&
    !validHexColor(style.backgroundColor, 4)
  ) {
    throw opErr(
      i,
      opName,
      "INVALID_CAPTION_STYLE",
      "backgroundColor must be empty or #RRGGBBAA",
    );
  }
  if (style.outlineColor !== undefined && !validHexColor(style.outlineColor, 3)) {
    throw opErr(i, opName, "INVALID_CAPTION_STYLE", "outlineColor must be #RRGGBB");
  }
  checkRange("outlineWidth", style.outlineWidth, 0, 8);
  if (
    style.align !== undefined &&
    !["left", "center", "right"].includes(style.align)
  ) {
    throw opErr(
      i,
      opName,
      "INVALID_CAPTION_STYLE",
      "align must be left, center, or right",
    );
  }
  checkRange("x", style.x, 0, 1);
  checkRange("y", style.y, 0, 1);
  checkRange("maxWidth", style.maxWidth, 0.1, 1);
  checkRange("lineHeight", style.lineHeight, 0.8, 3);
}

function mergeCaptionStyle(
  current: CaptionStyle | undefined,
  patch: CaptionStyle,
): CaptionStyle | undefined {
  const fields: (keyof CaptionStyle)[] = [
    "fontFamily",
    "fontSize",
    "textColor",
    "backgroundColor",
    "outlineColor",
    "outlineWidth",
    "bold",
    "italic",
    "align",
    "x",
    "y",
    "maxWidth",
    "lineHeight",
  ];
  if (fields.every((field) => patch[field] === undefined)) return undefined;
  const definedPatch = Object.fromEntries(
    fields
      .filter((field) => patch[field] !== undefined)
      .map((field) => [field, structuredClone(patch[field])]),
  ) as CaptionStyle;
  return { ...(current ? structuredClone(current) : {}), ...definedPatch };
}

function clampFades(item: Item): void {
  item.fadeInFrames = Math.min(item.fadeInFrames ?? 0, item.durationInFrames);
  item.fadeOutFrames = Math.min(
    item.fadeOutFrames ?? 0,
    item.durationInFrames - item.fadeInFrames,
  );
}

// ---- ApplyOps 主入口 ----

/**
 * 把一批操作应用到文档上，返回新文档与改动摘要（apply.go 的 ApplyOps）。
 *
 * 约束（与后端完全一致）：
 * - 纯函数：无 IO / 时间 / 随机数；
 * - 输入 doc 不被修改；
 * - 任一 op 失败则整批不生效，抛出 OpError；
 * - 批内允许临时不一致（如先重叠后移开），批后整体校验必须通过。
 */
export function applyOps(doc: TimelineDoc, ops: Operation[]): ApplyResult {
  return applyOpsInternal(doc, ops, true);
}

/**
 * History inversion needs the same per-op mutation and immediate validation as
 * the backend loop, while preserving temporary overlap/transition invalidity
 * until the complete user batch has established its final geometry.
 */
export function applyOperationWithoutFinalValidation(
  doc: TimelineDoc,
  op: Operation,
): TimelineDoc {
  return applyOpsInternal(doc, [op], false).doc;
}

function applyOpsInternal(
  doc: TimelineDoc,
  ops: Operation[],
  finalize: boolean,
): ApplyResult {
  if (ops.length === 0) {
    throw batchErr("BAD_REQUEST", "empty operations");
  }
  if (ops.length > MAX_OPS_PER_BATCH) {
    throw batchErr(
      "LIMIT_EXCEEDED",
      `operations ${ops.length} exceeds max ${MAX_OPS_PER_BATCH}`,
    );
  }
  if (doc.schemaVersion !== SCHEMA_VERSION) {
    throw batchErr("UNSUPPORTED", `schemaVersion ${doc.schemaVersion} unsupported`);
  }

  const next = cloneDoc(doc);
  const rec = new Recorder();
  for (let i = 0; i < ops.length; i++) {
    const op = ops[i];
    switch (op.op) {
      case "addItem":
        applyAddItem(next, i, op, rec);
        break;
      case "removeItems":
        applyRemoveItems(next, i, op, rec);
        break;
      case "split":
        applySplit(next, i, op, rec);
        break;
      case "trimStart":
        applyTrim(next, i, op, rec, true);
        break;
      case "trimEnd":
        applyTrim(next, i, op, rec, false);
        break;
      case "moveItem":
        applyMoveItem(next, i, op, rec);
        break;
      case "updateItem":
        applyUpdateItem(next, i, op, rec);
        break;
      case "rippleDelete":
        applyRippleDelete(next, i, op, rec);
        break;
      case "addTrack":
        applyAddTrack(next, i, op, rec);
        break;
      case "removeTrack":
        applyRemoveTrack(next, i, op, rec);
        break;
      case "updateTrack":
        applyUpdateTrack(next, i, op, rec);
        break;
      case "removeMedia":
        applyRemoveMedia(next, i, op, rec);
        break;
      case "updateMedia":
        applyUpdateMedia(next, i, op, rec);
        break;
      case "setCaptions":
        applySetCaptions(next, i, op, rec);
        break;
      case "setMetadata":
        applySetMetadata(next, i, op, rec);
        break;
      case "addTransition":
        applyAddTransition(next, i, op, rec);
        break;
      case "updateTransition":
        applyUpdateTransition(next, i, op, rec);
        break;
      case "removeTransition":
        applyRemoveTransition(next, i, op, rec);
        break;
      default:
        // 协议内但前端未移植的 op（addMedia 等）走后端执行；未知 op 同样拒绝。
        throw opErr(
          i,
          (op as Operation).op,
          "UNSUPPORTED",
          `op "${(op as Operation).op}" is not implemented in the frontend ApplyOps`,
        );
    }
  }

  if (finalize) {
    removeInvalidTransitions(next, rec);
    validateDoc(next);
  }
  return { doc: next, summary: rec.summary() };
}

// ---- items ----

function applyAddItem(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "addItem" }>,
  rec: Recorder,
): void {
  if (!op.item) {
    throw opErr(i, op.op, "BAD_REQUEST", "item is required");
  }
  const it: Item = structuredClone(op.item);
  if (itemIndexByID(d, it.itemId) >= 0) {
    throw opErr(i, op.op, "CONFLICTING_ID", `item "${it.itemId}" already exists`);
  }
  requireUnlockedTrack(d, i, op.op, it.trackId);
  validateItemShape(d, i, op.op, it);
  if (d.items.length >= MAX_ITEMS) {
    throw opErr(i, op.op, "LIMIT_EXCEEDED", `item count would exceed ${MAX_ITEMS}`);
  }
  d.items.push(it);
  rec.addedItems.push(it.itemId);
}

function applyRemoveItems(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "removeItems" }>,
  rec: Recorder,
): void {
  if (!op.itemIds || op.itemIds.length === 0) {
    throw opErr(i, op.op, "BAD_REQUEST", "itemIds is required");
  }
  // 先全部校验再删除，保证单 op 原子性。
  for (const id of op.itemIds) {
    requireUnlockedItem(d, i, op.op, id);
  }
  const remove = new Set(op.itemIds);
  d.items = d.items.filter((it) => !remove.has(it.itemId));
  rec.removedItems.push(...op.itemIds);
  removeTransitionsForItems(d, rec, remove);
}

/** split 右半段的确定性派生 ID（deriveSplitID，Workflow 内禁止随机数）。 */
export function deriveSplitID(itemId: string, frame: number): string {
  return `${itemId}~${frame}`;
}

function applySplit(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "split" }>,
  rec: Recorder,
): void {
  if (op.frame === undefined || op.frame === null) {
    throw opErr(i, op.op, "BAD_REQUEST", "frame is required");
  }
  const ii = requireUnlockedItem(d, i, op.op, op.itemId);
  const it = d.items[ii];
  const frame = op.frame;
  if (frame <= it.from || frame >= itemEnd(it)) {
    throw opErr(
      i,
      op.op,
      "OUT_OF_RANGE",
      `split frame ${frame} must be strictly inside item [${it.from}, ${itemEnd(it)})`,
    );
  }
  const newId = op.newItemId || deriveSplitID(it.itemId, frame);
  if (itemIndexByID(d, newId) >= 0) {
    throw opErr(i, op.op, "CONFLICTING_ID", `item "${newId}" already exists`);
  }
  if (d.items.length >= MAX_ITEMS) {
    throw opErr(i, op.op, "LIMIT_EXCEEDED", `item count would exceed ${MAX_ITEMS}`);
  }

  const leftDur = frame - it.from;
  const right: Item = { ...it };
  right.itemId = newId;
  right.from = frame;
  right.durationInFrames = itemEnd(it) - frame;
  right.fadeInFrames = 0;
  // 右半段的源起点 = 原 trimStart + 左半段消耗的源帧数（源区间总量守恒）。
  right.trimStart = (it.trimStart ?? 0) + roundFrames(leftDur * effectiveSpeed(it));

  d.items[ii] = { ...it, durationInFrames: leftDur, fadeOutFrames: 0 };
  clampFades(d.items[ii]);
  clampFades(right);
  d.items.push(right);
  rec.updatedItems.push(it.itemId);
  rec.addedItems.push(newId);
}

/** applyTrim：trimStart（isStart=true，改左沿）与 trimEnd（改右沿，开区间）。
 *  frame 是时间线绝对帧，不是 delta。 */
function applyTrim(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "trimStart" | "trimEnd" }>,
  rec: Recorder,
  isStart: boolean,
): void {
  if (op.frame === undefined || op.frame === null) {
    throw opErr(i, op.op, "BAD_REQUEST", "frame is required");
  }
  const ii = requireUnlockedItem(d, i, op.op, op.itemId);
  const it = d.items[ii];
  const frame = op.frame;
  const oldFrom = it.from;
  const oldEnd = itemEnd(it);
  if (isStart) {
    if (frame < 0 || frame >= itemEnd(it)) {
      throw opErr(
        i,
        op.op,
        "OUT_OF_RANGE",
        `trimStart frame ${frame} must be within [0, ${itemEnd(it)})`,
      );
    }
    const delta = frame - it.from; // 正 = 收窄，负 = 向左扩展
    const newTrim = (it.trimStart ?? 0) + roundFrames(delta * effectiveSpeed(it));
    if (it.kind === "video" || it.kind === "audio") {
      if (newTrim < 0) {
        throw opErr(
          i,
          op.op,
          "OUT_OF_RANGE",
          `cannot extend item "${it.itemId}" before source start`,
        );
      }
      it.trimStart = newTrim;
    }
    it.durationInFrames = itemEnd(it) - frame;
    it.from = frame;
    if (frame > oldFrom) {
      it.fadeInFrames = Math.max(0, (it.fadeInFrames ?? 0) - (frame - oldFrom));
    }
    clampFades(it);
  } else {
    if (frame <= it.from) {
      throw opErr(
        i,
        op.op,
        "OUT_OF_RANGE",
        `trimEnd frame ${frame} must be > item start ${it.from}`,
      );
    }
    it.durationInFrames = frame - it.from;
    if (it.kind === "video" || it.kind === "audio") {
      const mi = mediaIndexByID(d, it.mediaId ?? "");
      if (
        mi >= 0 &&
        (it.trimStart ?? 0) + sourceFramesConsumed(it) > d.media[mi].durationInFrames
      ) {
        throw opErr(
          i,
          op.op,
          "OUT_OF_RANGE",
          `cannot extend item "${it.itemId}" beyond source end`,
        );
      }
    }
    if (frame < oldEnd) {
      it.fadeOutFrames = Math.max(0, (it.fadeOutFrames ?? 0) - (oldEnd - frame));
    }
    clampFades(it);
  }
  rec.updatedItems.push(it.itemId);
}

function applyMoveItem(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "moveItem" }>,
  rec: Recorder,
): void {
  if (op.from === undefined || op.from === null) {
    throw opErr(i, op.op, "BAD_REQUEST", "from is required");
  }
  const ii = requireUnlockedItem(d, i, op.op, op.itemId);
  const it = d.items[ii];
  if (op.from < 0) {
    throw opErr(i, op.op, "OUT_OF_RANGE", "from must be >= 0");
  }
  if (op.trackId && op.trackId !== it.trackId) {
    const ti = requireUnlockedTrack(d, i, op.op, op.trackId);
    if (!itemKindAllowedOn(it.kind, d.tracks[ti].kind)) {
      throw opErr(
        i,
        op.op,
        "KIND_MISMATCH",
        `${it.kind} item not allowed on ${d.tracks[ti].kind} track`,
      );
    }
    it.trackId = op.trackId;
  }
  it.from = op.from;
  rec.updatedItems.push(it.itemId);
}

function applyUpdateItem(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "updateItem" }>,
  rec: Recorder,
): void {
  if (!op.patch) {
    throw opErr(i, op.op, "BAD_REQUEST", "patch is required");
  }
  const ii = requireUnlockedItem(d, i, op.op, op.itemId);
  const it = d.items[ii];
  const p = op.patch;

  if (p.speed !== undefined) {
    if (p.speed < 0.1 || p.speed > 10) {
      throw opErr(i, op.op, "BAD_REQUEST", "speed must be within [0.1, 10]");
    }
    if (it.kind === "text" || it.kind === "image") {
      throw opErr(i, op.op, "BAD_REQUEST", "speed only applies to video/audio items");
    }
    it.speed = p.speed;
    const media = d.media.find((m) => m.mediaId === it.mediaId);
    if (media && (it.trimStart ?? 0) + sourceFramesConsumed(it) > media.durationInFrames) {
      throw opErr(
        i,
        op.op,
        "OUT_OF_RANGE",
        "new speed consumes source beyond media duration; trim the item first",
      );
    }
  }
  if (p.volume !== undefined) {
    if (p.volume < 0 || p.volume > 2) {
      throw opErr(i, op.op, "BAD_REQUEST", "volume must be within [0, 2]");
    }
    if (it.kind === "text" || it.kind === "image") {
      throw opErr(i, op.op, "BAD_REQUEST", "volume only applies to video/audio items");
    }
    it.volume = p.volume;
  }
  if (p.muted !== undefined) {
    if (it.kind !== "video" && it.kind !== "audio") {
      throw opErr(i, op.op, "BAD_REQUEST", "muted only applies to video/audio items");
    }
    it.muted = p.muted;
  }
  if (p.fadeInFrames !== undefined) it.fadeInFrames = p.fadeInFrames;
  if (p.fadeOutFrames !== undefined) it.fadeOutFrames = p.fadeOutFrames;
  if (p.transform !== undefined) it.transform = structuredClone(p.transform);
  if (p.text !== undefined) {
    if (it.kind !== "text") {
      throw opErr(i, op.op, "BAD_REQUEST", "text only applies to text items");
    }
    if (!p.text.trim()) {
      throw opErr(i, op.op, "BAD_REQUEST", "text must be non-empty");
    }
    it.text = p.text;
  }
  if (p.stylePresetId !== undefined) it.stylePresetId = p.stylePresetId;
  if (p.captionStyle !== undefined) {
    if (it.kind !== "text") {
      throw opErr(
        i,
        op.op,
        "INVALID_CAPTION_STYLE",
        "captionStyle only applies to text items",
      );
    }
    it.captionStyle = mergeCaptionStyle(it.captionStyle, p.captionStyle);
  }
  validateItemShape(d, i, op.op, it);
  rec.updatedItems.push(it.itemId);
}

function applyRippleDelete(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "rippleDelete" }>,
  rec: Recorder,
): void {
  if (
    op.fromFrame === undefined ||
    op.fromFrame === null ||
    op.toFrame === undefined ||
    op.toFrame === null
  ) {
    throw opErr(i, op.op, "BAD_REQUEST", "fromFrame and toFrame are required");
  }
  const from = op.fromFrame;
  const to = op.toFrame;
  if (from < 0 || to <= from) {
    throw opErr(i, op.op, "OUT_OF_RANGE", `invalid range [${from}, ${to})`);
  }
  const delta = to - from;

  // 确定作用轨道：显式指定必须全部存在且未锁定；缺省 = 所有未锁定轨。
  const affected = new Set<string>();
  if (op.trackIds && op.trackIds.length > 0) {
    for (const tid of op.trackIds) {
      requireUnlockedTrack(d, i, op.op, tid);
      affected.add(tid);
    }
  } else {
    for (const t of d.tracks) {
      if (!t.locked) affected.add(t.trackId);
    }
  }

  const out: Item[] = [];
  for (const orig of d.items) {
    if (!affected.has(orig.trackId)) {
      out.push(orig);
      continue;
    }
    const it = { ...orig };
    const start = it.from;
    const end = itemEnd(it);
    if (end <= from) {
      // 完全在区间前，不动
      out.push(it);
    } else if (start >= to) {
      // 完全在区间后，整体左移
      it.from -= delta;
      out.push(it);
      rec.updatedItems.push(it.itemId);
    } else if (start >= from && end <= to) {
      // 完全被删
      rec.removedItems.push(it.itemId);
    } else if (start < from && end > to) {
      // 跨越整个区间：裂成左右两段
      const left = { ...it };
      left.durationInFrames = from - start;
      left.fadeOutFrames = 0;
      clampFades(left);
      const right = { ...it };
      right.itemId = deriveSplitID(it.itemId, to);
      if (itemIndexByID(d, right.itemId) >= 0) {
        throw opErr(
          i,
          op.op,
          "CONFLICTING_ID",
          `derived item id "${right.itemId}" already exists`,
        );
      }
      right.from = from; // 右段起点左移 delta（to - delta = from）
      right.durationInFrames = end - to;
      right.fadeInFrames = 0;
      right.trimStart =
        (it.trimStart ?? 0) + roundFrames((to - start) * effectiveSpeed(it));
      clampFades(right);
      out.push(left, right);
      rec.updatedItems.push(left.itemId);
      rec.addedItems.push(right.itemId);
    } else if (start < from) {
      // 尾部落入区间：截断到 from
      it.durationInFrames = from - start;
      it.fadeOutFrames = Math.max(0, (it.fadeOutFrames ?? 0) - (end - from));
      clampFades(it);
      out.push(it);
      rec.updatedItems.push(it.itemId);
    } else {
      // 头部落入区间：裁掉 [start, to)，剩余部分左移到 from
      const cut = to - start;
      it.trimStart = (it.trimStart ?? 0) + roundFrames(cut * effectiveSpeed(it));
      it.durationInFrames = end - to;
      it.from = from;
      it.fadeInFrames = Math.max(0, (it.fadeInFrames ?? 0) - cut);
      clampFades(it);
      out.push(it);
      rec.updatedItems.push(it.itemId);
    }
  }
  d.items = out;
}

// ---- tracks ----

function validateDuckingConfig(
  d: TimelineDoc,
  i: number,
  opName: string,
  track: Track,
): void {
  const config = track.ducking;
  if (!config) return;
  if (track.kind !== "audio") {
    throw opErr(i, opName, "INVALID_DUCKING", "ducking only applies to audio tracks");
  }
  if (config.sidechainTrackIds.length === 0) {
    throw opErr(i, opName, "INVALID_DUCKING", "sidechainTrackIds must not be empty");
  }
  if (!Number.isFinite(config.gain) || config.gain <= 0 || config.gain > 1) {
    throw opErr(
      i,
      opName,
      "INVALID_DUCKING",
      "ducking gain must be finite and within (0, 1]",
    );
  }
  const maxEnvelopeFrames = d.metadata.fps * 5;
  if (
    !Number.isInteger(config.attackFrames) ||
    !Number.isInteger(config.releaseFrames) ||
    config.attackFrames < 0 ||
    config.releaseFrames < 0 ||
    config.attackFrames > maxEnvelopeFrames ||
    config.releaseFrames > maxEnvelopeFrames
  ) {
    throw opErr(
      i,
      opName,
      "INVALID_DUCKING",
      `attackFrames and releaseFrames must be within [0, ${maxEnvelopeFrames}]`,
    );
  }
  const seen = new Set<string>();
  for (const trackId of config.sidechainTrackIds) {
    if (trackId === track.trackId) {
      throw opErr(
        i,
        opName,
        "INVALID_DUCKING",
        `track "${track.trackId}" cannot duck from itself`,
      );
    }
    if (seen.has(trackId)) {
      throw opErr(i, opName, "INVALID_DUCKING", `duplicate sidechain track "${trackId}"`);
    }
    seen.add(trackId);
    const sidechain = d.tracks.find((candidate) => candidate.trackId === trackId);
    if (!sidechain) {
      throw opErr(i, opName, "INVALID_DUCKING", `sidechain track "${trackId}" not found`);
    }
    if (sidechain.kind !== "audio" && sidechain.kind !== "video") {
      throw opErr(
        i,
        opName,
        "INVALID_DUCKING",
        `sidechain track "${trackId}" must be audio or video`,
      );
    }
  }
}

function applyAddTrack(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "addTrack" }>,
  rec: Recorder,
): void {
  if (!op.track) {
    throw opErr(i, op.op, "BAD_REQUEST", "track is required");
  }
  const t: Track = structuredClone(op.track);
  if (!t.trackId) {
    throw opErr(i, op.op, "BAD_REQUEST", "trackId is required");
  }
  if (t.kind !== "video" && t.kind !== "audio" && t.kind !== "caption") {
    throw opErr(i, op.op, "BAD_REQUEST", `unknown track kind "${t.kind}"`);
  }
  if (t.hidden && t.kind !== "video") {
    throw opErr(i, op.op, "BAD_REQUEST", "hidden only applies to video tracks");
  }
  if (trackIndexByID(d, t.trackId) >= 0) {
    throw opErr(i, op.op, "CONFLICTING_ID", `track "${t.trackId}" already exists`);
  }
  const volume = t.volume ?? 0;
  if (!Number.isFinite(volume) || volume < 0 || volume > 2) {
    throw opErr(i, op.op, "BAD_REQUEST", "track volume must be within [0, 2]");
  }
  validateDuckingConfig(d, i, op.op, t);
  if (d.tracks.length >= MAX_TRACKS) {
    throw opErr(i, op.op, "LIMIT_EXCEEDED", `track count would exceed ${MAX_TRACKS}`);
  }
  let pos = d.tracks.length;
  if (op.index !== undefined && op.index !== null) {
    if (op.index < 0 || op.index > d.tracks.length) {
      throw opErr(
        i,
        op.op,
        "OUT_OF_RANGE",
        `index ${op.index} out of range [0, ${d.tracks.length}]`,
      );
    }
    pos = op.index;
  }
  d.tracks.splice(pos, 0, t);
  rec.addedTracks.push(t.trackId);
}

/** 删除轨道并连带删除轨上全部 item（applyRemoveTrack）。 */
function applyRemoveTrack(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "removeTrack" }>,
  rec: Recorder,
): void {
  const ti = requireUnlockedTrack(d, i, op.op, op.trackId);
  const kept: Item[] = [];
  const removedItemIds = new Set<string>();
  for (const it of d.items) {
    if (it.trackId === op.trackId) {
      rec.removedItems.push(it.itemId);
      removedItemIds.add(it.itemId);
    } else {
      kept.push(it);
    }
  }
  d.items = kept;
  removeTransitionsForItems(d, rec, removedItemIds);
  d.tracks.splice(ti, 1);
  for (const track of d.tracks) {
    if (!track.ducking?.sidechainTrackIds.includes(op.trackId)) continue;
    const sidechainTrackIds = track.ducking.sidechainTrackIds.filter(
      (trackId) => trackId !== op.trackId,
    );
    track.ducking = sidechainTrackIds.length
      ? { ...track.ducking, sidechainTrackIds }
      : undefined;
    rec.updatedTracks.push(track.trackId);
  }
  rec.removedTracks.push(op.trackId);
}

/** 白名单 patch 轨道字段（applyUpdateTrack）。 */
function applyUpdateTrack(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "updateTrack" }>,
  rec: Recorder,
): void {
  if (!op.trackPatch) {
    throw opErr(i, op.op, "BAD_REQUEST", "trackPatch is required");
  }
  const ti = trackIndexByID(d, op.trackId);
  if (ti < 0) {
    throw opErr(i, op.op, "NOT_FOUND", `track "${op.trackId}" not found`);
  }
  const t = d.tracks[ti];
  const p = op.trackPatch;
  // 锁定轨上唯一允许的修改是 locked 字段本身（否则无法解锁）；
  // 其余字段必须先解锁（可在同批内先发一个解锁 patch）。
  if (
    t.locked &&
    (p.name !== undefined ||
      p.muted !== undefined ||
      p.hidden !== undefined ||
      p.volume !== undefined ||
      p.ducking !== undefined)
  ) {
    throw opErr(
      i,
      op.op,
      "LOCKED_TRACK",
      `track "${op.trackId}" is locked; unlock it before changing other fields`,
    );
  }
  if (p.volume !== undefined) {
    if (!Number.isFinite(p.volume) || p.volume < 0 || p.volume > 2) {
      throw opErr(i, op.op, "BAD_REQUEST", "track volume must be within [0, 2]");
    }
    t.volume = p.volume;
  }
  if (p.hidden !== undefined && t.kind !== "video") {
    throw opErr(i, op.op, "BAD_REQUEST", "hidden only applies to video tracks");
  }
  if (p.name !== undefined) t.name = p.name;
  if (p.locked !== undefined) t.locked = p.locked;
  if (p.muted !== undefined) t.muted = p.muted;
  if (p.hidden !== undefined) t.hidden = p.hidden;
  if (p.ducking !== undefined) {
    if (p.ducking.sidechainTrackIds.length === 0) {
      t.ducking = undefined;
      rec.updatedTracks.push(t.trackId);
      return;
    }
    const candidate = { ...t, ducking: structuredClone(p.ducking) };
    validateDuckingConfig(d, i, op.op, candidate);
    t.ducking = candidate.ducking;
  }
  rec.updatedTracks.push(t.trackId);
}

// ---- transitions ----

function visualItem(item: Item): boolean {
  return item.kind === "video" || item.kind === "image";
}

function validateTransitionShape(
  d: TimelineDoc,
  i: number,
  opName: string,
  transition: Transition,
  excludeIndex: number,
): void {
  if (
    transition.transitionId.length === 0 ||
    transition.transitionId.length > 128 ||
    !/^[A-Za-z0-9_-]+$/.test(transition.transitionId)
  ) {
    throw opErr(
      i,
      opName,
      "INVALID_TRANSITION",
      "transitionId must match [A-Za-z0-9_-]{1,128}",
    );
  }
  if (transition.kind !== "crossfade" && transition.kind !== "dipToBlack") {
    throw opErr(
      i,
      opName,
      "INVALID_TRANSITION",
      "transition kind must be crossfade or dipToBlack",
    );
  }
  if (transition.fromItemId === transition.toItemId) {
    throw opErr(i, opName, "INVALID_TRANSITION", "transition items must be different");
  }
  const fromItem = d.items.find((item) => item.itemId === transition.fromItemId);
  if (!fromItem) {
    throw opErr(i, opName, "NOT_FOUND", `from item "${transition.fromItemId}" not found`);
  }
  const toItem = d.items.find((item) => item.itemId === transition.toItemId);
  if (!toItem) {
    throw opErr(i, opName, "NOT_FOUND", `to item "${transition.toItemId}" not found`);
  }
  if (!visualItem(fromItem) || !visualItem(toItem)) {
    throw opErr(
      i,
      opName,
      "INVALID_TRANSITION",
      "transitions require video or image items",
    );
  }
  if (fromItem.trackId !== toItem.trackId) {
    throw opErr(
      i,
      opName,
      "INVALID_TRANSITION",
      "transition items must be on the same track",
    );
  }
  const track = d.tracks.find((candidate) => candidate.trackId === fromItem.trackId);
  if (!track) {
    throw opErr(i, opName, "NOT_FOUND", `track "${fromItem.trackId}" not found`);
  }
  if (track.kind !== "video") {
    throw opErr(i, opName, "INVALID_TRANSITION", "transitions require a video track");
  }
  if (itemEnd(fromItem) !== toItem.from) {
    throw opErr(
      i,
      opName,
      "INVALID_TRANSITION",
      `items "${fromItem.itemId}" and "${toItem.itemId}" must be exactly adjacent`,
    );
  }
  if (
    !Number.isFinite(transition.frames) ||
    !Number.isInteger(transition.frames) ||
    transition.frames <= 0 ||
    transition.frames > toItem.durationInFrames
  ) {
    throw opErr(
      i,
      opName,
      "INVALID_TRANSITION",
      `frames must be positive and no greater than to item duration ${toItem.durationInFrames}`,
    );
  }
  for (const [index, existing] of (d.transitions ?? []).entries()) {
    if (index === excludeIndex) continue;
    if (
      existing.fromItemId === transition.fromItemId &&
      existing.toItemId === transition.toItemId
    ) {
      throw opErr(
        i,
        opName,
        "INVALID_TRANSITION",
        `boundary "${transition.fromItemId}" -> "${transition.toItemId}" already has transition "${existing.transitionId}"`,
      );
    }
  }
  if (fromItem.kind === "video") {
    const media = d.media.find((candidate) => candidate.mediaId === fromItem.mediaId);
    if (!media) {
      throw opErr(i, opName, "NOT_FOUND", `media "${fromItem.mediaId ?? ""}" not found`);
    }
    const speed = effectiveSpeed(fromItem);
    const sourceEnd = (fromItem.trimStart ?? 0) + fromItem.durationInFrames * speed;
    const available = media.durationInFrames - sourceEnd;
    const required = transition.frames * speed;
    if (available + 1e-9 < required) {
      throw opErr(
        i,
        opName,
        "INSUFFICIENT_HANDLE",
        `from item "${fromItem.itemId}" has ${available.toFixed(9)} source frames of handle, needs ${required.toFixed(9)}`,
      );
    }
  }
}

function requireUnlockedTransitionTrack(
  d: TimelineDoc,
  i: number,
  opName: string,
  transition: Transition,
): void {
  const fromItem = d.items.find((item) => item.itemId === transition.fromItemId);
  if (!fromItem) {
    throw opErr(i, opName, "NOT_FOUND", `from item "${transition.fromItemId}" not found`);
  }
  requireUnlockedTrack(d, i, opName, fromItem.trackId);
}

function applyAddTransition(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "addTransition" }>,
  rec: Recorder,
): void {
  if (!op.transition) {
    throw opErr(i, op.op, "BAD_REQUEST", "transition is required");
  }
  const transition = structuredClone(op.transition);
  if (transitionIndexByID(d, transition.transitionId) >= 0) {
    throw opErr(
      i,
      op.op,
      "CONFLICTING_ID",
      `transition "${transition.transitionId}" already exists`,
    );
  }
  if ((d.transitions?.length ?? 0) >= MAX_TRANSITIONS) {
    throw opErr(
      i,
      op.op,
      "LIMIT_EXCEEDED",
      `transition count would exceed ${MAX_TRANSITIONS}`,
    );
  }
  validateTransitionShape(d, i, op.op, transition, -1);
  requireUnlockedTransitionTrack(d, i, op.op, transition);
  (d.transitions ??= []).push(transition);
  rec.addedTransitions.push(transition.transitionId);
}

function applyUpdateTransition(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "updateTransition" }>,
  rec: Recorder,
): void {
  if (!op.transitionPatch) {
    throw opErr(i, op.op, "BAD_REQUEST", "transitionPatch is required");
  }
  if (
    op.transitionPatch.kind === undefined &&
    op.transitionPatch.frames === undefined
  ) {
    throw opErr(
      i,
      op.op,
      "BAD_REQUEST",
      "transitionPatch must update at least one field",
    );
  }
  const transitionIndex = transitionIndexByID(d, op.transitionId);
  if (transitionIndex < 0) {
    throw opErr(i, op.op, "NOT_FOUND", `transition "${op.transitionId}" not found`);
  }
  const transition = structuredClone(d.transitions![transitionIndex]);
  requireUnlockedTransitionTrack(d, i, op.op, transition);
  if (op.transitionPatch.kind !== undefined) transition.kind = op.transitionPatch.kind;
  if (op.transitionPatch.frames !== undefined) {
    transition.frames = op.transitionPatch.frames;
  }
  validateTransitionShape(d, i, op.op, transition, transitionIndex);
  d.transitions![transitionIndex] = transition;
  rec.updatedTransitions.push(transition.transitionId);
}

function applyRemoveTransition(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "removeTransition" }>,
  rec: Recorder,
): void {
  const transitionIndex = transitionIndexByID(d, op.transitionId);
  if (transitionIndex < 0) {
    throw opErr(i, op.op, "NOT_FOUND", `transition "${op.transitionId}" not found`);
  }
  const transition = d.transitions![transitionIndex];
  requireUnlockedTransitionTrack(d, i, op.op, transition);
  d.transitions!.splice(transitionIndex, 1);
  rec.removedTransitions.push(transition.transitionId);
}

function removeTransitionsForItems(
  d: TimelineDoc,
  rec: Recorder,
  itemIds: ReadonlySet<string>,
): void {
  if (itemIds.size === 0 || !d.transitions?.length) return;
  d.transitions = d.transitions.filter((transition) => {
    if (itemIds.has(transition.fromItemId) || itemIds.has(transition.toItemId)) {
      rec.removedTransitions.push(transition.transitionId);
      return false;
    }
    return true;
  });
}

function removeInvalidTransitions(d: TimelineDoc, rec: Recorder): void {
  if (!d.transitions?.length) return;
  const validationDoc = { ...d, transitions: [] };
  d.transitions = d.transitions.filter((transition) => {
    try {
      validateTransitionShape(validationDoc, -1, "", transition, -1);
      return true;
    } catch {
      rec.removedTransitions.push(transition.transitionId);
      return false;
    }
  });
}

// ---- media ----

/** 删除素材；仍被 item 引用时拒绝（applyRemoveMedia）。 */
function applyRemoveMedia(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "removeMedia" }>,
  rec: Recorder,
): void {
  const mi = mediaIndexByID(d, op.mediaId);
  if (mi < 0) {
    throw opErr(i, op.op, "NOT_FOUND", `media "${op.mediaId}" not found`);
  }
  for (const it of d.items) {
    if (it.mediaId === op.mediaId) {
      throw opErr(
        i,
        op.op,
        "STILL_REFERENCED",
        `media "${op.mediaId}" is referenced by item "${it.itemId}"`,
      );
    }
  }
  d.media.splice(mi, 1);
  rec.removedMedia.push(op.mediaId);
}

/** 白名单 patch 素材字段（applyUpdateMedia）。目前只有 displayName（重命名）。 */
function applyUpdateMedia(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "updateMedia" }>,
  rec: Recorder,
): void {
  if (!op.mediaPatch) {
    throw opErr(i, op.op, "BAD_REQUEST", "mediaPatch is required");
  }
  const mi = mediaIndexByID(d, op.mediaId);
  if (mi < 0) {
    throw opErr(i, op.op, "NOT_FOUND", `media "${op.mediaId}" not found`);
  }
  const p = op.mediaPatch;
  if (p.displayName !== undefined) {
    // 空白字符串是显式"清空覆盖名，回退到默认命名"，不是错误——撤销首次
    // 重命名时需要能精确恢复到"未设置"状态。
    const name = p.displayName.trim();
    if (name.length > 256) {
      throw opErr(i, op.op, "BAD_REQUEST", "displayName must be at most 256 characters");
    }
    d.media[mi].displayName = name;
  }
  rec.updatedMedia.push(op.mediaId);
}

// ---- captions / metadata ----

function applySetCaptions(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "setCaptions" }>,
  rec: Recorder,
): void {
  const ti = requireUnlockedTrack(d, i, op.op, op.trackId);
  if (d.tracks[ti].kind !== "caption") {
    throw opErr(
      i,
      op.op,
      "KIND_MISMATCH",
      `setCaptions requires a caption track, got ${d.tracks[ti].kind}`,
    );
  }
  // 先移除本轨全部旧字幕（整轨替换语义）。
  const kept: Item[] = [];
  for (const it of d.items) {
    if (it.trackId === op.trackId) {
      rec.removedItems.push(it.itemId);
    } else {
      kept.push(it);
    }
  }
  d.items = kept;

  const captions = op.captions ?? [];
  for (let ci = 0; ci < captions.length; ci++) {
    const c = captions[ci];
    if (c.from < 0 || c.durationInFrames <= 0) {
      throw opErr(i, op.op, "OUT_OF_RANGE", `caption[${ci}] has invalid geometry`);
    }
    if (!c.text.trim()) {
      throw opErr(i, op.op, "BAD_REQUEST", `caption[${ci}] requires non-empty text`);
    }
    validateCaptionStyle(i, op.op, c.captionStyle);
    const it: Item = {
      itemId: `${op.trackId}_cap${ci}`,
      trackId: op.trackId,
      kind: "text",
      from: c.from,
      durationInFrames: c.durationInFrames,
      text: c.text,
    };
    if (c.stylePresetId) it.stylePresetId = c.stylePresetId;
    if (c.captionStyle) it.captionStyle = structuredClone(c.captionStyle);
    if (itemIndexByID(d, it.itemId) >= 0) {
      throw opErr(
        i,
        op.op,
        "CONFLICTING_ID",
        `derived caption id "${it.itemId}" already exists`,
      );
    }
    if (d.items.length >= MAX_ITEMS) {
      throw opErr(i, op.op, "LIMIT_EXCEEDED", `item count would exceed ${MAX_ITEMS}`);
    }
    d.items.push(it);
    rec.addedItems.push(it.itemId);
  }
}

function applySetMetadata(
  d: TimelineDoc,
  i: number,
  op: Extract<Operation, { op: "setMetadata" }>,
  rec: Recorder,
): void {
  if (!op.metadata) {
    throw opErr(i, op.op, "BAD_REQUEST", "metadata is required");
  }
  const p = op.metadata;
  if (p.fps !== undefined && p.fps !== d.metadata.fps) {
    if (!ALLOWED_FPS.has(p.fps)) {
      throw opErr(i, op.op, "BAD_REQUEST", `fps ${p.fps} not allowed`);
    }
    // 所有帧坐标（含 media.durationInFrames）都按当前 fps 表达，
    // 非空文档改 fps 会让全部坐标失真，MVP 一律禁止。
    if (d.items.length > 0 || d.media.length > 0) {
      throw opErr(i, op.op, "FPS_LOCKED", "cannot change fps on a non-empty timeline");
    }
    d.metadata.fps = p.fps;
  }
  if (p.width !== undefined) {
    if (p.width < 16 || p.width > 8192) {
      throw opErr(i, op.op, "BAD_REQUEST", "width must be within [16, 8192]");
    }
    d.metadata.width = p.width;
  }
  if (p.height !== undefined) {
    if (p.height < 16 || p.height > 8192) {
      throw opErr(i, op.op, "BAD_REQUEST", "height must be within [16, 8192]");
    }
    d.metadata.height = p.height;
  }
  if (p.name !== undefined) {
    d.metadata.name = p.name;
  }
  if (p.backgroundColor !== undefined) {
    d.metadata.backgroundColor = p.backgroundColor;
  }
  rec.metadataChanged = true;
}

// ---- validate.go ----

/**
 * 对整份文档做不变量校验（validate.go 的 ValidateDoc）。
 * applyOps 在整批 op 应用完后调用；从快照恢复、外部导入也应调用。
 * 失败抛 OpError（index=-1 表示批级）。
 */
export function validateDoc(d: TimelineDoc): void {
  if (d.schemaVersion !== SCHEMA_VERSION) {
    throw batchErr("UNSUPPORTED", `schemaVersion ${d.schemaVersion} unsupported`);
  }
  if (!ALLOWED_FPS.has(d.metadata.fps)) {
    throw batchErr("BAD_REQUEST", `metadata.fps ${d.metadata.fps} not allowed`);
  }
  if (
    d.metadata.width < 16 ||
    d.metadata.width > 8192 ||
    d.metadata.height < 16 ||
    d.metadata.height > 8192
  ) {
    throw batchErr(
      "BAD_REQUEST",
      `metadata canvas ${d.metadata.width}x${d.metadata.height} out of range`,
    );
  }
  if (
    d.media.length > MAX_MEDIA ||
    d.tracks.length > MAX_TRACKS ||
    d.items.length > MAX_ITEMS ||
    (d.transitions?.length ?? 0) > MAX_TRANSITIONS
  ) {
    throw batchErr(
      "LIMIT_EXCEEDED",
      `document exceeds size limits (media=${d.media.length} tracks=${d.tracks.length} items=${d.items.length} transitions=${d.transitions?.length ?? 0})`,
    );
  }

  // 唯一性
  const mediaIds = new Set<string>();
  for (const m of d.media) {
    if (!m.mediaId || mediaIds.has(m.mediaId)) {
      throw batchErr("INVARIANT_VIOLATION", `duplicate or empty mediaId "${m.mediaId}"`);
    }
    mediaIds.add(m.mediaId);
  }
  const trackIds = new Set<string>();
  for (const t of d.tracks) {
    if (!t.trackId || trackIds.has(t.trackId)) {
      throw batchErr("INVARIANT_VIOLATION", `duplicate or empty trackId "${t.trackId}"`);
    }
    trackIds.add(t.trackId);
    if (t.kind !== "video" && t.kind !== "audio" && t.kind !== "caption") {
      throw batchErr(
        "INVARIANT_VIOLATION",
        `track "${t.trackId}" has invalid kind "${t.kind}"`,
      );
    }
    if (t.createdBy !== undefined && t.createdBy !== "auto" && t.createdBy !== "user") {
      throw batchErr(
        "INVARIANT_VIOLATION",
        `track "${t.trackId}" has invalid createdBy "${t.createdBy}"`,
      );
    }
    if (t.hidden && t.kind !== "video") {
      throw batchErr(
        "INVARIANT_VIOLATION",
        `track "${t.trackId}" hidden only applies to video tracks`,
      );
    }
    const volume = t.volume ?? 0;
    if (!Number.isFinite(volume) || volume < 0 || volume > 2) {
      throw batchErr(
        "INVARIANT_VIOLATION",
        `track "${t.trackId}" volume must be finite and within [0, 2]`,
      );
    }
  }
  for (const t of d.tracks) {
    try {
      validateDuckingConfig(d, -1, "", t);
    } catch (err) {
      if (err instanceof OpError) {
        throw batchErr(
          "INVARIANT_VIOLATION",
          `track "${t.trackId}" ducking invalid: ${err.detail}`,
        );
      }
      throw err;
    }
  }
  const itemIds = new Set<string>();
  for (const it of d.items) {
    if (!it.itemId || itemIds.has(it.itemId)) {
      throw batchErr("INVARIANT_VIOLATION", `duplicate or empty itemId "${it.itemId}"`);
    }
    itemIds.add(it.itemId);
  }
  const transitionIds = new Set<string>();
  for (const [index, transition] of (d.transitions ?? []).entries()) {
    if (!transition.transitionId || transitionIds.has(transition.transitionId)) {
      throw batchErr(
        "INVARIANT_VIOLATION",
        `duplicate or empty transitionId "${transition.transitionId}"`,
      );
    }
    transitionIds.add(transition.transitionId);
    try {
      validateTransitionShape(d, -1, "", transition, index);
    } catch (err) {
      if (err instanceof OpError) {
        throw batchErr(
          "INVARIANT_VIOLATION",
          `transition "${transition.transitionId}" invalid: ${err.detail}`,
        );
      }
      throw err;
    }
  }

  // 引用、类别、几何（复用单 op 的 shape 校验）
  for (const it of d.items) {
    try {
      validateItemShape(d, -1, "", it);
    } catch (err) {
      if (err instanceof OpError) {
        // 与后端一致：包装成 INVARIANT_VIOLATION 批级错误。
        throw batchErr(
          "INVARIANT_VIOLATION",
          `item "${it.itemId}" invalid: ${err.detail}`,
        );
      }
      throw err;
    }
  }

  // 同轨禁止重叠。按 tracks 声明顺序遍历（与后端一致，保证错误信息确定性）。
  for (const tr of d.tracks) {
    const items = d.items.filter((it) => it.trackId === tr.trackId);
    // 稳定排序：from 升序，同 from 按 itemId。
    items.sort((a, b) =>
      a.from !== b.from ? a.from - b.from : a.itemId < b.itemId ? -1 : a.itemId > b.itemId ? 1 : 0,
    );
    for (let k = 1; k < items.length; k++) {
      if (items[k].from < itemEnd(items[k - 1])) {
        throw batchErr(
          "OVERLAP",
          `items "${items[k - 1].itemId}" and "${items[k].itemId}" overlap on track "${tr.trackId}"`,
        );
      }
    }
  }

  // 文档体积（与后端同一上限；后端算的是 Go json.Marshal 字节数，
  // 两边不会 byte 级一致，但同为 4MiB 量级的护栏即可）。
  const raw = JSON.stringify(d);
  if (raw.length > MAX_DOC_BYTES) {
    throw batchErr(
      "LIMIT_EXCEEDED",
      `document size ${raw.length} exceeds ${MAX_DOC_BYTES} bytes`,
    );
  }
}
