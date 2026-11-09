import type { CSSProperties } from "react";

import { fadeEnvelope } from "./previewSemantics";
import type {
  CaptionStyle,
  DuckingConfig,
  Item,
  NormalizedRect,
  TimelineDoc,
  Track,
  Transition,
  ItemTransform,
} from "./timeline/types";
import {
  effectiveOpacity,
  effectiveSpeed,
  effectiveVolume,
  itemEnd,
} from "./timeline/types";

export const FULL_SOURCE_CROP: NormalizedRect = { x: 0, y: 0, width: 1, height: 1 };

export interface FrameInterval {
  start: number;
  end: number;
}

export interface TransitionPreviewState {
  transition: Transition;
  from: Item;
  to: Item;
  start: number;
  end: number;
  progress: number;
  fromOpacity: number;
  toOpacity: number;
  blackOpacity: number;
}

export interface M3BValidationIssue {
  level: "error" | "warning";
  message: string;
}

const HEX_RGB = /^#[0-9a-f]{6}$/i;
const HEX_RGBA = /^#[0-9a-f]{8}$/i;

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

export function itemById(doc: TimelineDoc, itemId: string): Item | undefined {
  return doc.items.find((item) => item.itemId === itemId);
}

export function transitionBoundaries(
  doc: TimelineDoc,
  trackId: string,
): { from: Item; to: Item; transition?: Transition }[] {
  const items = doc.items
    .filter(
      (item) =>
        item.trackId === trackId &&
        (item.kind === "video" || item.kind === "image"),
    )
    .sort((a, b) => a.from - b.from || a.itemId.localeCompare(b.itemId));
  const transitions = new Map(
    (doc.transitions ?? []).map((transition) => [
      `${transition.fromItemId}:${transition.toItemId}`,
      transition,
    ]),
  );
  const boundaries: { from: Item; to: Item; transition?: Transition }[] = [];
  for (let index = 1; index < items.length; index += 1) {
    const from = items[index - 1];
    const to = items[index];
    if (itemEnd(from) !== to.from) continue;
    boundaries.push({
      from,
      to,
      transition: transitions.get(`${from.itemId}:${to.itemId}`),
    });
  }
  return boundaries;
}

export function transitionPreviewAt(
  doc: TimelineDoc,
  currentFrame: number,
): TransitionPreviewState | null {
  for (const transition of doc.transitions ?? []) {
    const from = itemById(doc, transition.fromItemId);
    const to = itemById(doc, transition.toItemId);
    if (!from || !to) continue;
    const start = to.from;
    const end = start + transition.frames;
    if (currentFrame < start || currentFrame >= end) continue;
    const progress = clamp((currentFrame - start) / transition.frames, 0, 1);
    if (transition.kind === "dipToBlack") {
      const fromOpacity = progress < 0.5 ? 1 - progress * 2 : 0;
      const toOpacity = progress < 0.5 ? 0 : progress * 2 - 1;
      return {
        transition,
        from,
        to,
        start,
        end,
        progress,
        fromOpacity,
        toOpacity,
        blackOpacity: 1 - Math.max(fromOpacity, toOpacity),
      };
    }
    return {
      transition,
      from,
      to,
      start,
      end,
      progress,
      fromOpacity: 1 - progress,
      toOpacity: progress,
      blackOpacity: 0,
    };
  }
  return null;
}

/** 转场期间 from 从 nominal source end 后继续读取真实 handle。 */
export function transitionHandleSourceFrame(from: Item, currentFrame: number, start: number): number {
  return (
    (from.trimStart ?? 0) +
    (from.durationInFrames + Math.max(0, currentFrame - start)) * effectiveSpeed(from)
  );
}

export function transitionGainForItem(
  state: TransitionPreviewState | null,
  itemId: string,
): number {
  if (!state) return 1;
  if (itemId === state.from.itemId) return state.fromOpacity;
  if (itemId === state.to.itemId) return state.toOpacity;
  return 1;
}

