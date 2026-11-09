import type { Item, TimelineDoc } from "./types";
import { docDurationInFrames, itemEnd } from "./types";

export interface SnapResult {
  frame: number;
  target: number | null;
}

/** 生成拖拽/裁剪吸附候选：0、播放头、总时长、目标轨道内其它片段首尾。 */
export function snapTargets(
  doc: TimelineDoc,
  trackId: string,
  excludeItemId: string,
  playhead: number,
): number[] {
  const values = new Set<number>([0, Math.round(playhead), docDurationInFrames(doc)]);
  for (const item of doc.items) {
    if (item.itemId === excludeItemId || item.trackId !== trackId) continue;
    values.add(item.from);
    values.add(itemEnd(item));
  }
  return [...values].sort((a, b) => a - b);
}

/**
 * 将片段边界吸附到最近候选。move 同时比较左、右边界；trim 只比较被拖边界。
 * thresholdPx 固定为视觉阈值，按 pxPerFrame 换算后缩放级别不改变手感。
 */
export function snapFrame(
  rawFrame: number,
  targets: number[],
  pxPerFrame: number,
  thresholdPx = 8,
  alternateBoundaryOffset = 0,
): SnapResult {
  const thresholdFrames = thresholdPx / Math.max(pxPerFrame, 0.001);
  let bestFrame = rawFrame;
  let bestTarget: number | null = null;
  let bestDistance = Number.POSITIVE_INFINITY;
  for (const target of targets) {
    for (const offset of alternateBoundaryOffset ? [0, alternateBoundaryOffset] : [0]) {
      const alignedFrame = target - offset;
      const distance = Math.abs(alignedFrame - rawFrame);
      if (distance <= thresholdFrames && distance < bestDistance) {
        bestDistance = distance;
        bestFrame = alignedFrame;
        bestTarget = target;
      }
    }
  }
  return { frame: Math.round(bestFrame), target: bestTarget };
}

export function compatibleTargetTrack(
  doc: TimelineDoc,
  item: Item,
  trackId: string | null,
): string {
  if (!trackId) return item.trackId;
  const track = doc.tracks.find((candidate) => candidate.trackId === trackId);
  if (!track || track.locked) return item.trackId;
  if (
    (track.kind === "video" && !["video", "image"].includes(item.kind)) ||
    (track.kind === "audio" && item.kind !== "audio") ||
    (track.kind === "caption" && item.kind !== "text")
  ) {
    return item.trackId;
  }
  return track.trackId;
}
