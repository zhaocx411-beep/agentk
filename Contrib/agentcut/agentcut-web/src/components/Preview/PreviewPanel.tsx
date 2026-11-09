// 右栏预览窗口（设计第 6 节的简化方案：<video>/<img> 直接叠加，不做
// WebCodecs+Canvas 逐帧合成）。用单一 JS 时钟（playbackStore.currentFrame）
// 驱动所有轨道的播放位置，而不是依赖某一路 <video> 自身的原生时钟——这样
// 多轨视频/图片才能对齐同一个时间线帧号。代价（已知、可接受，见设计文档）：
// 快速拖动进度条或多轨同时播放时可能有轻微卡顿/短暂黑帧，不追求像素级精确。
import { Pause, Play, Upload, X } from "lucide-react";
import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { Button } from "@/components/ui/button";
import {
  captionLayerStyle,
  croppedMediaStyle,
  effectivePreviewGain,
  fittedRotationStyle,
  itemVisualOpacity,
  resizeCrop,
  screenDeltaToSourceCropDelta,
  transitionHandleSourceFrame,
  transitionVisualOpacity,
  transitionPreviewAt,
  type CropEdge,
  type TransitionPreviewState,
} from "@/domain/m3bSemantics";
import {
  clampFrame,
  frameFromRatio,
  frameToSeconds,
  secondsToFrame,
} from "@/domain/timeline/time";
import type { Item, Media, TimelineDoc, Track } from "@/domain/timeline/types";
import {
  docDurationInFrames,
  effectiveSpeed,
  itemEnd,
} from "@/domain/timeline/types";
import { formatFrames } from "@/lib/media";
import { visualLayerStyle } from "@/domain/previewSemantics";
import { usePlaybackStore } from "@/store/playbackStore";
import { useProjectStore } from "@/store/projectStore";
import { usePreviewOverridesStore } from "@/store/previewOverridesStore";
import { submitOps } from "@/store/submitOps";
import { useTimelineStore } from "@/store/timelineStore";

import { PreviewAudioGraph } from "./previewAudioGraph";

/** 一帧内允许的 currentTime 漂移（秒）；超过才强制 seek，减少无意义的频繁 seek。 */
const DRIFT_TOLERANCE_SEC = 0.12;

function browserBlockedPlayback(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "name" in error &&
    error.name === "NotAllowedError"
  );
}

/** 当前帧下活跃的 item，按轨道数组顺序排（后面的轨道叠在上面）。 */
function activeItemsAt(
  doc: TimelineDoc,
  frame: number,
  transitionState: TransitionPreviewState | null,
): Item[] {
  const trackOrder = new Map(doc.tracks.map((t, i) => [t.trackId, i]));
  const trackById = new Map(doc.tracks.map((track) => [track.trackId, track]));
  const active = doc.items
    .filter((it) => {
      if (!(it.from <= frame && frame < itemEnd(it))) return false;
      const track = trackById.get(it.trackId);
      return !(
        it.kind === "text" &&
        track?.kind === "caption" &&
        track.muted
      );
    })
    .sort(
      (a, b) =>
        (trackOrder.get(a.trackId) ?? 0) - (trackOrder.get(b.trackId) ?? 0),
    );
  if (
    transitionState &&
    !active.some((item) => item.itemId === transitionState.from.itemId)
  ) {
    active.push(transitionState.from);
  }
  active.sort((a, b) => {
    const trackDelta =
      (trackOrder.get(a.trackId) ?? 0) - (trackOrder.get(b.trackId) ?? 0);
    if (trackDelta !== 0) return trackDelta;
    if (transitionState) {
      if (
        a.itemId === transitionState.from.itemId &&
        b.itemId === transitionState.to.itemId
      ) {
        return -1;
      }
      if (
        a.itemId === transitionState.to.itemId &&
        b.itemId === transitionState.from.itemId
      ) {
        return 1;
      }
    }
    return 0;
  });
  return active;
}

