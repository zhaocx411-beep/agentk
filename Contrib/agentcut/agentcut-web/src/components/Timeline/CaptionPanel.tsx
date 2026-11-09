import { Save, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  backgroundAlpha,
  backgroundRGB,
  buildCaptionItemOperations,
  buildCaptionTrackOperation,
  CAPTION_STYLE_PRESETS,
  captionDraftFromItem,
  captionDraftForNewItem,
  captionPreviewPatch,
  captionStyleHasValues,
  validateCaptionDraft,
  withBackgroundAlpha,
  type CaptionDraft,
} from "@/domain/captionEditing";
import { newItemId } from "@/domain/timeline/insertMedia";
import { frameToSeconds, secondsToFrame } from "@/domain/timeline/time";
import type {
  CaptionAlign,
  CaptionFontFamily,
  Item,
  Operation,
  TimelineDoc,
} from "@/domain/timeline/types";
import { usePreviewOverridesStore } from "@/store/previewOverridesStore";

interface CaptionPanelProps {
  doc: TimelineDoc;
  item: Item | null;
  newCaptionTrackId?: string;
  initialFrame?: number;
  onClose(): void;
  onSubmit(operations: Operation[]): void;
}

export function CaptionPanel({
  doc,
  item,
  newCaptionTrackId,
  initialFrame = 0,
  onClose,
  onSubmit,
}: CaptionPanelProps) {
  const fps = doc.metadata.fps;
  const isNew = !item && !!newCaptionTrackId;
  const [draft, setDraft] = useState<CaptionDraft>(() =>
    item
      ? captionDraftFromItem(item, fps)
      : captionDraftForNewItem(initialFrame, fps),
  );
  const [error, setError] = useState<string | null>(null);
  const setPreviewPatch = usePreviewOverridesStore(
    (state) => state.setItemPatch,
  );
  const clearPreviewPatch = usePreviewOverridesStore(
    (state) => state.clearItemPatch,
  );
  const track = doc.tracks.find(
    (candidate) => candidate.trackId === (item?.trackId ?? newCaptionTrackId),
  );
  const locked = !!track?.locked;

  useEffect(() => {
    setDraft(
      item
        ? captionDraftFromItem(item, fps)
        : captionDraftForNewItem(initialFrame, fps),
    );
    setError(null);
  }, [fps, initialFrame, item, newCaptionTrackId]);

  useEffect(() => {
    if (!item) return;
    setPreviewPatch(item.itemId, captionPreviewPatch(draft));
  }, [draft, item, setPreviewPatch]);

  useEffect(
    () => () => {
      if (item) clearPreviewPatch(item.itemId);
    },
    [clearPreviewPatch, item],
  );

  function submit(mode: "single" | "all") {
    const message = validateCaptionDraft(draft, fps);
    if (message) {
      setError(message);
      return;
    }
    if (locked) {
      setError("字幕轨已锁定，请先解锁后编辑");
      return;
    }
    if (isNew && newCaptionTrackId) {
      const from = secondsToFrame(draft.fromSec, fps);
      const durationInFrames = secondsToFrame(draft.durationSec, fps);
      const operations: Operation[] = [];
      if (!track) {
        operations.push({
          op: "addTrack",
          track: {
            trackId: newCaptionTrackId,
            kind: "caption",
            name: "字幕轨",
          },
        });
      }
      operations.push({
        op: "addItem",
        item: {
          itemId: newItemId(),
          trackId: newCaptionTrackId,
          kind: "text",
          from,
          durationInFrames,
          text: draft.text.trim(),
          stylePresetId: draft.stylePresetId || undefined,
          captionStyle: captionStyleHasValues(draft.captionStyle)
            ? structuredClone(draft.captionStyle)
            : undefined,
        },
      });
      onSubmit(operations);
      onClose();
      return;
    }
    if (!item) return;
    const operations =
      mode === "all"
        ? [buildCaptionTrackOperation(doc, item, draft, fps)]
        : buildCaptionItemOperations(item, draft, fps);
    if (operations.length > 0) onSubmit(operations);
    onClose();
  }

  function patch(next: Partial<CaptionDraft>) {
    setDraft((current) => ({ ...current, ...next }));
  }

  function patchStyle(
    next: Partial<CaptionDraft["captionStyle"]>,
  ) {
    setDraft((current) => ({
      ...current,
      captionStyle: { ...current.captionStyle, ...next },
    }));
  }

  return (
    <div
      role="dialog"
      aria-label={isNew ? "新增字幕" : "编辑字幕"}
      data-timeline-shortcuts-disabled
      className="fixed right-3 bottom-60 z-50 max-h-[calc(100vh-16rem)] w-[360px] max-w-[calc(100vw-1.5rem)] overflow-y-auto rounded-md border bg-background p-3 shadow-xl"
    >
      <div className="mb-3 flex items-center">
        <strong className="text-sm">
          {isNew ? "新增 Caption 字幕" : "Caption 字幕"}
        </strong>
        <span className="ml-2 truncate text-[10px] text-muted-foreground">
          {track?.name ?? newCaptionTrackId ?? item?.trackId}
        </span>
        <div className="flex-1" />
        {item && (
          <Button
            variant="ghost"
            size="icon-sm"
            title="删除字幕"
            aria-label="删除字幕"
            disabled={locked}
            onClick={() => {
              onSubmit([{ op: "removeItems", itemIds: [item.itemId] }]);
              onClose();
            }}
          >
            <Trash2 className="text-destructive" />
          </Button>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="关闭字幕面板"
          onClick={onClose}
        >
          <X />
        </Button>
      </div>

      <label className="mb-2 block text-xs">
        字幕文字
        <Input
          className="mt-1 h-8"
          value={draft.text}
          onChange={(event) => patch({ text: event.target.value })}
        />
      </label>

      <div className="grid grid-cols-2 gap-2">
        <NumberField
          label="起点（秒）"
          value={draft.fromSec}
          min={0}
          onChange={(fromSec) => patch({ fromSec })}
        />
        <NumberField
          label="时长（秒）"
          value={draft.durationSec}
          min={frameToSeconds(1, fps)}
          onChange={(durationSec) => patch({ durationSec })}
        />
        <label className="col-span-2 text-xs">
          模板 / Preset
          <select
            aria-label="字幕模板"
            className="mt-1 h-8 w-full rounded-md border bg-background px-2 text-sm"
            value={draft.stylePresetId}
            onChange={(event) =>
              patch({ stylePresetId: event.target.value })
            }
          >
            {CAPTION_STYLE_PRESETS.map((preset) => (
              <option key={preset.value} value={preset.value}>
                {preset.label}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs">
          字体
          <select
            aria-label="字幕字体"
            className="mt-1 h-8 w-full rounded-md border bg-background px-2 text-sm"
            value={draft.captionStyle.fontFamily ?? "default"}
            onChange={(event) =>
              patchStyle({
                fontFamily: event.target.value as CaptionFontFamily,
              })
            }
          >
            {(["default", "sans", "serif", "mono"] as const).map((family) => (
              <option key={family}>{family}</option>
            ))}
          </select>
        </label>
        <NumberField
          label="字号"
          value={
            draft.captionStyle.fontSize ??
            Math.max(20, Math.min(64, doc.metadata.height / 14))
          }
          min={8}
          max={200}
          step={1}
          onChange={(fontSize) => patchStyle({ fontSize })}
        />
        <ColorField
          label="文字颜色"
          value={draft.captionStyle.textColor ?? "#FFFFFF"}
          onChange={(textColor) => patchStyle({ textColor })}
        />
        <ColorField
          label="描边颜色"
          value={draft.captionStyle.outlineColor ?? "#000000"}
          onChange={(outlineColor) => patchStyle({ outlineColor })}
        />
        <NumberField
          label="描边宽度"
          value={draft.captionStyle.outlineWidth ?? 2}
          min={0}
          max={8}
          onChange={(outlineWidth) => patchStyle({ outlineWidth })}
        />
        <ColorField
          label="背景颜色"
          value={backgroundRGB(draft.captionStyle.backgroundColor)}
          onChange={(rgb) =>
            patchStyle({
              backgroundColor: withBackgroundAlpha(
                rgb,
                backgroundAlpha(draft.captionStyle.backgroundColor),
              ),
            })
          }
        />
        <label className="text-xs">
          背景透明度
          <input
            aria-label="字幕背景透明度"
            className="mt-2 w-full"
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={backgroundAlpha(draft.captionStyle.backgroundColor)}
            onChange={(event) =>
              patchStyle({
                backgroundColor: withBackgroundAlpha(
                  backgroundRGB(draft.captionStyle.backgroundColor),
                  Number(event.target.value),
                ),
              })
            }
          />
        </label>
        <label className="flex items-center gap-2 text-xs">
          <input
            type="checkbox"
            checked={draft.captionStyle.bold ?? false}
            onChange={(event) => patchStyle({ bold: event.target.checked })}
          />
          粗体
        </label>
        <label className="flex items-center gap-2 text-xs">
          <input
            type="checkbox"
            checked={draft.captionStyle.italic ?? false}
            onChange={(event) => patchStyle({ italic: event.target.checked })}
          />
          斜体
        </label>
        <label className="col-span-2 text-xs">
          对齐
          <select
            aria-label="字幕对齐"
            className="mt-1 h-8 w-full rounded-md border bg-background px-2 text-sm"
            value={draft.captionStyle.align ?? "center"}
            onChange={(event) =>
              patchStyle({ align: event.target.value as CaptionAlign })
            }
          >
            <option value="left">left</option>
            <option value="center">center</option>
            <option value="right">right</option>
          </select>
        </label>
        {(["x", "y", "maxWidth", "lineHeight"] as const).map((field) => (
          <NumberField
            key={field}
            label={field}
            value={
              draft.captionStyle[field] ??
              (field === "x"
                ? 0.5
                : field === "y"
                  ? 0.8
                  : field === "maxWidth"
                    ? 0.8
                    : 1.1)
            }
            min={
              field === "lineHeight" ? 0.8 : field === "maxWidth" ? 0.1 : 0
            }
            max={field === "lineHeight" ? 3 : 1}
            onChange={(value) => patchStyle({ [field]: value })}
          />
        ))}
      </div>

      {locked && (
        <p className="mt-2 text-xs text-amber-600">字幕轨已锁定，只能查看。</p>
      )}
      {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
      <div className="mt-3 flex justify-end gap-2">
        {!isNew && (
          <Button
            size="sm"
            variant="outline"
            disabled={locked}
            onClick={() => submit("all")}
          >
            应用到全部字幕
          </Button>
        )}
        <Button size="sm" disabled={locked} onClick={() => submit("single")}>
          <Save />
          {isNew ? "新增字幕" : "保存当前字幕"}
        </Button>
      </div>
    </div>
  );
}

function NumberField({
  label,
  value,
  min,
  max,
  step = 0.01,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max?: number;
  step?: number;
  onChange(value: number): void;
}) {
  return (
    <label className="text-xs">
      {label}
      <Input
        className="mt-1 h-8"
        type="number"
        step={step}
        min={min}
        max={max}
        value={Number.isFinite(value) ? value : 0}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </label>
  );
}

function ColorField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange(value: string): void;
}) {
  return (
    <label className="text-xs">
      {label}
      <Input
        aria-label={label}
        className="mt-1 h-8 p-1"
        type="color"
        value={value}
        onChange={(event) => onChange(event.target.value.toUpperCase())}
      />
    </label>
  );
}
