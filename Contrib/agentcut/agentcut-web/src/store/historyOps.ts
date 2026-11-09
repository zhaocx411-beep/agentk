import {
  applyOperationWithoutFinalValidation,
  applyOps,
} from "@/domain/timeline/applyOps";
import type { Item, Media, Operation, TimelineDoc, Transition } from "@/domain/timeline/types";
import { itemEnd } from "@/domain/timeline/types";

/** 为用户操作生成可提交到同一 ApplyOps 协议的反向操作。 */
export function invertOperations(before: TimelineDoc, operations: Operation[]): Operation[] {
  const after = applyOps(before, operations).doc;
  const steps: { before: TimelineDoc; op: Operation }[] = [];
  let cursor = before;
  for (const op of operations) {
    steps.push({ before: cursor, op });
    cursor = applyOperationWithoutFinalValidation(cursor, op);
  }
  const inverse: Operation[] = [];
  for (const step of steps.reverse()) {
    const op = step.op;
    const before = step.before;
    switch (op.op) {
      case "moveItem": {
        const old = requireItem(before, op.itemId);
        inverse.push({ op: "moveItem", itemId: old.itemId, from: old.from, trackId: old.trackId });
        break;
      }
      case "trimStart": {
        const old = requireItem(before, op.itemId);
        inverse.push({ op: "trimStart", itemId: old.itemId, frame: old.from });
        break;
      }
      case "trimEnd": {
        const old = requireItem(before, op.itemId);
        inverse.push({ op: "trimEnd", itemId: old.itemId, frame: itemEnd(old) });
        break;
      }
      case "updateItem": {
        const old = requireItem(before, op.itemId);
        const patch: Extract<Operation, { op: "updateItem" }>["patch"] = {};
        if ("speed" in op.patch) patch.speed = old.speed ?? 1;
        if ("volume" in op.patch) patch.volume = old.volume ?? 1;
        if ("muted" in op.patch) patch.muted = !!old.muted;
        if ("fadeInFrames" in op.patch) patch.fadeInFrames = old.fadeInFrames ?? 0;
        if ("fadeOutFrames" in op.patch) patch.fadeOutFrames = old.fadeOutFrames ?? 0;
        if ("transform" in op.patch) {
          patch.transform = structuredClone(
            old.transform ?? { x: 0, y: 0, width: 1, height: 1, opacity: 1, fit: "contain" },
          );
        }
        if ("text" in op.patch) patch.text = old.text ?? "";
        if ("stylePresetId" in op.patch) patch.stylePresetId = old.stylePresetId ?? "";
        if (Object.keys(patch).length > 0) {
          inverse.push({ op: "updateItem", itemId: old.itemId, patch });
        }
        if ("captionStyle" in op.patch) {
          inverse.push({
            op: "updateItem",
            itemId: old.itemId,
            patch: { captionStyle: {} },
          });
          if (old.captionStyle) {
            inverse.push({
              op: "updateItem",
              itemId: old.itemId,
              patch: { captionStyle: structuredClone(old.captionStyle) },
            });
          }
        }
        break;
      }
      case "updateMedia": {
        const old = requireMedia(before, op.mediaId);
        const mediaPatch: Extract<Operation, { op: "updateMedia" }>["mediaPatch"] = {};
        if ("displayName" in op.mediaPatch) mediaPatch.displayName = old.displayName ?? "";
        inverse.push({ op: "updateMedia", mediaId: old.mediaId, mediaPatch });
        break;
      }
      case "addItem":
        inverse.push({ op: "removeItems", itemIds: [op.item.itemId] });
        break;
      case "removeItems": {
        const removed = op.itemIds.map((id) => structuredClone(requireItem(before, id)));
        inverse.push(...removed.map((item) => ({ op: "addItem" as const, item })));
        break;
      }
      case "split": {
        const old = structuredClone(requireItem(before, op.itemId));
        const rightID = op.newItemId || `${op.itemId}~${op.frame}`;
        inverse.push(
          { op: "removeItems", itemIds: [op.itemId, rightID] },
          { op: "addItem", item: old },
        );
        break;
      }
      case "addTrack":
        inverse.push({ op: "removeTrack", trackId: op.track.trackId });
        break;
      case "removeTrack": {
        const index = before.tracks.findIndex((track) => track.trackId === op.trackId);
        if (index >= 0) {
          inverse.push({
            op: "addTrack",
            track: structuredClone(before.tracks[index]),
            index,
          });
          inverse.push(
            ...before.items
              .filter((item) => item.trackId === op.trackId)
              .map((item) => ({ op: "addItem" as const, item: structuredClone(item) })),
          );
          for (const track of before.tracks) {
            if (
              track.trackId !== op.trackId &&
              track.ducking?.sidechainTrackIds.includes(op.trackId)
            ) {
              pushTrackPatchRespectingLock(inverse, track, {
                ducking: structuredClone(track.ducking),
              });
            }
          }
        }
        break;
      }
      case "updateTrack": {
        const old = before.tracks.find((track) => track.trackId === op.trackId);
        if (!old) throw new Error(`track "${op.trackId}" not found`);
        const trackPatch: Extract<Operation, { op: "updateTrack" }>["trackPatch"] = {};
        if ("name" in op.trackPatch) trackPatch.name = old.name ?? "";
        if ("locked" in op.trackPatch) trackPatch.locked = !!old.locked;
        if ("muted" in op.trackPatch) trackPatch.muted = !!old.muted;
        if ("hidden" in op.trackPatch) trackPatch.hidden = !!old.hidden;
        if ("volume" in op.trackPatch) trackPatch.volume = old.volume ?? 1;
        if ("ducking" in op.trackPatch) {
          trackPatch.ducking = old.ducking
            ? structuredClone(old.ducking)
            : { sidechainTrackIds: [], gain: 0, attackFrames: 0, releaseFrames: 0 };
        }
        const restoresOtherFields = Object.keys(trackPatch).some(
          (field) => field !== "locked",
        );
        if (op.trackPatch.locked === true && restoresOtherFields) {
          inverse.push({
            op: "updateTrack",
            trackId: old.trackId,
            trackPatch: { locked: false },
          });
          delete trackPatch.locked;
        }
        if (Object.keys(trackPatch).length > 0) {
          inverse.push({ op: "updateTrack", trackId: old.trackId, trackPatch });
        }
        break;
      }
      case "setCaptions": {
        inverse.push({
          op: "setCaptions",
          trackId: op.trackId,
          captions: before.items
            .filter((item) => item.trackId === op.trackId && item.kind === "text")
            .sort((a, b) => a.from - b.from)
            .map((item) => ({
              from: item.from,
              durationInFrames: item.durationInFrames,
              text: item.text ?? "",
              stylePresetId: item.stylePresetId,
              captionStyle: item.captionStyle
                ? structuredClone(item.captionStyle)
                : undefined,
            })),
        });
        break;
      }
      case "addTransition":
      case "updateTransition":
      case "removeTransition":
        // Transition state is reconciled after structural inverses so cascaded
        // removals and immutable boundary changes are restored exactly.
        break;
      case "setMetadata": {
        const metadata: Extract<Operation, { op: "setMetadata" }>["metadata"] = {};
        if ("name" in op.metadata) metadata.name = before.metadata.name ?? "";
        if ("width" in op.metadata) metadata.width = before.metadata.width;
        if ("height" in op.metadata) metadata.height = before.metadata.height;
        if ("fps" in op.metadata) metadata.fps = before.metadata.fps;
        if ("backgroundColor" in op.metadata) {
          metadata.backgroundColor = before.metadata.backgroundColor ?? "";
        }
        inverse.push({ op: "setMetadata", metadata });
        break;
      }
      // 媒体登记与 rippleDelete 不由本次手动编辑 UI 产生；发生时清空历史，
      // 不尝试构造可能破坏引用关系的近似反向操作。
      default:
        throw new Error(`operation "${op.op}" cannot be undone safely`);
    }
  }
  inverse.push(...transitionReconciliation(before, after));
  return inverse;
}