/** item 在源素材里对应的播放位置（秒），由时间线当前帧反推。 */
function sourceTimeSeconds(
  item: Item,
  currentFrame: number,
  fps: number,
  transitionState?: TransitionPreviewState | null,
): number {
  if (transitionState?.from.itemId === item.itemId) {
    return frameToSeconds(
      transitionHandleSourceFrame(item, currentFrame, transitionState.start),
      fps,
    );
  }
  const speed = effectiveSpeed(item);
  const framesIntoItem = currentFrame - item.from;
  const sourceFrame = (item.trimStart ?? 0) + framesIntoItem * speed;
  return frameToSeconds(sourceFrame, fps);
}

function TextLayer({ item, canvasHeight }: { item: Item; canvasHeight: number }) {
  return (
    <div className="pointer-events-none absolute inset-0">
      <span
        data-testid={`caption-layer-${item.itemId}`}
        style={captionLayerStyle(item, canvasHeight)}
      >
        {item.text}
      </span>
    </div>
  );
}

function MediaUnavailable({ detail }: { detail: string }) {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-black/70 px-4 text-center text-xs text-white">
      {detail}。请重新上传或替换素材后重试。
    </div>
  );
}

/**
 * 受管素材通过同源 API 流式读取。对象存储 CDN 可能允许 <video> 直接播放但
 * 不返回 CORS 头；这类媒体接入 MediaElementAudioSourceNode 后浏览器会把
 * 音频强制输出为全零。项目代理支持 Range，既保留 seek 又可安全接入 Web Audio。
 */
function previewMediaUri(projectId: string | undefined, media: Media): string {
  if (!projectId || !media.blobKey) return media.uri;
  const resource = media.kind === "audio" ? "playback" : "content";
  return `/v1/projects/${encodeURIComponent(projectId)}/media/${encodeURIComponent(media.mediaId)}/${resource}`;
}

function ImageLayer({
  media,
  fit,
  item,
}: {
  media: Media;
  fit: "contain" | "cover";
  item: Item;
}) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [media.uri]);
  if (failed) return <MediaUnavailable detail="图片素材无法加载" />;
  return (
    <img
      src={media.uri}
      alt=""
      draggable={false}
      className={fit === "cover" ? "object-cover" : "object-contain"}
      style={croppedMediaStyle(item.transform?.crop)}
      onError={() => setFailed(true)}
    />
  );
}

function VideoLayer({
  item,
  media,
  currentFrame,
  fps,
  isPlaying,
  track,
  graph,
  onApproximate,
  onPlaybackError,
  onPlaybackStarted,
  gain,
  transitionState,
}: {
  item: Item;
  media: Media;
  currentFrame: number;
  fps: number;
  isPlaying: boolean;
  track?: Track;
  graph: PreviewAudioGraph;
  onApproximate(): void;
  onPlaybackError(error: unknown): void;
  onPlaybackStarted(): void;
  gain: number;
  transitionState: TransitionPreviewState | null;
}) {
  const ref = useRef<HTMLVideoElement>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [media.uri]);
  useEffect(() => {
    const el = ref.current;
    return () => {
      if (el) graph.release(el);
    };
  }, [graph]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const target = sourceTimeSeconds(item, currentFrame, fps, transitionState);
    if (!isPlaying || Math.abs(el.currentTime - target) > DRIFT_TOLERANCE_SEC) {
      try {
        el.currentTime = target;
      } catch {
        // 元数据还没加载完时赋值会被忽略；loadedmetadata 后下一次 tick 自然重试。
      }
    }
    el.playbackRate = effectiveSpeed(item);
    const exact = graph.applyGain(el, gain);
    if (!exact) onApproximate();
  }, [
    item,
    track,
    currentFrame,
    fps,
    isPlaying,
    graph,
    onApproximate,
    gain,
    transitionState,
  ]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (isPlaying) {
      void el.play().then(onPlaybackStarted).catch(onPlaybackError);
    } else {
      el.pause();
    }
  }, [isPlaying, onPlaybackError, onPlaybackStarted]);

  if (failed) return <MediaUnavailable detail="视频素材无法播放" />;
  return (
    <video
      ref={ref}
      src={media.uri}
      playsInline
      className={item.transform?.fit === "cover" ? "object-cover" : "object-contain"}
      style={croppedMediaStyle(item.transform?.crop)}
      onError={() => setFailed(true)}
    />
  );
}

