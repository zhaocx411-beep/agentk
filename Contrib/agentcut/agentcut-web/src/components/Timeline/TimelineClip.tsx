import { VolumeX } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";
import type {
  KeyboardEvent,
  MouseEvent,
  PointerEvent,
} from "react";

import type { FadeEdge } from "@/domain/timeline/editing";
import { THUMBNAIL_FRAME_COUNT } from "@/domain/timeline/types";
import type { Item, Media, TrackKind } from "@/domain/timeline/types";

import {
  resampleWaveformPeaks,
  visibleSourceRange,
  waveformDisplayGain,
} from "./timelineWaveform";

export type TimelineEditMode = "select" | "blade";

const HANDLE_PX = 6;
const TRACK_COLOR: Record<TrackKind, string> = {
  video: "border-sky-700/55 bg-slate-700",
  audio:
    "border-emerald-700/45 bg-emerald-100 dark:border-emerald-500/45 dark:bg-emerald-950",
  caption:
    "border-violet-700/45 bg-violet-100 dark:border-violet-500/45 dark:bg-violet-950",
};

interface TimelineClipProps {
  item: Item;
  media?: Media;
  trackKind: TrackKind;
  trackLocked: boolean;
  trackMuted?: boolean;
  editMode: TimelineEditMode;
  left: number;
  width: number;
  pxPerFrame: number;
  selected: boolean;
  dragging: boolean;
  agentTouched: boolean;
  trimTargetFrame: number | null;
  fps: number;
  fadePreviewEdge?: FadeEdge;
  fadeInDisabledReason?: string | null;
  fadeOutDisabledReason?: string | null;
  onKeyDown(event: KeyboardEvent<HTMLDivElement>): void;
  onContextMenu(event: MouseEvent<HTMLDivElement>): void;
  onPointerDown(event: PointerEvent<HTMLDivElement>): void;
  onTrimPointerDown(
    event: PointerEvent<HTMLDivElement>,
    mode: "trimStart" | "trimEnd",
  ): void;
  onFadePointerDown(event: PointerEvent<HTMLButtonElement>, edge: FadeEdge): void;
}

function clipDisplayName(item: Item, media: Media | undefined): string {
  if (item.text) return item.text;
  if (media?.displayName) return media.displayName;
  if (media?.blobKey) {
    const name = media.blobKey.split("/").pop();
    if (name) return name;
  }
  if (media) {
    return { video: "未命名视频", audio: "未命名音频", image: "未命名图片" }[
      media.kind
    ];
  }
  return "素材片段";
}

function Filmstrip({
  thumbnailUri,
  range,
  clipWidthPx,
  bottomPx,
}: {
  thumbnailUri: string;
  range: [number, number];
  clipWidthPx: number;
  bottomPx: number;
}) {
  const [start, end] = range;
  const tileWidth = 72;
  const tileCount = Math.max(1, Math.ceil(clipWidthPx / tileWidth));
  return (
    <div
      data-testid="video-filmstrip"
      className="pointer-events-none absolute inset-x-0 top-0 flex overflow-hidden bg-slate-800"
      style={{ bottom: bottomPx }}
    >
      {Array.from({ length: tileCount }, (_, index) => {
        const sourceRatio =
          start + ((index + 0.5) / tileCount) * Math.max(0, end - start);
        const frameIndex = Math.min(
          THUMBNAIL_FRAME_COUNT - 1,
          Math.max(0, Math.floor(sourceRatio * THUMBNAIL_FRAME_COUNT)),
        );
        return (
          <div
            key={`${index}-${frameIndex}`}
            className="h-full shrink-0 overflow-hidden border-r border-white/15"
            style={{ width: tileWidth }}
          >
            <img
              src={thumbnailUri}
              alt=""
              draggable={false}
              className="h-full max-w-none object-cover"
              style={{
                width: `${THUMBNAIL_FRAME_COUNT * 100}%`,
                transform: `translateX(-${
                  (frameIndex / THUMBNAIL_FRAME_COUNT) * 100
                }%)`,
              }}
            />
          </div>
        );
      })}
    </div>
  );
}

