import type { Item, Operation, TimelineDoc } from "./types";
import {
  effectiveSpeed,
  itemEnd,
  itemKindAllowedOn,
  MAX_OPS_PER_BATCH,
} from "./types";

export type TimelineDragMode = "move" | "trimStart" | "trimEnd";

export interface TimelineDragPreview {
  itemId: string;
  mode: TimelineDragMode;
  deltaFrames: number;
  targetTrackId: string;
  snapTarget: number | null;
}

export interface TimelineClipboard {
  items: Item[];
  anchorFrame: number;
}

export interface SelectionRect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export type FadeEdge = "fadeInFrames" | "fadeOutFrames";

export type PasteResult =
  | {
      operations: Extract<Operation, { op: "addItem" }>[];
      itemIds: string[];
    }
  | { error: string };

export function clampEditDelta(
  doc: TimelineDoc,
  item: Item,
  mode: TimelineDragMode,
  delta: number,
): number {
  const speed = effectiveSpeed(item);
  const isAV = item.kind === "video" || item.kind === "audio";
  if (mode === "move") return Math.max(delta, -item.from);
  if (mode === "trimStart") {
    const minLeft = isAV
      ? Math.max(0, item.from - Math.floor((item.trimStart ?? 0) / speed))
      : 0;
    const next = Math.min(Math.max(item.from + delta, minLeft), itemEnd(item) - 1);
    return next - item.from;
  }

  const minEnd = item.from + 1;
  let maxEnd = Number.MAX_SAFE_INTEGER;
  if (isAV && item.mediaId) {
    const media = doc.media.find((candidate) => candidate.mediaId === item.mediaId);
    if (media) {
      maxEnd =
        item.from + Math.floor((media.durationInFrames - (item.trimStart ?? 0)) / speed);
    }
  }
  const next = Math.min(Math.max(itemEnd(item) + delta, minEnd), maxEnd);
  return next - itemEnd(item);
}

export function buildEditOperation(item: Item, drag: TimelineDragPreview): Operation {
  if (drag.mode === "move") {
    return {
      op: "moveItem",
      itemId: item.itemId,
      from: item.from + drag.deltaFrames,
      trackId: drag.targetTrackId,
    };
  }
  if (drag.mode === "trimStart") {
    return { op: "trimStart", itemId: item.itemId, frame: item.from + drag.deltaFrames };
  }
  return { op: "trimEnd", itemId: item.itemId, frame: itemEnd(item) + drag.deltaFrames };
}

export function updateItemSelection(
  selectedItemIds: string[],
  itemId: string,
  additive: boolean,
): string[] {
  if (!additive) return [itemId];
  return selectedItemIds.includes(itemId)
    ? selectedItemIds.filter((selectedId) => selectedId !== itemId)
    : [...selectedItemIds, itemId];
}

export function buildSelectionDeleteOperation(
  selectedItemIds: string[],
): Extract<Operation, { op: "removeItems" }> | null {
  const itemIds = [...new Set(selectedItemIds)];
  return itemIds.length > 0 ? { op: "removeItems", itemIds } : null;
}

export function exceedsPointerThreshold(
  startX: number,
  startY: number,
  clientX: number,
  clientY: number,
  threshold = 4,
): boolean {
  return Math.hypot(clientX - startX, clientY - startY) > threshold;
}

export function normalizeSelectionRect(
  startX: number,
  startY: number,
  endX: number,
  endY: number,
): SelectionRect {
  return {
    left: Math.min(startX, endX),
    top: Math.min(startY, endY),
    right: Math.max(startX, endX),
    bottom: Math.max(startY, endY),
  };
}

export function selectionRectIntersects(
  selection: SelectionRect,
  candidate: SelectionRect,
): boolean {
  return (
    selection.left < candidate.right &&
    selection.right > candidate.left &&
    selection.top < candidate.bottom &&
    selection.bottom > candidate.top
  );
}

export function selectionFromMarquee(
  baseSelection: string[],
  candidates: Array<{ itemId: string; rect: SelectionRect }>,
  marquee: SelectionRect,
  additive: boolean,
): string[] {
  const hits = candidates
    .filter(({ rect }) => selectionRectIntersects(marquee, rect))
    .map(({ itemId }) => itemId);
  return additive ? [...new Set([...baseSelection, ...hits])] : hits;
}