function AudioLayer({
  item,
  media,
  currentFrame,
  fps,
  isPlaying,
  track,
  graph,
  onApproximate,
  onPlaybackError,
  onPlaybackStarted,
  gain,
}: {
  item: Item;
  media: Media;
  currentFrame: number;
  fps: number;
  isPlaying: boolean;
  track?: Track;
  graph: PreviewAudioGraph;
  onApproximate(): void;
  onPlaybackError(error: unknown): void;
  onPlaybackStarted(): void;
  gain: number;
}) {
  const ref = useRef<HTMLAudioElement>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [media.uri]);
  useEffect(() => {
    const el = ref.current;
    return () => {
      if (el) graph.release(el);
    };
  }, [graph]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const target = sourceTimeSeconds(item, currentFrame, fps);
    if (!isPlaying || Math.abs(el.currentTime - target) > DRIFT_TOLERANCE_SEC) {
      try {
        el.currentTime = target;
      } catch {
        // 见 VideoLayer 同名分支。
      }
    }
    el.playbackRate = effectiveSpeed(item);
    const exact = graph.applyGain(el, gain);
    if (!exact) onApproximate();
  }, [item, track, currentFrame, fps, isPlaying, graph, onApproximate, gain]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (isPlaying) {
      void el.play().then(onPlaybackStarted).catch(onPlaybackError);
    } else {
      el.pause();
    }
  }, [isPlaying, onPlaybackError, onPlaybackStarted]);

  if (failed) {
    return (
      <div
        role="status"
        className="pointer-events-none absolute inset-x-3 bottom-12 z-30 rounded bg-destructive/90 px-3 py-2 text-center text-xs text-destructive-foreground"
      >
        当前音频片段无法预览；其他视频和音频仍会继续播放。
      </div>
    );
  }
  return <audio ref={ref} src={media.uri} onError={() => setFailed(true)} />;
}

interface PreviewLayerProps {
  doc: TimelineDoc;
  item: Item;
  media?: Media;
  currentFrame: number;
  fps: number;
  isPlaying: boolean;
  track?: Track;
  graph: PreviewAudioGraph;
  onApproximate(): void;
  onPlaybackError(error: unknown): void;
  onPlaybackStarted(): void;
  transitionState: TransitionPreviewState | null;
}

function PreviewLayer({
  doc,
  item,
  media,
  currentFrame,
  fps,
  isPlaying,
  track,
  graph,
  onApproximate,
  onPlaybackError,
  onPlaybackStarted,
  transitionState,
}: PreviewLayerProps) {
  const transitionOpacity = transitionVisualOpacity(transitionState, item.itemId);
  const layerStyle = visualLayerStyle(item, currentFrame);
  const visualHidden = track?.kind === "video" && !!track.hidden;
  const visualStyle = {
    ...layerStyle,
    opacity: visualHidden
      ? 0
      : itemVisualOpacity(doc, item, currentFrame) * transitionOpacity,
  };
  const gain = effectivePreviewGain(doc, track, item, currentFrame, transitionState);
  const visualTransform = item.transform ?? {
    x: 0,
    y: 0,
    width: 1,
    height: 1,
    opacity: 1,
    fit: "contain" as const,
  };
  const fittedStyle = media
    ? fittedRotationStyle(
        media.probe?.width ?? doc.metadata.width,
        media.probe?.height ?? doc.metadata.height,
        doc.metadata.width,
        doc.metadata.height,
        visualTransform,
      )
    : undefined;
  if (item.kind === "text") {
    return <TextLayer item={item} canvasHeight={doc.metadata.height} />;
  }
  if (!media) return <MediaUnavailable detail="片段引用的媒体已缺失" />;
  if (item.kind === "image") {
    return (
      <div
        data-testid={`preview-layer-${item.itemId}`}
        className="pointer-events-none absolute overflow-hidden"
        style={visualStyle}
      >
        <div style={fittedStyle}>
          <div className="absolute inset-0 overflow-hidden">
            <ImageLayer
              item={item}
              media={media}
              fit={item.transform?.fit ?? "contain"}
            />
          </div>
        </div>
      </div>
    );
  }
  if (item.kind === "video") {
    return (
      <div
        data-testid={`preview-layer-${item.itemId}`}
        className="pointer-events-none absolute overflow-hidden"
        style={visualStyle}
      >
        <div style={fittedStyle}>
          <div className="absolute inset-0 overflow-hidden">
            <VideoLayer
              item={item}
              media={media}
              currentFrame={currentFrame}
              fps={fps}
              isPlaying={isPlaying}
              track={track}
              graph={graph}
              gain={gain}
              transitionState={transitionState}
              onApproximate={onApproximate}
              onPlaybackError={onPlaybackError}
              onPlaybackStarted={onPlaybackStarted}
            />
          </div>
        </div>
      </div>
    );
  }
  // audio：不产生画面，只挂一个不可见元素维持声音同步。
  return (
    <AudioLayer
      item={item}
      media={media}
      currentFrame={currentFrame}
      fps={fps}
      isPlaying={isPlaying}
      track={track}
      graph={graph}
      gain={gain}
      onApproximate={onApproximate}
      onPlaybackError={onPlaybackError}
      onPlaybackStarted={onPlaybackStarted}
    />
  );
}