function WaveformCanvas({
  peaks,
  rangeStart,
  rangeEnd,
  widthPx,
  heightPx,
  bottomPx = 0,
  color = "rgba(255,255,255,0.85)",
}: {
  peaks: number[];
  rangeStart: number;
  rangeEnd: number;
  widthPx: number;
  heightPx: number;
  bottomPx?: number;
  color?: string;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const samples = useMemo(
    () =>
      resampleWaveformPeaks(
        peaks,
        rangeStart,
        rangeEnd,
        widthPx,
      ),
    [peaks, rangeStart, rangeEnd, widthPx],
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || widthPx <= 0 || heightPx <= 0 || samples.length === 0) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.round(widthPx * dpr));
    canvas.height = Math.max(1, Math.round(heightPx * dpr));
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, widthPx, heightPx);

    ctx.fillStyle = color;
    const barWidth = widthPx / samples.length;
    const midY = heightPx / 2;
    const displayGain = waveformDisplayGain(samples);
    samples.forEach((peak, index) => {
      const barHeight = Math.max(
        1,
        Math.min(1, Math.max(0, peak) * displayGain) * heightPx,
      );
      ctx.fillRect(
        index * barWidth,
        midY - barHeight / 2,
        Math.max(1, barWidth - Math.min(1, barWidth / 3)),
        barHeight,
      );
    });
  }, [samples, widthPx, heightPx, color]);

  return (
    <canvas
      ref={canvasRef}
      data-testid="audio-waveform"
      className="pointer-events-none absolute inset-x-0"
      style={{ width: widthPx, height: heightPx, bottom: bottomPx }}
    />
  );
}