/**
 * A transition owns the two boundary fades: an outgoing transition replaces
 * that item's fadeOut, and an incoming transition replaces that item's fadeIn.
 * The opposite edge remains unchanged.
 */
export function transitionAwareFadeEnvelope(
  doc: TimelineDoc,
  item: Item,
  currentFrame: number,
): number {
  const transitions = doc.transitions ?? [];
  const ownsFadeIn = transitions.some(
    (transition) => transition.toItemId === item.itemId,
  );
  const ownsFadeOut = transitions.some(
    (transition) => transition.fromItemId === item.itemId,
  );
  if (!ownsFadeIn && !ownsFadeOut) return fadeEnvelope(item, currentFrame);

  const local = Math.max(0, currentFrame - item.from);
  let value = 1;
  const fadeIn = ownsFadeIn ? 0 : item.fadeInFrames ?? 0;
  const fadeOut = ownsFadeOut ? 0 : item.fadeOutFrames ?? 0;
  if (fadeIn > 0 && local < fadeIn) value = Math.min(value, local / fadeIn);
  const remaining = item.durationInFrames - local;
  if (fadeOut > 0 && remaining <= fadeOut) {
    value = Math.min(value, remaining / fadeOut);
  }
  return clamp(value, 0, 1);
}

/**
 * DOM 叠层的视觉 opacity。crossfade 将 from 作为不透明底层、to 以 progress
 * 覆盖，从而使两者的有效贡献仍为 (1-progress)/progress，避免透明底色发黑。
 */
export function transitionVisualOpacity(
  state: TransitionPreviewState | null,
  itemId: string,
): number {
  if (!state) return 1;
  if (state.transition.kind === "crossfade") {
    if (itemId === state.from.itemId) return 1;
    if (itemId === state.to.itemId) return state.toOpacity;
    return 1;
  }
  return transitionGainForItem(state, itemId);
}

/**
 * Picks the longest safe transition up to the one-second UI default.
 * Full-length video clips often retain only a rounding frame of source tail;
 * using that real handle is preferable to presenting a silently disabled drop.
 */
export function transitionFramesForDrop(
  doc: TimelineDoc,
  from: Item,
  to: Item,
): number {
  const requested = Math.max(1, Math.min(doc.metadata.fps, to.durationInFrames));
  if (from.kind !== "video") return requested;
  const media = doc.media.find((candidate) => candidate.mediaId === from.mediaId);
  if (!media) return requested;
  const speed = effectiveSpeed(from);
  const sourceEnd = (from.trimStart ?? 0) + from.durationInFrames * speed;
  const available = Math.max(0, media.durationInFrames - sourceEnd);
  const handleFrames = Math.floor((available + 1e-9) / speed);
  return handleFrames > 0 ? Math.min(requested, handleFrames) : requested;
}

export function transitionDisableReason(
  doc: TimelineDoc,
  from: Item,
  to: Item,
  frames: number,
): string | null {
  const track = doc.tracks.find((candidate) => candidate.trackId === from.trackId);
  if (!track || to.trackId !== from.trackId || track.kind !== "video") {
    return "转场两侧必须位于同一视频轨道";
  }
  if (track.locked) return "轨道已锁定";
  if (!["video", "image"].includes(from.kind) || !["video", "image"].includes(to.kind)) {
    return "仅视频或图片片段支持转场";
  }
  if (itemEnd(from) !== to.from) return "片段必须首尾相邻";
  if (!Number.isInteger(frames) || frames <= 0 || frames > to.durationInFrames) {
    return `时长必须为 1–${to.durationInFrames} 帧`;
  }
  if (from.kind === "video") {
    const media = doc.media.find((candidate) => candidate.mediaId === from.mediaId);
    if (!media) return "转场源素材已缺失";
    const sourceEnd =
      (from.trimStart ?? 0) + from.durationInFrames * effectiveSpeed(from);
    const available = media.durationInFrames - sourceEnd;
    const required = frames * effectiveSpeed(from);
    if (available + 1e-9 < required) {
      return `源尾部 handle 不足：可用 ${Math.max(0, available).toFixed(1)} 帧，需要 ${required.toFixed(1)} 帧`;
    }
  }
  return null;
}