/** 素材源预览：直接用原生控件播放素材原片，与时间线预览互斥。 */
function SourcePreview({ media, onClose }: { media: Media; onClose: () => void }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [media.uri]);
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 bg-[#F2F2F0] p-4">
      <div className="flex w-full max-w-[640px] items-center justify-between">
        <span className="truncate text-xs text-muted-foreground" title={media.uri}>
          源预览：{media.blobKey?.split("/").pop() ?? media.mediaId}
        </span>
        <Button variant="outline" size="icon-sm" title="返回时间线预览" onClick={onClose}>
          <X data-slot="icon" />
        </Button>
      </div>
      <div className="relative w-full max-w-full overflow-hidden rounded-md border bg-black">
        {failed ? (
          <div className="relative h-32">
            <MediaUnavailable detail="源素材无法预览" />
          </div>
        ) : media.kind === "image" ? (
          <img
            src={media.uri}
            alt=""
            className="mx-auto max-h-[60vh] object-contain"
            onError={() => setFailed(true)}
          />
        ) : media.kind === "audio" ? (
          <div className="flex h-32 items-center justify-center px-6">
            {/* 原生控件自带播放/进度/音量，源预览不需要接 playbackStore 时钟 */}
            <audio
              src={media.uri}
              controls
              autoPlay
              className="w-full"
              onError={() => setFailed(true)}
            />
          </div>
        ) : (
          <video
            src={media.uri}
            controls
            autoPlay
            playsInline
            className="mx-auto max-h-[60vh] w-full object-contain"
            onError={() => setFailed(true)}
          />
        )}
      </div>
      <span className="text-xs text-muted-foreground">
        正在预览素材原片；拖素材到下方时间线轨道后，播放按钮预览的才是成片效果。
      </span>
    </div>
  );
}

const CROP_HANDLES: CropEdge[] = ["nw", "n", "ne", "e", "se", "s", "sw", "w"];