export function TimelineClip({
  item,
  media,
  trackKind,
  trackLocked,
  trackMuted = false,
  editMode,
  left,
  width,
  pxPerFrame,
  selected,
  dragging,
  agentTouched,
  trimTargetFrame,
  fps,
  fadePreviewEdge,
  fadeInDisabledReason,
  fadeOutDisabledReason,
  onKeyDown,
  onContextMenu,
  onPointerDown,
  onTrimPointerDown,
  onFadePointerDown,
}: TimelineClipProps) {
  const clipWidthPx = Math.max(width, HANDLE_PX * 2);
  const visibleRange = visibleSourceRange(item, media);
  const hasWaveform = !!media?.waveformPeaks?.length;
  const hasFilmstrip = item.kind === "video" && !!media?.thumbnailUri;
  const hasEmbeddedAudio =
    item.kind === "video" &&
    (media?.probe?.hasAudio === true || hasWaveform);
  const waveformLoading =
    hasEmbeddedAudio &&
    (media?.processing?.waveform === "pending" ||
      media?.processing?.waveform === "running");
  const waveformMuted = !!item.muted || trackMuted;
  const hasImageThumb = item.kind === "image" && !!media?.thumbnailUri;
  const displayName = clipDisplayName(item, media);
  const clipTone =
    item.kind === "audio" && waveformMuted
      ? "border-emerald-950/35 bg-emerald-950/35 dark:border-emerald-900/30 dark:bg-black/40"
      : TRACK_COLOR[trackKind];
  const labelTone =
    item.kind === "video"
      ? "bg-sky-950/80 text-sky-50"
      : item.kind === "audio"
        ? waveformMuted
          ? "bg-emerald-950/60 text-emerald-100/60"
          : "bg-emerald-900/85 text-emerald-50"
        : "bg-violet-950/80 text-violet-50";
  const fadeInWidthPx = Math.min(
    clipWidthPx,
    Math.max(0, (item.fadeInFrames ?? 0) * pxPerFrame),
  );
  const fadeOutWidthPx = Math.min(
    clipWidthPx,
    Math.max(0, (item.fadeOutFrames ?? 0) * pxPerFrame),
  );
  const fadeHandleInset = Math.min(6, clipWidthPx / 2);
  const fadeInHandleX = Math.min(
    clipWidthPx - fadeHandleInset,
    Math.max(fadeHandleInset, fadeInWidthPx),
  );
  const fadeOutHandleX = Math.min(
    clipWidthPx - fadeHandleInset,
    Math.max(fadeHandleInset, clipWidthPx - fadeOutWidthPx),
  );
  const fadeTooltipX =
    fadePreviewEdge === "fadeInFrames" ? fadeInHandleX : fadeOutHandleX;
  const fadeTooltipLabel =
    fadePreviewEdge === "fadeInFrames" ? "淡入" : "淡出";

  return (
    <div
      data-testid={`timeline-clip-${item.itemId}`}
      data-timeline-item-id={item.itemId}
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      aria-label={`${displayName}，片段`}
      data-muted={
        item.kind === "audio" ? (waveformMuted ? "true" : "false") : undefined
      }
      className={`group absolute bottom-1 top-1 flex min-w-0 items-center overflow-visible rounded-[4px] border text-xs select-none ${clipTone} ${
        trackLocked
          ? "cursor-not-allowed"
          : editMode === "blade"
            ? "cursor-crosshair"
            : "cursor-grab"
      } ${
        dragging
          ? "z-10 ring-2 ring-orange-500"
          : selected
            ? "z-10 ring-2 ring-orange-500"
            : agentTouched
              ? "z-10 animate-pulse ring-2 ring-orange-500"
              : ""
      }`}
      style={{
        left,
        width: clipWidthPx,
      }}
      onKeyDown={onKeyDown}
      onContextMenu={onContextMenu}
      onPointerDown={onPointerDown}
    >
      <div className="pointer-events-none absolute inset-0 overflow-hidden rounded-[3px]">
        {hasFilmstrip && (
          <Filmstrip
            thumbnailUri={media.thumbnailUri!}
            range={visibleRange}
            clipWidthPx={clipWidthPx}
            bottomPx={hasEmbeddedAudio ? 48 : 20}
          />
        )}
        {hasImageThumb && (
          <img
            src={media.thumbnailUri}
            alt=""
            draggable={false}
            className="absolute inset-x-0 bottom-5 top-0 w-full object-cover"
          />
        )}
        {item.kind === "audio" && hasWaveform && (
          <WaveformCanvas
            peaks={media.waveformPeaks!}
            rangeStart={visibleRange[0]}
            rangeEnd={visibleRange[1]}
            widthPx={clipWidthPx}
            heightPx={40}
            bottomPx={20}
            color={
              waveformMuted
                ? "rgba(6,78,59,0.30)"
                : "rgba(6,95,70,0.88)"
            }
          />
        )}
        {hasEmbeddedAudio && (
          <div
            data-testid="video-embedded-waveform"
            data-muted={waveformMuted ? "true" : "false"}
            className={`absolute inset-x-0 bottom-0 h-7 overflow-hidden border-t border-teal-700/15 ${
              waveformMuted
                ? "bg-teal-100/35 dark:bg-teal-950/25"
                : "bg-teal-100/85 dark:bg-teal-950/70"
            }`}
          >
            {hasWaveform && (
              <WaveformCanvas
                peaks={media.waveformPeaks!}
                rangeStart={visibleRange[0]}
                rangeEnd={visibleRange[1]}
                widthPx={clipWidthPx}
                heightPx={28}
                color={
                  waveformMuted
                    ? "rgba(15,118,110,0.28)"
                    : "rgba(15,118,110,0.82)"
                }
              />
            )}
            {waveformLoading && !hasWaveform && (
              <span className="absolute inset-0 flex items-center px-2 text-[10px] text-teal-800/65 dark:text-teal-200/55">
                <span className="mr-1.5 size-1.5 animate-pulse rounded-full bg-current" />
                波形生成中…
              </span>
            )}
          </div>
        )}
        <div
          className={`absolute inset-x-0 flex h-5 items-center px-2 ${labelTone}`}
          style={{ bottom: hasEmbeddedAudio ? 28 : 0 }}
        >
          <span className="min-w-0 truncate text-[12px] font-medium">
            {displayName}
          </span>
        </div>
      </div>
      {trimTargetFrame !== null && (
        <span className="pointer-events-none absolute top-0 left-1/2 z-20 -translate-x-1/2 rounded bg-black/80 px-1 font-mono text-[10px] text-white">
          {trimTargetFrame}f
        </span>
      )}
      {fadeInWidthPx > 0 && (
        <svg
          data-testid={`fade-in-${item.itemId}`}
          className="pointer-events-none absolute left-0 top-0 z-10 h-full"
          style={{ width: fadeInWidthPx }}
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
        >
          <title>{`淡入 ${item.fadeInFrames} 帧`}</title>
          <path
            d="M 0 0 H 100 C 65 0 35 100 0 100 Z"
            fill="rgba(0,0,0,0.48)"
          />
          <path
            d="M 0 100 C 35 100 65 0 100 0"
            fill="none"
            stroke="rgba(255,255,255,0.9)"
            strokeWidth="1.5"
            vectorEffect="non-scaling-stroke"
          />
        </svg>
      )}
      {fadeOutWidthPx > 0 && (
        <svg
          data-testid={`fade-out-${item.itemId}`}
          className="pointer-events-none absolute right-0 top-0 z-10 h-full"
          style={{ width: fadeOutWidthPx }}
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
        >
          <title>{`淡出 ${item.fadeOutFrames} 帧`}</title>
          <path
            d="M 0 0 H 100 V 100 C 65 100 35 0 0 0 Z"
            fill="rgba(0,0,0,0.48)"
          />
          <path
            d="M 0 0 C 35 0 65 100 100 100"
            fill="none"
            stroke="rgba(255,255,255,0.9)"
            strokeWidth="1.5"
            vectorEffect="non-scaling-stroke"
          />
        </svg>
      )}
      {fadePreviewEdge && (
        <span
          data-testid={`fade-tooltip-${item.itemId}`}
          className="pointer-events-none absolute z-50 -translate-x-1/2 whitespace-nowrap rounded-md bg-[#171411] px-3 py-2 text-sm font-medium text-white shadow-lg"
          style={{
            left: fadeTooltipX,
            bottom: "calc(100% + 12px)",
          }}
        >
          {fadeTooltipLabel}: {((item[fadePreviewEdge] ?? 0) / fps).toFixed(1)}s
          <span className="absolute left-1/2 top-full -translate-x-1/2 border-x-4 border-t-4 border-x-transparent border-t-[#171411]" />
        </span>
      )}
      {item.muted && (item.kind === "video" || item.kind === "audio") && (
        <span
          className="pointer-events-none absolute right-2 top-1 z-20 rounded-[3px] bg-slate-950/75 p-0.5 text-white"
          title="该片段已静音"
        >
          <VolumeX className="size-3" />
        </span>
      )}
      <div
        className={`absolute inset-y-0 left-0 z-20 cursor-ew-resize bg-slate-950/45 transition-opacity hover:bg-slate-950/70 ${
          selected
            ? "opacity-100"
            : "opacity-0 group-hover:opacity-100"
        }`}
        style={{ width: HANDLE_PX }}
        title="拖动左侧手柄裁剪片段起点"
        data-testid={`trim-start-${item.itemId}`}
        onPointerDown={(event) => {
          if (editMode === "blade") return;
          onTrimPointerDown(event, "trimStart");
        }}
      />
      <div
        className={`absolute inset-y-0 right-0 z-20 cursor-ew-resize bg-slate-950/45 transition-opacity hover:bg-slate-950/70 ${
          selected
            ? "opacity-100"
            : "opacity-0 group-hover:opacity-100"
        }`}
        style={{ width: HANDLE_PX }}
        title="拖动右侧手柄裁剪片段终点"
        data-testid={`trim-end-${item.itemId}`}
        onPointerDown={(event) => {
          if (editMode === "blade") return;
          onTrimPointerDown(event, "trimEnd");
        }}
      />
      {(item.kind === "video" || item.kind === "audio") && editMode !== "blade" && (
        <>
          <button
            type="button"
            data-testid={`fade-handle-in-${item.itemId}`}
            aria-label="拖动调整淡入"
            title={fadeInDisabledReason ?? "拖动调整淡入（平滑曲线）"}
            disabled={trackLocked || !!fadeInDisabledReason}
            className={`absolute top-0 z-40 h-3 w-2 -translate-x-1/2 -translate-y-0.5 cursor-ew-resize rounded-b-sm rounded-t-[2px] border border-white/70 bg-[#171411] shadow-sm transition-opacity disabled:cursor-not-allowed disabled:opacity-40 ${
              fadePreviewEdge
                ? "opacity-100"
                : "opacity-0 group-hover:opacity-100 focus:opacity-100"
            }`}
            style={{ left: fadeInHandleX }}
            onPointerDown={(event) => onFadePointerDown(event, "fadeInFrames")}
          />
          <button
            type="button"
            data-testid={`fade-handle-out-${item.itemId}`}
            aria-label="拖动调整淡出"
            title={fadeOutDisabledReason ?? "拖动调整淡出（平滑曲线）"}
            disabled={trackLocked || !!fadeOutDisabledReason}
            className={`absolute top-0 z-40 h-3 w-2 -translate-x-1/2 -translate-y-0.5 cursor-ew-resize rounded-b-sm rounded-t-[2px] border border-white/70 bg-[#171411] shadow-sm transition-opacity disabled:cursor-not-allowed disabled:opacity-40 ${
              fadePreviewEdge
                ? "opacity-100"
                : "opacity-0 group-hover:opacity-100 focus:opacity-100"
            }`}
            style={{ left: fadeOutHandleX }}
            onPointerDown={(event) => onFadePointerDown(event, "fadeOutFrames")}
          />
        </>
      )}
    </div>
  );
}