export function historyLabel(operations: Operation[]): string {
  const labels: Partial<Record<Operation["op"], string>> = {
    moveItem: "移动片段",
    trimStart: "裁剪片段",
    trimEnd: "裁剪片段",
    updateItem: "编辑片段",
    addItem: "添加片段",
    removeItems: "删除片段",
    split: "分割片段",
    addTrack: "添加轨道",
    removeTrack: "删除轨道",
    updateTrack: "编辑轨道",
    setCaptions: "编辑字幕",
    setMetadata: "编辑项目",
    updateMedia: "重命名素材",
    addTransition: "添加转场",
    updateTransition: "编辑转场",
    removeTransition: "删除转场",
  };
  return operations.length === 1 ? labels[operations[0].op] ?? operations[0].op : "批量编辑";
}

function requireItem(doc: TimelineDoc, itemId: string): Item {
  const item = doc.items.find((candidate) => candidate.itemId === itemId);
  if (!item) throw new Error(`item "${itemId}" not found`);
  return item;
}

function requireMedia(doc: TimelineDoc, mediaId: string): Media {
  const media = doc.media.find((candidate) => candidate.mediaId === mediaId);
  if (!media) throw new Error(`media "${mediaId}" not found`);
  return media;
}

function pushTrackPatchRespectingLock(
  operations: Operation[],
  track: TimelineDoc["tracks"][number],
  trackPatch: Extract<Operation, { op: "updateTrack" }>["trackPatch"],
): void {
  if (track.locked) {
    operations.push({
      op: "updateTrack",
      trackId: track.trackId,
      trackPatch: { locked: false },
    });
  }
  operations.push({ op: "updateTrack", trackId: track.trackId, trackPatch });
  if (track.locked) {
    operations.push({
      op: "updateTrack",
      trackId: track.trackId,
      trackPatch: { locked: true },
    });
  }
}