function CropOverlay({
  item,
  projectId,
  sourceWidth,
  sourceHeight,
  canvasWidth,
  canvasHeight,
}: {
  item: Item;
  projectId: string | undefined;
  sourceWidth: number;
  sourceHeight: number;
  canvasWidth: number;
  canvasHeight: number;
}) {
  const setItemPatch = usePreviewOverridesStore((state) => state.setItemPatch);
  const clearItemPatch = usePreviewOverridesStore((state) => state.clearItemPatch);
  const setCropEditingItem = usePreviewOverridesStore(
    (state) => state.setCropEditingItem,
  );
  const dragCleanupRef = useRef<(() => void) | null>(null);
  const crop = item.transform?.crop ?? { x: 0, y: 0, width: 1, height: 1 };

  useEffect(() => {
    const cancel = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      dragCleanupRef.current?.();
      clearItemPatch(item.itemId);
      setCropEditingItem(null);
      useTimelineStore.getState().setGestureActive(false);
    };
    window.addEventListener("keydown", cancel);
    return () => {
      window.removeEventListener("keydown", cancel);
      dragCleanupRef.current?.();
      clearItemPatch(item.itemId);
      useTimelineStore.getState().setGestureActive(false);
    };
  }, [clearItemPatch, item.itemId, setCropEditingItem]);

  const startDrag = (edge: CropEdge, event: React.PointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.stopPropagation();
    const cropOverlay =
      event.currentTarget.closest<HTMLElement>("[data-crop-overlay]");
    const bounds = cropOverlay?.parentElement?.getBoundingClientRect();
    if (!bounds || bounds.width <= 0 || bounds.height <= 0) return;
    const initialX = event.clientX;
    const initialY = event.clientY;
    const initialCrop = { ...crop };
    let latest = initialCrop;
    dragCleanupRef.current?.();
    useTimelineStore.getState().setGestureActive(true);
    const move = (moveEvent: PointerEvent) => {
      const delta = screenDeltaToSourceCropDelta(
        moveEvent.clientX - initialX,
        moveEvent.clientY - initialY,
        bounds.width,
        bounds.height,
        sourceWidth,
        sourceHeight,
        canvasWidth,
        canvasHeight,
        item.transform ?? {
          x: 0,
          y: 0,
          width: 1,
          height: 1,
          opacity: 1,
          fit: "contain",
        },
      );
      latest = resizeCrop(
        initialCrop,
        edge,
        delta.x,
        delta.y,
      );
      const patch = usePreviewOverridesStore.getState().itemPatches[item.itemId] ?? {};
      setItemPatch(item.itemId, {
        ...patch,
        transform: {
          ...(item.transform ?? {
            x: 0,
            y: 0,
            width: 1,
            height: 1,
            opacity: 1,
            fit: "contain",
          }),
          crop: latest,
        },
      });
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", finish);
      if (dragCleanupRef.current === cleanup) dragCleanupRef.current = null;
    };
    const finish = (finishEvent: PointerEvent) => {
      cleanup();
      useTimelineStore.getState().setGestureActive(false);
      if (finishEvent.type === "pointercancel") {
        clearItemPatch(item.itemId);
        return;
      }
      clearItemPatch(item.itemId);
      if (projectId) {
        const transform = {
          ...(item.transform ?? {
            x: 0,
            y: 0,
            width: 1,
            height: 1,
            opacity: 1,
            fit: "contain" as const,
          }),
          crop: latest,
        };
        void submitOps(projectId, [
          { op: "updateItem", itemId: item.itemId, patch: { transform } },
        ]);
      }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", finish);
    window.addEventListener("pointercancel", finish);
    dragCleanupRef.current = cleanup;
  };

  return (
    <div
      data-testid={`crop-overlay-${item.itemId}`}
      data-crop-overlay
      className="absolute border-2 border-primary bg-primary/5"
      style={fittedRotationStyle(
        sourceWidth,
        sourceHeight,
        canvasWidth,
        canvasHeight,
        item.transform ?? {
          x: 0,
          y: 0,
          width: 1,
          height: 1,
          opacity: 1,
          fit: "contain",
        },
      )}
      onPointerDown={(event) => startDrag("move", event)}
    >
      {CROP_HANDLES.map((edge) => (
        <div
          key={edge}
          data-testid={`crop-handle-${edge}`}
          className="absolute h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full border bg-background"
          style={{
            left: edge.includes("w") ? "0%" : edge.includes("e") ? "100%" : "50%",
            top: edge.includes("n") ? "0%" : edge.includes("s") ? "100%" : "50%",
          }}
          onPointerDown={(event) => startDrag(edge, event)}
        />
      ))}
    </div>
  );
}