export function sidechainIntervals(doc: TimelineDoc, config: DuckingConfig): FrameInterval[] {
  const selected = new Set(config.sidechainTrackIds);
  const tracks = new Map(doc.tracks.map((track) => [track.trackId, track]));
  const media = new Map(doc.media.map((source) => [source.mediaId, source]));
  const intervals: FrameInterval[] = [];
  for (const item of doc.items) {
    if (!selected.has(item.trackId) || item.muted) continue;
    const track = tracks.get(item.trackId);
    if (!track || track.muted) continue;
    const triggers =
      item.kind === "audio" ||
      (item.kind === "video" && media.get(item.mediaId ?? "")?.probe?.hasAudio === true);
    if (!triggers) continue;
    intervals.push({ start: item.from, end: itemEnd(item) });
  }
  intervals.sort((a, b) => a.start - b.start || a.end - b.end);
  const merged: FrameInterval[] = [];
  for (const interval of intervals) {
    const previous = merged.at(-1);
    if (!previous || interval.start > previous.end) {
      merged.push({ ...interval });
    } else {
      previous.end = Math.max(previous.end, interval.end);
    }
  }
  return merged;
}

export function duckGainAtFrame(
  intervals: FrameInterval[],
  config: DuckingConfig | undefined,
  frame: number,
): number {
  if (!config || intervals.length === 0) return 1;
  let value = 1;
  for (const interval of intervals) {
    if (frame >= interval.start && frame < interval.end) {
      value = Math.min(value, config.gain);
      continue;
    }
    if (config.attackFrames > 0) {
      const attackStart = interval.start - config.attackFrames;
      if (frame >= attackStart && frame < interval.start) {
        const progress = (frame - attackStart) / config.attackFrames;
        value = Math.min(value, 1 - (1 - config.gain) * progress);
      }
    }
    if (config.releaseFrames > 0) {
      const releaseEnd = interval.end + config.releaseFrames;
      if (frame >= interval.end && frame < releaseEnd) {
        const progress = (frame - interval.end) / config.releaseFrames;
        value = Math.min(value, config.gain + (1 - config.gain) * progress);
      }
    }
  }
  return clamp(value, config.gain, 1);
}

export function effectivePreviewGain(
  doc: TimelineDoc,
  track: Track | undefined,
  item: Item,
  currentFrame: number,
  transitionState: TransitionPreviewState | null = null,
): number {
  if (track?.muted || item.muted) return 0;
  const duckIntervals = track?.ducking ? sidechainIntervals(doc, track.ducking) : [];
  return (
    effectiveVolume(track?.volume) *
    effectiveVolume(item.volume) *
    transitionAwareFadeEnvelope(doc, item, currentFrame) *
    duckGainAtFrame(duckIntervals, track?.ducking, currentFrame) *
    transitionGainForItem(transitionState, item.itemId)
  );
}

export function clampCrop(crop: NormalizedRect): NormalizedRect {
  const width = clamp(crop.width, 0.01, 1);
  const height = clamp(crop.height, 0.01, 1);
  return {
    x: clamp(crop.x, 0, 1 - width),
    y: clamp(crop.y, 0, 1 - height),
    width,
    height,
  };
}

export function moveCrop(
  crop: NormalizedRect,
  deltaX: number,
  deltaY: number,
): NormalizedRect {
  return clampCrop({ ...crop, x: crop.x + deltaX, y: crop.y + deltaY });
}

export type CropEdge =
  | "move"
  | "n"
  | "s"
  | "e"
  | "w"
  | "ne"
  | "nw"
  | "se"
  | "sw";

