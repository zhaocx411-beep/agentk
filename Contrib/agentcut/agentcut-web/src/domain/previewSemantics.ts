import type { CSSProperties } from "react";

import type { Item, ItemTransform, Track } from "./timeline/types";
import { effectiveOpacity, effectiveVolume } from "./timeline/types";

export function effectiveItemGain(track: Track | undefined, item: Item): number {
  if (track?.muted || item.muted) return 0;
  return effectiveVolume(track?.volume) * effectiveVolume(item.volume);
}

/** 线性淡入淡出包络；currentFrame 使用共享 Timeline 帧空间。 */
export function fadeEnvelope(item: Item, currentFrame: number): number {
  const local = Math.max(0, currentFrame - item.from);
  const fadeIn = item.fadeInFrames ?? 0;
  const fadeOut = item.fadeOutFrames ?? 0;
  let value = 1;
  if (fadeIn > 0 && local < fadeIn) value = Math.min(value, local / fadeIn);
  const remaining = item.durationInFrames - local;
  if (fadeOut > 0 && remaining <= fadeOut) value = Math.min(value, remaining / fadeOut);
  return Math.max(0, Math.min(1, value));
}

export const FULL_CANVAS_TRANSFORM: ItemTransform = {
  x: 0,
  y: 0,
  width: 1,
  height: 1,
  opacity: 1,
  fit: "contain",
};

export function effectiveTransform(item: Item): ItemTransform {
  return item.transform ?? FULL_CANVAS_TRANSFORM;
}

export function visualLayerStyle(item: Item, currentFrame: number): CSSProperties {
  const transform = effectiveTransform(item);
  return {
    left: `${transform.x * 100}%`,
    top: `${transform.y * 100}%`,
    width: `${transform.width * 100}%`,
    height: `${transform.height * 100}%`,
    opacity: effectiveOpacity(item) * fadeEnvelope(item, currentFrame),
  };
}

export function hasApproximatePreview(items: Item[]): boolean {
  return items.some(
    (item) =>
      (item.kind === "audio" || item.kind === "video") &&
      effectiveVolume(item.volume) > 1,
  );
}