export function createTimelineClipboard(
  doc: TimelineDoc,
  selectedItemIds: string[],
): TimelineClipboard | null {
  const selected = new Set(selectedItemIds);
  const trackOrder = new Map(doc.tracks.map((track, index) => [track.trackId, index]));
  const items = doc.items
    .filter((item) => selected.has(item.itemId))
    .sort(
      (a, b) =>
        a.from - b.from ||
        (trackOrder.get(a.trackId) ?? Number.MAX_SAFE_INTEGER) -
          (trackOrder.get(b.trackId) ?? Number.MAX_SAFE_INTEGER),
    )
    .map((item) => structuredClone(item));
  if (items.length === 0) return null;
  return {
    items,
    anchorFrame: Math.min(...items.map((item) => item.from)),
  };
}

function itemsOverlap(a: Item, b: Item): boolean {
  return a.trackId === b.trackId && a.from < itemEnd(b) && itemEnd(a) > b.from;
}

export function buildPasteOperations(
  doc: TimelineDoc,
  clipboard: TimelineClipboard,
  targetFrame: number,
  createItemId: () => string,
): PasteResult {
  if (clipboard.items.length === 0) return { error: "剪贴板中没有时间线片段" };
  if (clipboard.items.length > MAX_OPS_PER_BATCH) {
    return { error: `一次最多粘贴 ${MAX_OPS_PER_BATCH} 个片段` };
  }

  const shift = Math.max(0, Math.round(targetFrame)) - clipboard.anchorFrame;
  const pasted = clipboard.items.map((source) => ({
    ...structuredClone(source),
    itemId: createItemId(),
    from: source.from + shift,
  }));
  if (pasted.some((item) => item.from < 0)) {
    return { error: "粘贴后有片段超出时间线起点" };
  }

  for (const item of pasted) {
    const track = doc.tracks.find((candidate) => candidate.trackId === item.trackId);
    if (!track) return { error: `原轨道 ${item.trackId} 已不存在，无法粘贴` };
    if (track.locked) return { error: `轨道 ${track.name ?? track.trackId} 已锁定，无法粘贴` };
    if (!itemKindAllowedOn(item.kind, track.kind)) {
      return { error: `片段类型与轨道 ${track.name ?? track.trackId} 不兼容` };
    }
    const conflict = doc.items.find((existing) => itemsOverlap(item, existing));
    if (conflict) {
      return {
        error: `粘贴位置在轨道 ${track.name ?? track.trackId} 与片段 ${conflict.itemId} 重叠`,
      };
    }
  }
  for (let index = 0; index < pasted.length; index += 1) {
    const conflict = pasted
      .slice(0, index)
      .find((candidate) => itemsOverlap(pasted[index], candidate));
    if (conflict) return { error: "复制的片段在同一轨道内发生重叠，无法粘贴" };
  }

  return {
    operations: pasted.map((item) => ({ op: "addItem" as const, item })),
    itemIds: pasted.map((item) => item.itemId),
  };
}

export function duplicateTargetFrame(clipboard: TimelineClipboard): number {
  return Math.max(...clipboard.items.map(itemEnd));
}

export function clampFadeFrames(
  item: Item,
  edge: FadeEdge,
  requestedFrames: number,
): number {
  const other =
    edge === "fadeInFrames"
      ? item.fadeOutFrames ?? 0
      : item.fadeInFrames ?? 0;
  return Math.min(
    Math.max(0, Math.round(requestedFrames)),
    Math.max(0, item.durationInFrames - other),
  );
}

export function fadeDisabledReason(
  doc: TimelineDoc,
  item: Item,
  edge: FadeEdge,
): string | null {
  const transitionOwnsEdge = (doc.transitions ?? []).some((transition) =>
    edge === "fadeInFrames"
      ? transition.toItemId === item.itemId
      : transition.fromItemId === item.itemId,
  );
  return transitionOwnsEdge ? "该边界由转场控制" : null;
}