export function resizeCrop(
  crop: NormalizedRect,
  edge: CropEdge,
  deltaX: number,
  deltaY: number,
): NormalizedRect {
  if (edge === "move") return moveCrop(crop, deltaX, deltaY);
  let left = crop.x;
  let top = crop.y;
  let right = crop.x + crop.width;
  let bottom = crop.y + crop.height;
  if (edge.includes("w")) left = clamp(left + deltaX, 0, right - 0.01);
  if (edge.includes("e")) right = clamp(right + deltaX, left + 0.01, 1);
  if (edge.includes("n")) top = clamp(top + deltaY, 0, bottom - 0.01);
  if (edge.includes("s")) bottom = clamp(bottom + deltaY, top + 0.01, 1);
  return {
    x: left,
    y: top,
    width: right - left,
    height: bottom - top,
  };
}

export function normalizeRotation(degrees: number): number {
  if (!Number.isFinite(degrees)) return 0;
  let normalized = degrees % 360;
  if (normalized > 180) normalized -= 360;
  if (normalized < -180) normalized += 360;
  return normalized;
}

/** 应用于源媒体元素；父层先裁剪，再由外层旋转。 */
export function croppedMediaStyle(crop?: NormalizedRect): CSSProperties {
  const value = clampCrop(crop ?? FULL_SOURCE_CROP);
  return {
    position: "absolute",
    left: `${(-value.x / value.width) * 100}%`,
    top: `${(-value.y / value.height) * 100}%`,
    width: `${100 / value.width}%`,
    height: `${100 / value.height}%`,
    maxWidth: "none",
    maxHeight: "none",
  };
}

export function rotationStyle(degrees?: number): CSSProperties {
  return {
    position: "absolute",
    inset: 0,
    transform: `rotate(${normalizeRotation(degrees ?? 0)}deg)`,
    transformOrigin: "50% 50%",
  };
}

export interface VisualFitGeometry {
  crop: NormalizedRect;
  rotationDegrees: number;
  rotatedSourceWidth: number;
  rotatedSourceHeight: number;
  scale: number;
  fittedWidth: number;
  fittedHeight: number;
  offsetX: number;
  offsetY: number;
}

/** crop → rotate → fit 的像素几何；region 本身随后由 transform 放到画布。 */
export function visualFitGeometry(
  sourceWidth: number,
  sourceHeight: number,
  canvasWidth: number,
  canvasHeight: number,
  transform: ItemTransform,
): VisualFitGeometry {
  const crop = clampCrop(transform.crop ?? FULL_SOURCE_CROP);
  const croppedWidth = Math.max(1e-9, sourceWidth * crop.width);
  const croppedHeight = Math.max(1e-9, sourceHeight * crop.height);
  const radians = (normalizeRotation(transform.rotationDegrees ?? 0) * Math.PI) / 180;
  const cosine = Math.abs(Math.cos(radians));
  const sine = Math.abs(Math.sin(radians));
  const rotatedSourceWidth = croppedWidth * cosine + croppedHeight * sine;
  const rotatedSourceHeight = croppedWidth * sine + croppedHeight * cosine;
  const regionWidth = Math.max(1e-9, canvasWidth * transform.width);
  const regionHeight = Math.max(1e-9, canvasHeight * transform.height);
  const scaleX = regionWidth / rotatedSourceWidth;
  const scaleY = regionHeight / rotatedSourceHeight;
  const scale = transform.fit === "cover" ? Math.max(scaleX, scaleY) : Math.min(scaleX, scaleY);
  const fittedWidth = rotatedSourceWidth * scale;
  const fittedHeight = rotatedSourceHeight * scale;
  return {
    crop,
    rotationDegrees: normalizeRotation(transform.rotationDegrees ?? 0),
    rotatedSourceWidth,
    rotatedSourceHeight,
    scale,
    fittedWidth,
    fittedHeight,
    offsetX: (regionWidth - fittedWidth) / 2,
    offsetY: (regionHeight - fittedHeight) / 2,
  };
}