export function PreviewPanel() {
  const projectId = useProjectStore((state) => state.project?.projectId);
  const localDoc = useTimelineStore((s) => s.localDoc);
  const currentFrame = usePlaybackStore((s) => s.currentFrame);
  const isPlaying = usePlaybackStore((s) => s.isPlaying);
  const setCurrentFrame = usePlaybackStore((s) => s.setCurrentFrame);
  const pause = usePlaybackStore((s) => s.pause);
  const togglePlay = usePlaybackStore((s) => s.togglePlay);
  const sourcePreviewMediaId = usePlaybackStore((s) => s.sourcePreviewMediaId);
  const setSourcePreview = usePlaybackStore((s) => s.setSourcePreview);
  const [approximateAudio, setApproximateAudio] = useState(false);
  const [playbackFailed, setPlaybackFailed] = useState(false);
  const itemPatches = usePreviewOverridesStore((state) => state.itemPatches);
  const trackPatches = usePreviewOverridesStore((state) => state.trackPatches);
  const cropEditingItemId = usePreviewOverridesStore(
    (state) => state.cropEditingItemId,
  );
  const setCropEditingItem = usePreviewOverridesStore(
    (state) => state.setCropEditingItem,
  );
  const audioGraph = useMemo(() => new PreviewAudioGraph(projectId), [projectId]);
  useEffect(() => {
    setApproximateAudio(false);
    setPlaybackFailed(false);
    return () => audioGraph.dispose();
  }, [audioGraph]);
  const markPlaybackError = useCallback((error: unknown) => {
    if (browserBlockedPlayback(error)) setPlaybackFailed(true);
  }, []);
  const markPlaybackStarted = useCallback(() => setPlaybackFailed(false), []);
  useEffect(() => {
    // 捕获阶段直接在真实用户手势里创建/resume AudioContext，早于 React 的
    // 播放按钮和全局 Space 快捷键处理，避免 Chrome 自动播放策略令音频图静音。
    const activateAudio = () => {
      void audioGraph.activate().then((exact) => {
        if (!exact) setApproximateAudio(true);
      });
    };
    const activateFromKeyboard = (event: KeyboardEvent) => {
      activateAudio();
      const blocked =
        event.target instanceof Element &&
        !!event.target.closest(
          "input, textarea, select, button, [contenteditable]:not([contenteditable='false']), [role='dialog'], [role='menu'], [data-timeline-shortcuts-disabled]",
        );
      if (
        event.key === " " &&
        !blocked &&
        !usePlaybackStore.getState().isPlaying
      ) {
        setPlaybackFailed(false);
        audioGraph.playAll(markPlaybackError, markPlaybackStarted);
      }
    };
    window.addEventListener("pointerdown", activateAudio, { capture: true });
    window.addEventListener("keydown", activateFromKeyboard, { capture: true });
    return () => {
      window.removeEventListener("pointerdown", activateAudio, { capture: true });
      window.removeEventListener("keydown", activateFromKeyboard, { capture: true });
    };
  }, [audioGraph, markPlaybackError, markPlaybackStarted]);
  const markApproximate = useCallback(() => setApproximateAudio(true), []);

  const fps = localDoc?.metadata.fps ?? 30;
  const width = localDoc?.metadata.width ?? 1920;
  const height = localDoc?.metadata.height ?? 1080;
  const duration = localDoc ? docDurationInFrames(localDoc) : 0;

  useEffect(() => {
    if (!sourcePreviewMediaId && currentFrame > duration) {
      setCurrentFrame(duration);
    }
  }, [currentFrame, duration, setCurrentFrame, sourcePreviewMediaId]);

  const mediaById = useMemo(() => {
    const map = new Map<string, Media>();
    for (const media of localDoc?.media ?? []) {
      map.set(media.mediaId, {
        ...media,
        uri: previewMediaUri(projectId, media),
      });
    }
    return map;
  }, [localDoc, projectId]);

  const previewDoc = useMemo<TimelineDoc | null>(() => {
    if (!localDoc) return null;
    return {
      ...localDoc,
      items: localDoc.items.map((item) => ({
        ...item,
        ...itemPatches[item.itemId],
      })),
      tracks: localDoc.tracks.map((track) => ({
        ...track,
        ...trackPatches[track.trackId],
      })),
    };
  }, [itemPatches, localDoc, trackPatches]);
  const transitionState = previewDoc
    ? transitionPreviewAt(previewDoc, currentFrame)
    : null;
  const activeItems = previewDoc
    ? activeItemsAt(previewDoc, currentFrame, transitionState)
    : [];
  const trackById = useMemo(
    () => new Map((previewDoc?.tracks ?? []).map((track) => [track.trackId, track])),
    [previewDoc],
  );

  // 播放时钟：rAF 按真实经过时间换算帧数推进 currentFrame，到片尾自动停。
  const rafRef = useRef<number | null>(null);
  const playStartTsRef = useRef<number | null>(null);
  const playStartFrameRef = useRef(0);
  useEffect(() => {
    if (!isPlaying) {
      playStartTsRef.current = null;
      return;
    }
    const tick = (ts: number) => {
      if (playStartTsRef.current === null) {
        playStartTsRef.current = ts;
        playStartFrameRef.current = usePlaybackStore.getState().currentFrame;
      }
      const elapsedSeconds = (ts - playStartTsRef.current) / 1000;
      const next =
        playStartFrameRef.current + secondsToFrame(elapsedSeconds, fps, "floor");
      if (next >= duration) {
        usePlaybackStore.getState().setCurrentFrame(duration);
        usePlaybackStore.getState().pause();
        return;
      }
      usePlaybackStore.getState().setCurrentFrame(next);
      rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    return () => {
      if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    };
  }, [isPlaying, fps, duration]);

  function handleTogglePlay() {
    if (!localDoc || duration <= 0) return;
    if (!isPlaying) {
      if (currentFrame >= duration) setCurrentFrame(0);
      setPlaybackFailed(false);
      audioGraph.playAll(markPlaybackError, markPlaybackStarted);
    }
    togglePlay();
  }

  function handleScrub(e: React.PointerEvent<HTMLDivElement>) {
    if (duration <= 0) return;
    const el = e.currentTarget;
    const seekAt = (clientX: number) => {
      const rect = el.getBoundingClientRect();
      const ratio = Math.min(
        1,
        Math.max(0, (clientX - rect.left) / rect.width),
      );
      setCurrentFrame(frameFromRatio(ratio, duration));
    };
    pause();
    seekAt(e.clientX);
    const onMove = (ev: PointerEvent) => seekAt(ev.clientX);
    const onEnd = (endEvent: PointerEvent) => {
      if (endEvent.type === "pointerup") seekAt(endEvent.clientX);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onEnd);
      window.removeEventListener("pointercancel", onEnd);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onEnd, { once: true });
    window.addEventListener("pointercancel", onEnd, { once: true });
  }

  const aspect = width && height ? width / height : 16 / 9;
  const displayFrame = currentFrame;
  const timelineEmpty = !!previewDoc && previewDoc.items.length === 0;

  // 素材库点击触发的源预览模式：直接播原片（素材可能已被删除，现查兜底）。
  const sourceMedia = sourcePreviewMediaId ? mediaById.get(sourcePreviewMediaId) : undefined;
  if (sourceMedia) {
    return <SourcePreview media={sourceMedia} onClose={() => setSourcePreview(null)} />;
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 bg-[#F2F2F0] p-4">
      <div
        className={`relative max-h-full w-full max-w-full overflow-hidden ${
          timelineEmpty
            ? "bg-[#F2F2F0]"
            : "rounded-md border border-[#D8D6D2] bg-black"
        }`}
        style={{
          aspectRatio: aspect,
          backgroundColor: timelineEmpty
            ? "#F2F2F0"
            : localDoc?.metadata.backgroundColor || "black",
          containerType: "size",
        }}
      >
        {activeItems.length === 0 ? (
          timelineEmpty ? (
            <div
              data-testid="preview-empty-state"
              className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-[#9CA3AF]"
            >
              <Upload className="size-8" aria-hidden="true" />
              <span className="text-xs">将素材添加到时间线后预览</span>
            </div>
          ) : (
            <div className="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">
              {localDoc ? "当前位置没有内容" : "加载中…"}
            </div>
          )
        ) : (
          activeItems.map((it) => {
            const item = it;
            const track = trackById.get(it.trackId);
            return (
              <Fragment key={`${projectId ?? "no-project"}:${it.itemId}`}>
                {transitionState?.to.itemId === item.itemId &&
                  !track?.hidden &&
                  transitionState.blackOpacity > 0 && (
                    <div
                      data-testid="transition-black-layer"
                      className="pointer-events-none absolute bg-black"
                      style={{
                        ...visualLayerStyle(transitionState.from, currentFrame),
                        opacity: transitionState.blackOpacity,
                      }}
                    />
                  )}
                <PreviewLayer
                  doc={previewDoc!}
                  item={item}
                  media={it.mediaId ? mediaById.get(it.mediaId) : undefined}
                  currentFrame={currentFrame}
                  fps={fps}
                  isPlaying={isPlaying}
                  track={track}
                  graph={audioGraph}
                  transitionState={transitionState}
                  onApproximate={markApproximate}
                  onPlaybackError={markPlaybackError}
                  onPlaybackStarted={markPlaybackStarted}
                />
              </Fragment>
            );
          })
        )}
        {cropEditingItemId &&
          previewDoc &&
          (() => {
            const cropItem = previewDoc.items.find(
              (item) => item.itemId === cropEditingItemId,
            );
            if (!cropItem || (cropItem.kind !== "video" && cropItem.kind !== "image")) {
              return null;
            }
            const cropMedia = cropItem.mediaId
              ? mediaById.get(cropItem.mediaId)
              : undefined;
            return (
              <div
                className="absolute"
                style={{
                  left: `${(cropItem.transform?.x ?? 0) * 100}%`,
                  top: `${(cropItem.transform?.y ?? 0) * 100}%`,
                  width: `${(cropItem.transform?.width ?? 1) * 100}%`,
                  height: `${(cropItem.transform?.height ?? 1) * 100}%`,
                }}
              >
                <CropOverlay
                  item={cropItem}
                  projectId={projectId}
                  sourceWidth={cropMedia?.probe?.width ?? width}
                  sourceHeight={cropMedia?.probe?.height ?? height}
                  canvasWidth={width}
                  canvasHeight={height}
                />
              </div>
            );
          })()}
        {cropEditingItemId && (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            className="absolute right-2 top-2 z-50"
            onClick={() => {
              usePreviewOverridesStore
                .getState()
                .clearItemPatch(cropEditingItemId);
              useTimelineStore.getState().setGestureActive(false);
              setCropEditingItem(null);
            }}
          >
            <X />
            退出裁剪
          </Button>
        )}
      </div>
      {approximateAudio && (
        <p role="status" className="text-xs text-amber-600">
          当前浏览器不支持增益超过 1 的 Web Audio 预览，已限制音量；预览近似。
        </p>
      )}
      {playbackFailed && (
        <p role="alert" className="text-xs text-destructive">
          浏览器阻止了时间线媒体播放。请再次点击播放；若仍无声音，请检查站点声音权限。
        </p>
      )}
      {!timelineEmpty && (
      <div className="flex w-full max-w-[640px] items-center gap-2">
        <Button
          variant="outline"
          size="icon-sm"
          disabled={!localDoc || duration <= 0}
          onClick={handleTogglePlay}
          title={isPlaying ? "暂停" : "播放"}
          aria-label={isPlaying ? "暂停时间线" : "播放时间线"}
        >
          {isPlaying ? <Pause data-slot="icon" /> : <Play data-slot="icon" />}
        </Button>
        <div
          className="relative h-1.5 flex-1 cursor-pointer rounded-full bg-border"
          onPointerDown={handleScrub}
          role="slider"
          tabIndex={0}
          aria-label="时间线播放位置"
          aria-valuemin={0}
          aria-valuemax={duration}
          aria-valuenow={currentFrame}
          onKeyDown={(event) => {
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
            event.preventDefault();
            pause();
            setCurrentFrame(
              clampFrame(
                currentFrame + (event.key === "ArrowLeft" ? -1 : 1),
                duration,
              ),
            );
          }}
        >
          <div
            className="h-full rounded-full bg-primary"
            style={{
              width:
                duration > 0
                  ? `${Math.min(100, (currentFrame / duration) * 100)}%`
                  : "0%",
            }}
          />
        </div>
        <span className="text-xs tabular-nums text-muted-foreground">
          {formatFrames(displayFrame, fps)} / {formatFrames(duration, fps)}
        </span>
      </div>
      )}
    </div>
  );
}
