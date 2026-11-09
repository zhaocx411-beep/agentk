import type { KeyboardEvent, MouseEvent, PointerEvent } from "react";

import type { Item } from "@/domain/timeline/types";

interface CaptionClipProps {
  item: Item;
  left: number;
  width: number;
  selected: boolean;
  muted: boolean;
  locked: boolean;
  onOpenEditor(): void;
  onKeyDown(event: KeyboardEvent<HTMLButtonElement>): void;
  onContextMenu(event: MouseEvent<HTMLButtonElement>): void;
  onPointerDown(event: PointerEvent<HTMLButtonElement>): void;
}

export function CaptionClip({
  item,
  left,
  width,
  selected,
  muted,
  locked,
  onOpenEditor,
  onKeyDown,
  onContextMenu,
  onPointerDown,
}: CaptionClipProps) {
  const text = item.text?.trim() || "空字幕";
  const showText = width >= 32;
  return (
    <button
      type="button"
      data-testid={`timeline-clip-${item.itemId}`}
      data-timeline-item-id={item.itemId}
      aria-pressed={selected}
      aria-label={`${text}，字幕`}
      className={`group/caption absolute inset-y-1 min-w-4 rounded-[3px] border text-left text-[11px] ${
        muted
          ? "border-violet-300/35 bg-violet-200/20 text-violet-900/55 opacity-60 dark:border-violet-700/35 dark:bg-violet-950/25 dark:text-violet-200/55"
          : "border-violet-400/45 bg-violet-300/50 text-violet-950/80 dark:border-violet-600/45 dark:bg-violet-900/55 dark:text-violet-100/80"
      } ${
        selected
          ? "z-10 ring-2 ring-orange-500"
          : "hover:border-violet-700 dark:hover:border-violet-300"
      } ${locked ? "cursor-not-allowed" : "cursor-pointer"}`}
      style={{ left, width: Math.max(16, width) }}
      onDoubleClick={onOpenEditor}
      onKeyDown={onKeyDown}
      onContextMenu={onContextMenu}
      onPointerDown={onPointerDown}
    >
      <span className="block h-full overflow-hidden px-2">
        {showText && (
          <span className="block truncate leading-[26px]">{text}</span>
        )}
      </span>
      <span
        role="tooltip"
        className="pointer-events-none absolute bottom-[calc(100%+6px)] left-1/2 z-50 hidden max-w-72 -translate-x-1/2 whitespace-nowrap rounded-[3px] border border-white/10 bg-[#171411] px-2 py-1 text-[11px] text-white group-hover/caption:block group-focus-visible/caption:block"
      >
        {muted ? `${text}（字幕轨已关闭）` : text}
      </span>
    </button>
  );
}