export function fittedRotationStyle(
  sourceWidth: number,
  sourceHeight: number,
  canvasWidth: number,
  canvasHeight: number,
  transform: ItemTransform,
): CSSProperties {
  const geometry = visualFitGeometry(
    sourceWidth,
    sourceHeight,
    canvasWidth,
    canvasHeight,
    transform,
  );
  const regionWidth = Math.max(1e-9, canvasWidth * transform.width);
  const regionHeight = Math.max(1e-9, canvasHeight * transform.height);
  const cropStageWidth = sourceWidth * geometry.crop.width * geometry.scale;
  const cropStageHeight = sourceHeight * geometry.crop.height * geometry.scale;
  return {
    position: "absolute",
    left: "50%",
    top: "50%",
    width: `${(cropStageWidth / regionWidth) * 100}%`,
    height: `${(cropStageHeight / regionHeight) * 100}%`,
    transform: `translate(-50%, -50%) rotate(${geometry.rotationDegrees}deg)`,
    transformOrigin: "50% 50%",
  };
}

export function screenDeltaToSourceCropDelta(
  deltaX: number,
  deltaY: number,
  regionPixelWidth: number,
  regionPixelHeight: number,
  sourceWidth: number,
  sourceHeight: number,
  canvasWidth: number,
  canvasHeight: number,
  transform: ItemTransform,
): { x: number; y: number } {
  const geometry = visualFitGeometry(
    sourceWidth,
    sourceHeight,
    canvasWidth,
    canvasHeight,
    transform,
  );
  const radians = (geometry.rotationDegrees * Math.PI) / 180;
  const localX = deltaX * Math.cos(radians) + deltaY * Math.sin(radians);
  const localY = -deltaX * Math.sin(radians) + deltaY * Math.cos(radians);
  const outputRegionWidth = Math.max(1e-9, canvasWidth * transform.width);
  const outputRegionHeight = Math.max(1e-9, canvasHeight * transform.height);
  const displayScaleX = regionPixelWidth / outputRegionWidth;
  const displayScaleY = regionPixelHeight / outputRegionHeight;
  return {
    x:
      localX /
      Math.max(1e-9, sourceWidth * geometry.scale * displayScaleX),
    y:
      localY /
      Math.max(1e-9, sourceHeight * geometry.scale * displayScaleY),
  };
}

export const CAPTION_PRESETS: Record<string, CaptionStyle> = {
  "subtitle-default": {},
  "subtitle-bold": { bold: true },
  "subtitle-outline": { backgroundColor: "", outlineWidth: 4 },
};

export const DEFAULT_CAPTION_STYLE: Required<CaptionStyle> = {
  fontFamily: "default",
  fontSize: 64,
  textColor: "#FFFFFF",
  backgroundColor: "#00000096",
  outlineColor: "#000000",
  outlineWidth: 2,
  bold: false,
  italic: false,
  align: "center",
  x: 0.5,
  y: 0.8,
  maxWidth: 0.8,
  lineHeight: 1.1,
};

export function resolvedCaptionStyle(
  item: Item,
  canvasHeight = 1080,
): Required<CaptionStyle> {
  return {
    ...DEFAULT_CAPTION_STYLE,
    fontSize: clamp(canvasHeight / 14, 20, 64),
    ...(CAPTION_PRESETS[item.stylePresetId ?? ""] ?? {}),
    ...(item.captionStyle ?? {}),
  };
}