function transitionReconciliation(before: TimelineDoc, after: TimelineDoc): Operation[] {
  const operations: Operation[] = [];
  const beforeById = new Map(
    (before.transitions ?? []).map((transition) => [transition.transitionId, transition]),
  );
  const afterById = new Map(
    (after.transitions ?? []).map((transition) => [transition.transitionId, transition]),
  );
  const beforeItemIds = new Set(before.items.map((item) => item.itemId));
  const touchedTrackIds = new Set<string>();

  const trackFor = (transition: Transition, doc: TimelineDoc) => {
    const item = doc.items.find((candidate) => candidate.itemId === transition.fromItemId);
    return item?.trackId;
  };
  const touchLockedTrack = (transition: Transition, doc: TimelineDoc) => {
    const trackId = trackFor(transition, doc);
    if (trackId && before.tracks.find((track) => track.trackId === trackId)?.locked) {
      touchedTrackIds.add(trackId);
    }
  };

  const removals: Operation[] = [];
  const updates: Operation[] = [];
  const additions: Operation[] = [];

  for (const transition of after.transitions ?? []) {
    const old = beforeById.get(transition.transitionId);
    if (!old) {
      if (
        beforeItemIds.has(transition.fromItemId) &&
        beforeItemIds.has(transition.toItemId)
      ) {
        touchLockedTrack(transition, before);
        removals.push({
          op: "removeTransition",
          transitionId: transition.transitionId,
        });
      }
      continue;
    }
    if (
      old.fromItemId !== transition.fromItemId ||
      old.toItemId !== transition.toItemId
    ) {
      touchLockedTrack(transition, before);
      removals.push({ op: "removeTransition", transitionId: transition.transitionId });
      touchLockedTrack(old, before);
      additions.push({ op: "addTransition", transition: structuredClone(old) });
    } else if (old.kind !== transition.kind || old.frames !== transition.frames) {
      touchLockedTrack(old, before);
      updates.push({
        op: "updateTransition",
        transitionId: old.transitionId,
        transitionPatch: { kind: old.kind, frames: old.frames },
      });
    }
  }
  for (const transition of before.transitions ?? []) {
    if (afterById.has(transition.transitionId)) continue;
    touchLockedTrack(transition, before);
    additions.push({ op: "addTransition", transition: structuredClone(transition) });
  }

  for (const trackId of touchedTrackIds) {
    operations.push({
      op: "updateTrack",
      trackId,
      trackPatch: { locked: false },
    });
  }
  operations.push(...removals, ...updates, ...additions);
  for (const trackId of touchedTrackIds) {
    operations.push({
      op: "updateTrack",
      trackId,
      trackPatch: { locked: true },
    });
  }
  return operations;
}
