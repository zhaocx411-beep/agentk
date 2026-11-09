import { frameToSeconds, secondsToFrame } from "./timeline/time";
import type {
  CaptionStyle,
  Item,
  ItemPatch,
  Operation,
  TimelineDoc,
} from "./timeline/types";

export const CAPTION_STYLE_PRESETS = [
  { value: "", label: "默认样式" },
  { value: "subtitle-default", label: "标准字幕" },
  { value: "subtitle-bold", label: "粗体字幕" },
  { value: "subtitle-outline", label: "描边字幕" },
] as const;

export interface CaptionDraft {
  text: string;
  fromSec: number;
  durationSec: number;
  stylePresetId: string;
  captionStyle: CaptionStyle;
}

export function captionDraftFromItem(item: Item, fps: number): CaptionDraft {
  return {
    text: item.text ?? "",
    fromSec: frameToSeconds(item.from, fps),
    durationSec: frameToSeconds(item.durationInFrames, fps),
    stylePresetId: item.stylePresetId ?? "",
    captionStyle: structuredClone(item.captionStyle ?? {}),
  };
}

export function captionDraftForNewItem(
  initialFrame: number,
  fps: number,
): CaptionDraft {
  return {
    text: "",
    fromSec: frameToSeconds(initialFrame, fps),
    durationSec: frameToSeconds(Math.max(1, fps * 2), fps),
    stylePresetId: "",
    captionStyle: {},
  };
}

export function captionStyleHasValues(style: CaptionStyle): boolean {
  return Object.values(style).some((value) => value !== undefined);
}

export function validateCaptionDraft(
  draft: CaptionDraft,
  fps: number,
): string | null {
  const from = secondsToFrame(draft.fromSec, fps);
  const duration = secondsToFrame(draft.durationSec, fps);
  if (from < 0 || duration <= 0) {
    return "起始时间必须 ≥ 0，时长必须 > 0";
  }
  if (!draft.text.trim()) return "字幕文本不能为空";
  const style = draft.captionStyle;
  if (
    (style.fontSize !== undefined &&
      (style.fontSize < 8 || style.fontSize > 200)) ||
    (style.outlineWidth !== undefined &&
      (style.outlineWidth < 0 || style.outlineWidth > 8)) ||
    (style.maxWidth !== undefined &&
      (style.maxWidth < 0.1 || style.maxWidth > 1)) ||
    (style.lineHeight !== undefined &&
      (style.lineHeight < 0.8 || style.lineHeight > 3))
  ) {
    return "字幕样式参数超出后端允许范围";
  }
  return null;
}

export function captionPreviewPatch(draft: CaptionDraft): ItemPatch {
  return {
    text: draft.text,
    stylePresetId: draft.stylePresetId,
    captionStyle: structuredClone(draft.captionStyle),
  };
}

export function buildCaptionItemOperations(
  item: Item,
  draft: CaptionDraft,
  fps: number,
): Operation[] {
  const from = secondsToFrame(draft.fromSec, fps);
  const duration = secondsToFrame(draft.durationSec, fps);
  const operations: Operation[] = [];
  if (from !== item.from) {
    operations.push({ op: "moveItem", itemId: item.itemId, from });
  }
  if (duration !== item.durationInFrames) {
    operations.push({
      op: "trimEnd",
      itemId: item.itemId,
      frame: from + duration,
    });
  }
  const patch: Extract<Operation, { op: "updateItem" }>["patch"] = {};
  const text = draft.text.trim();
  if (text !== item.text) patch.text = text;
  if (draft.stylePresetId !== (item.stylePresetId ?? "")) {
    patch.stylePresetId = draft.stylePresetId;
  }
  if (
    JSON.stringify(draft.captionStyle) !==
    JSON.stringify(item.captionStyle ?? {})
  ) {
    patch.captionStyle = structuredClone(draft.captionStyle);
  }
  if (Object.keys(patch).length > 0) {
    operations.push({ op: "updateItem", itemId: item.itemId, patch });
  }
  return operations;
}

export function buildCaptionTrackOperation(
  doc: TimelineDoc,
  item: Item,
  draft: CaptionDraft,
  fps: number,
): Operation {
  const selectedFrom = secondsToFrame(draft.fromSec, fps);
  const selectedDuration = secondsToFrame(draft.durationSec, fps);
  const captionStyle = captionStyleHasValues(draft.captionStyle)
    ? structuredClone(draft.captionStyle)
    : undefined;
  const captions = doc.items
    .filter(
      (candidate) =>
        candidate.trackId === item.trackId && candidate.kind === "text",
    )
    .map((candidate) => ({
      from: candidate.itemId === item.itemId ? selectedFrom : candidate.from,
      durationInFrames:
        candidate.itemId === item.itemId
          ? selectedDuration
          : candidate.durationInFrames,
      text:
        candidate.itemId === item.itemId
          ? draft.text.trim()
          : (candidate.text ?? ""),
      stylePresetId: draft.stylePresetId || undefined,
      captionStyle,
    }))
    .toSorted((a, b) => a.from - b.from || a.text.localeCompare(b.text));
  return { op: "setCaptions", trackId: item.trackId, captions };
}

export function backgroundRGB(value?: string): string {
  return value && /^#[0-9a-f]{8}$/i.test(value) ? value.slice(0, 7) : "#000000";
}

export function backgroundAlpha(value?: string): number {
  if (!value || !/^#[0-9a-f]{8}$/i.test(value)) return 150 / 255;
  return Number.parseInt(value.slice(7), 16) / 255;
}

export function withBackgroundAlpha(rgb: string, alpha: number): string {
  const hex = Math.round(Math.max(0, Math.min(1, alpha)) * 255)
    .toString(16)
    .padStart(2, "0");
  return `${rgb}${hex}`.toUpperCase();
}