export function captionLayerStyle(item: Item, canvasHeight = 1080): CSSProperties {
  const knownPreset = ["subtitle-default", "subtitle-bold", "subtitle-outline"].includes(
    item.stylePresetId ?? "",
  );
  const hasOverrides =
    !!item.captionStyle &&
    Object.values(item.captionStyle).some((value) => value !== undefined);
  if (!knownPreset && !hasOverrides) {
    return {
      position: "absolute",
      left: "50%",
      bottom: "1.5rem",
      maxWidth: "calc(100% - 3rem)",
      transform: "translateX(-50%)",
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap",
      borderRadius: "0.25rem",
      backgroundColor: "rgb(0 0 0 / 60%)",
      padding: "0.25rem 0.75rem",
      color: "#FFFFFF",
      fontSize: "0.875rem",
      lineHeight: "1.25rem",
      textAlign: "center",
    };
  }
  const style = resolvedCaptionStyle(item, canvasHeight);
  const translateX =
    style.align === "left" ? "0%" : style.align === "right" ? "-100%" : "-50%";
  return {
    position: "absolute",
    left: `${style.x * 100}%`,
    top: `${style.y * 100}%`,
    width: `${style.maxWidth * 100}%`,
    transform: `translateX(${translateX})`,
    color: style.textColor,
    backgroundColor: style.backgroundColor || "transparent",
    WebkitTextStroke: `${style.outlineWidth}px ${style.outlineColor}`,
    paintOrder: "stroke fill",
    fontFamily:
      style.fontFamily === "mono"
        ? "ui-monospace, monospace"
        : style.fontFamily === "serif"
          ? "ui-serif, serif"
          : "Inter, ui-sans-serif, sans-serif",
    fontSize: `${(style.fontSize / Math.max(1, canvasHeight)) * 100}cqh`,
    fontWeight: style.bold ? 700 : 400,
    fontStyle: style.italic ? "italic" : "normal",
    textAlign: style.align,
    lineHeight: style.lineHeight,
    whiteSpace: "pre-wrap",
    overflowWrap: "anywhere",
    padding: style.backgroundColor ? "0.12em 0.24em" : undefined,
  };
}

function validateCaptionStyle(style: CaptionStyle | undefined): string | null {
  if (!style) return null;
  if (
    style.fontFamily !== undefined &&
    !["default", "sans", "serif", "mono"].includes(style.fontFamily)
  ) {
    return "字幕字体仅支持 default/sans/serif/mono";
  }
  const range: [keyof CaptionStyle, number, number][] = [
    ["fontSize", 8, 200],
    ["outlineWidth", 0, 8],
    ["x", 0, 1],
    ["y", 0, 1],
    ["maxWidth", 0.1, 1],
    ["lineHeight", 0.8, 3],
  ];
  for (const [key, min, max] of range) {
    const value = style[key];
    if (
      typeof value === "number" &&
      (!Number.isFinite(value) || value < min || value > max)
    ) {
      return `字幕 ${key} 必须在 ${min}–${max} 之间`;
    }
  }
  if (style.textColor !== undefined && !HEX_RGB.test(style.textColor)) {
    return "字幕文字颜色必须是 #RRGGBB";
  }
  if (
    style.backgroundColor !== undefined &&
    style.backgroundColor !== "" &&
    !HEX_RGBA.test(style.backgroundColor)
  ) {
    return "字幕背景颜色必须是 #RRGGBBAA";
  }
  if (style.outlineColor !== undefined && !HEX_RGB.test(style.outlineColor)) {
    return "字幕描边颜色必须是 #RRGGBB";
  }
  return null;
}

export function validateM3BDoc(doc: TimelineDoc): M3BValidationIssue[] {
  const issues: M3BValidationIssue[] = [];
  const transitionIds = new Set<string>();
  const transitionBoundariesSeen = new Set<string>();
  for (const transition of doc.transitions ?? []) {
    const boundary = `${transition.fromItemId}:${transition.toItemId}`;
    if (
      !transition.transitionId ||
      transitionIds.has(transition.transitionId) ||
      transitionBoundariesSeen.has(boundary) ||
      (transition.kind !== "crossfade" && transition.kind !== "dipToBlack")
    ) {
      issues.push({
        level: "error",
        message: `转场 ${transition.transitionId || "（无 ID）"} 的标识、边界或类型无效`,
      });
      continue;
    }
    transitionIds.add(transition.transitionId);
    transitionBoundariesSeen.add(boundary);
    const from = itemById(doc, transition.fromItemId);
    const to = itemById(doc, transition.toItemId);
    if (!from || !to) {
      issues.push({ level: "error", message: `转场 ${transition.transitionId} 引用了缺失片段` });
      continue;
    }
    const reason = transitionDisableReason(doc, from, to, transition.frames);
    if (reason) {
      issues.push({ level: "error", message: `转场 ${transition.transitionId}：${reason}` });
    }
  }
  const tracksById = new Map(doc.tracks.map((track) => [track.trackId, track]));
  for (const track of doc.tracks) {
    const config = track.ducking;
    if (!config) continue;
    if (
      track.kind !== "audio" ||
      config.sidechainTrackIds.length === 0 ||
      !Number.isFinite(config.gain) ||
      config.gain <= 0 ||
      config.gain > 1 ||
      !Number.isInteger(config.attackFrames) ||
      !Number.isInteger(config.releaseFrames) ||
      config.attackFrames < 0 ||
      config.releaseFrames < 0 ||
      config.attackFrames > doc.metadata.fps * 5 ||
      config.releaseFrames > doc.metadata.fps * 5
    ) {
      issues.push({ level: "error", message: `音轨 ${track.trackId} 的自动闪避配置无效` });
      continue;
    }
    const unique = new Set(config.sidechainTrackIds);
    if (
      unique.size !== config.sidechainTrackIds.length ||
      config.sidechainTrackIds.some((trackId) => {
        const sidechain = tracksById.get(trackId);
        return (
          trackId === track.trackId ||
          !sidechain ||
          (sidechain.kind !== "audio" && sidechain.kind !== "video")
        );
      })
    ) {
      issues.push({
        level: "error",
        message: `音轨 ${track.trackId} 的 sidechain 轨道引用无效`,
      });
    }
  }
  for (const item of doc.items) {
    if (item.transform?.crop) {
      const crop = item.transform.crop;
      if (
        crop.width <= 0 ||
        crop.height <= 0 ||
        crop.x < 0 ||
        crop.y < 0 ||
        crop.x + crop.width > 1 ||
        crop.y + crop.height > 1
      ) {
        issues.push({ level: "error", message: `片段 ${item.itemId} 的裁剪区域无效` });
      }
    }
    const rotation = item.transform?.rotationDegrees;
    if (rotation !== undefined && (!Number.isFinite(rotation) || rotation < -180 || rotation > 180)) {
      issues.push({ level: "error", message: `片段 ${item.itemId} 的旋转角度无效` });
    }
    const captionError = validateCaptionStyle(item.captionStyle);
    if (captionError) {
      issues.push({ level: "error", message: `字幕 ${item.itemId}：${captionError}` });
    }
  }
  if (
    (doc.transitions?.length ?? 0) > 0 ||
    doc.tracks.some((track) => track.ducking) ||
    doc.items.some(
      (item) =>
        item.transform?.crop ||
        item.transform?.rotationDegrees ||
        (item.captionStyle &&
          Object.values(item.captionStyle).some((value) => value !== undefined)) ||
        ["subtitle-default", "subtitle-bold", "subtitle-outline"].includes(
          item.stylePresetId ?? "",
        ),
    )
  ) {
    issues.push({
      level: "warning",
      message: "M3B 效果需要 Local Executor；Agent-K Executor 不会静默降级。",
    });
  }
  return issues;
}

export function itemVisualOpacity(
  doc: TimelineDoc,
  item: Item,
  currentFrame: number,
): number {
  return (
    effectiveOpacity(item) *
    transitionAwareFadeEnvelope(doc, item, currentFrame)
  );
}
