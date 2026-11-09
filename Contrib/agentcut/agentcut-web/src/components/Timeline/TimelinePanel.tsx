import {
  Captions,
  ClipboardPaste,
  Copy,
  Crop,
  Eye,
  EyeOff,
  Ellipsis,
  Lock,
  LockOpen,
  LoaderCircle,
  Magnet,
  Maximize2,
  MousePointer2,
  Pencil,
  Plus,
  Scissors,
  Search,
  Slice,
  Trash2,
  Unlink,
  Volume2,
  VolumeX,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { isMediaReadyForTimeline } from "@/domain/mediaProcessing";
import {
  autoCaptionMediaIdForTrack,
  captionProjectionsForVideoTrack,
  captionTracksForVideoTrack,
  isAutoCaptionTrackId,
  mainTimelineTracks,
} from "@/domain/captionTracks";
import {
  sidechainIntervals,
  transitionBoundaries,
  transitionDisableReason,
  transitionFramesForDrop,
} from "@/domain/m3bSemantics";
import {
  buildEditOperation,
  buildPasteOperations,
  buildSelectionDeleteOperation,
  clampFadeFrames,
  clampEditDelta,
  createTimelineClipboard,
  duplicateTargetFrame,
  exceedsPointerThreshold,
  fadeDisabledReason,
  normalizeSelectionRect,
  selectionFromMarquee,
  type FadeEdge,
  type SelectionRect,
  type TimelineClipboard,
  type TimelineDragMode,
  type TimelineDragPreview,
  updateItemSelection,
} from "@/domain/timeline/editing";
import {
  buildInsertMediaOps,
  newItemId,
  type InsertMediaPlan,
} from "@/domain/timeline/insertMedia";
import { buildTranscriptCaptionOps } from "@/domain/transcriptCaptions";
import { compatibleTargetTrack, snapFrame, snapTargets } from "@/domain/timeline/snap";
import {
  clampFrame,
  normalizeFrame,
  safeFps,
} from "@/domain/timeline/time";
import type {
  Item,
  ItemKind,
  MediaKind,
  Operation,
  Track,
  TrackKind,
  TimelineDoc,
  Transition,
} from "@/domain/timeline/types";
import {
  DEFAULT_FPS,
  docDurationInFrames,
  effectiveSpeed,
  itemEnd,
  itemKindAllowedOn,
} from "@/domain/timeline/types";
import {
  MEDIA_DND_TYPE,
  TRANSITION_DND_KIND_TYPE,
  TRANSITION_DND_TYPE,
  type TransitionDragPayload,
} from "@/lib/dnd";
import {
  snapshotEditorContext,
  useEditorContextStore,
  type EditorContextSnapshot,
} from "@/store/editorContextStore";
import { usePlaybackStore } from "@/store/playbackStore";
import { usePreviewOverridesStore } from "@/store/previewOverridesStore";
import { useProjectStore } from "@/store/projectStore";
import {
  scheduleSequenceViewSave,
  useSequenceStore,
} from "@/store/sequenceStore";
import {
  createSequence,
  deleteSequence,
  renameSequence,
  switchSequence,
} from "@/store/sequenceActions";
import { submitOps } from "@/store/submitOps";
import { separateAudioItem, type SeparateAudioPhase } from "@/store/separateAudio";
import { useTimelineStore } from "@/store/timelineStore";
import { toast } from "@/store/toastStore";

import { CaptionClip } from "./CaptionClip";
import { CaptionPanel } from "./CaptionPanel";
import { TimelineClip } from "./TimelineClip";
import type { TimelineEditMode } from "./TimelineClip";

const TRACK_HEADER_PX = 208;
const RULER_HEIGHT_PX = 32;
const MIN_ZOOM = 0.25;
const MAX_ZOOM = 4;
const ZOOM_STEP = 0.25;
const POINTER_DRAG_THRESHOLD_PX = 4;
const FIT_ALL_HORIZONTAL_MARGIN_PX = 40;

interface MarqueePreview {
  left: number;
  top: number;
  width: number;
  height: number;
}

interface FadePreview {
  itemId: string;
  edge: FadeEdge;
  frames: number;
}

function TransitionDropTarget({
  doc,
  track,
  from,
  to,
  existing,
  pxPerFrame,
  dragKind,
  onDrop,
  onBlocked,
  onRemove,
}: {
  doc: TimelineDoc;
  track: Track;
  from: Item;
  to: Item;
  existing?: Transition;
  pxPerFrame: number;
  dragKind: TransitionDragPayload["kind"] | null;
  onDrop(event: React.DragEvent<HTMLDivElement>, from: Item, to: Item, existing?: Transition): void;
  onBlocked(reason: string): void;
  onRemove(transition: Transition): void;
}) {
  const preferredFrames = Math.max(1, Math.min(doc.metadata.fps, to.durationInFrames));
  const frames = existing?.frames ?? transitionFramesForDrop(doc, from, to);
  const reason =
    dragKind === "none"
      ? track.locked
        ? "轨道已锁定"
        : existing
          ? null
          : "该边界没有可移除的转场"
      : track.locked
        ? "轨道已锁定"
        : transitionDisableReason(doc, from, to, frames);
  const active = dragKind !== null;
  const target = (
    <div
      data-testid={`transition-drop-target-${from.itemId}-${to.itemId}`}
      data-drop-state={!active ? "idle" : reason ? "disabled" : "enabled"}
      data-transition-frames={frames}
      className={`absolute inset-y-0 z-30 -translate-x-1/2 ${
        active ? "w-6" : "w-2"
      } ${reason && active ? "bg-destructive/25" : active ? "bg-emerald-400/25" : ""}`}
      style={{ left: to.from * pxPerFrame }}
      role="button"
      tabIndex={existing ? 0 : -1}
      aria-label={`片段 ${from.itemId} 到 ${to.itemId} 的转场边界`}
      title={
        active
          ? reason ??
            `${existing ? "替换" : "添加"}转场（${frames} 帧${
              !existing && frames < preferredFrames ? "，已按素材余量缩短" : ""
            }）`
          : existing
            ? `${existing.kind} ${existing.frames} 帧；右键可删除`
            : "从 Library 的转场分类拖入"
      }
      onPointerDown={(event) => event.stopPropagation()}
      onDragOver={(event) => {
        if (!active) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = reason ? "none" : "copy";
      }}
      onDrop={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (reason) {
          onBlocked(reason);
          return;
        }
        onDrop(event, from, to, existing);
      }}
    >
      <span
        className={`pointer-events-none absolute inset-y-1 left-1/2 w-px ${
          active
            ? reason
              ? "bg-destructive"
              : "bg-emerald-400"
            : existing
              ? "bg-violet-400"
              : "bg-border"
        }`}
      />
    </div>
  );
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{target}</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem
          variant="destructive"
          disabled={!existing || !!track.locked}
          onSelect={() => existing && onRemove(existing)}
        >
          <Trash2 />
          删除转场
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

const TRACK_ID_PREFIX: Record<TrackKind, string> = { video: "v", audio: "a", caption: "c" };
const TRACK_KIND_LABEL: Record<TrackKind, string> = {
  video: "视频",
  audio: "音频",
  caption: "字幕",
};

interface CaptionEditorState {
  itemId?: string;
  newCaptionTrackId?: string;
}

function agentAdjustmentPrompt(context: EditorContextSnapshot): string {
  return [
    "请调整选中时间线项目的精确参数（速度、音量、源裁剪时间、时间线起点/时长、位置、缩放、旋转或画面适配）；如涉及所在轨道，也可调整轨道音量、duck/sidechain 或 ripple 参数。",
    `上下文快照：selectedItemIds=${JSON.stringify(context.selectedItemIds)}；playheadFrame=${context.playheadFrame}。`,
    "请先核对目标项目，再执行修改。",
  ].join(" ");
}

function trackAgentAdjustmentPrompt(
  context: EditorContextSnapshot,
  track: Track,
): string {
  return [
    `请调整轨道 ${track.trackId}（${track.name ?? TRACK_KIND_LABEL[track.kind] + "轨"}）的精确参数。`,
    "可按需重命名、调整轨道音量、配置或移除 duck/sidechain；不要用 muted 代替视频可见性。",
    `当前值：kind=${track.kind}；muted=${!!track.muted}；hidden=${!!track.hidden}；volume=${track.volume ?? 1}；ducking=${JSON.stringify(track.ducking ?? null)}。`,
    `上下文快照：selectedItemIds=${JSON.stringify(context.selectedItemIds)}；playheadFrame=${context.playheadFrame}。`,
    "请先核对目标轨道，再执行修改。",
  ].join(" ");
}

function transitionDragKind(
  types: readonly string[],
): TransitionDragPayload["kind"] | null {
  for (const [kind, marker] of Object.entries(TRANSITION_DND_KIND_TYPE)) {
    if (types.includes(marker)) return kind as TransitionDragPayload["kind"];
  }
  return types.includes(TRANSITION_DND_TYPE) ? "crossfade" : null;
}

function newTrackId(tracks: Track[], kind: TrackKind): string {
  const used = new Set(tracks.map((track) => track.trackId));
  const prefix = TRACK_ID_PREFIX[kind];
  for (let n = 1; ; n += 1) {
    if (!used.has(`${prefix}${n}`)) return `${prefix}${n}`;
  }
}

function trackDisplayCode(track: Track, tracks: Track[]): string {
  const prefix = track.kind === "video" ? "V" : track.kind === "audio" ? "A" : "C";
  const sameKind = tracks.filter((candidate) => candidate.kind === track.kind);
  return `${prefix}${sameKind.findIndex((candidate) => candidate.trackId === track.trackId) + 1}`;
}

function trackVisualMetrics(doc: TimelineDoc, track: Track): {
  height: number;
  layout: string;
} {
  const trackItems = doc.items.filter((item) => item.trackId === track.trackId);
  const hasContent = trackItems.length > 0;
  const hasVideoFilmstrip =
    track.kind === "video" &&
    trackItems.some((item) => {
      const media = doc.media.find((candidate) => candidate.mediaId === item.mediaId);
      return item.kind === "video" && !!media?.thumbnailUri;
    });
  const hasEmbeddedVideoAudio =
    track.kind === "video" &&
    trackItems.some((item) => {
      const media = doc.media.find((candidate) => candidate.mediaId === item.mediaId);
      return (
        item.kind === "video" &&
        (media?.probe?.hasAudio === true || !!media?.waveformPeaks?.length)
      );
    });
  const hasAudioWaveform =
    track.kind === "audio" &&
    trackItems.some((item) => {
      const media = doc.media.find((candidate) => candidate.mediaId === item.mediaId);
      return !!media?.waveformPeaks?.length;
    });
  if (track.kind === "caption") {
    return { height: 36, layout: hasContent ? "caption" : "empty-caption" };
  }
  if (!hasContent) return { height: 44, layout: "empty" };
  if (hasVideoFilmstrip && hasEmbeddedVideoAudio) {
    return { height: 94, layout: "video-with-embedded-audio" };
  }
  if (hasVideoFilmstrip) return { height: 80, layout: "video-filmstrip" };
  if (hasEmbeddedVideoAudio) return { height: 72, layout: "video-with-embedded-audio" };
  if (hasAudioWaveform) return { height: 72, layout: "audio-waveform" };
  return { height: 52, layout: track.kind };
}

export interface TrackVerticalLayout {
  contentHeight: number;
  dividerY: number;
  initialScrollTop: number;
  overflow: boolean;
  topOffset: number;
}

export function calculateTrackVerticalLayout(
  viewportHeight: number,
  videoHeight: number,
  audioHeight: number,
): TrackVerticalLayout {
  const viewport = Math.max(0, viewportHeight);
  const videos = Math.max(0, videoHeight);
  const audios = Math.max(0, audioHeight);
  const trackHeight = videos + audios;
  const overflow = viewport > 0 && trackHeight > viewport;

  if (overflow) {
    const maxScrollTop = trackHeight - viewport;
    return {
      contentHeight: trackHeight,
      dividerY: videos,
      initialScrollTop: Math.min(
        maxScrollTop,
        Math.max(0, videos - viewport / 2),
      ),
      overflow: true,
      topOffset: 0,
    };
  }

  const centeredDivider = viewport / 2;
  const dividerY = Math.min(
    Math.max(centeredDivider, videos),
    Math.max(videos, viewport - audios),
  );
  return {
    contentHeight: viewport,
    dividerY,
    initialScrollTop: 0,
    overflow: false,
    topOffset: Math.max(0, dividerY - videos),
  };
}

function rulerStepFrames(fps: number, pxPerFrame: number): number {
  const safeFPS = safeFps(fps);
  const seconds = [1, 2, 5, 10, 15, 30, 60, 120].find(
    (candidate) => candidate * safeFPS * pxPerFrame >= 90,
  );
  return safeFPS * (seconds ?? 120);
}

function formatTimelineClock(frame: number, fps: number): string {
  const totalSeconds = Math.floor(normalizeFrame(frame) / safeFps(fps));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor(totalSeconds / 60) % 60;
  const seconds = totalSeconds % 60;
  return [hours, minutes, seconds]
    .map((value) => String(value).padStart(2, "0"))
    .join(":");
}

function timelineShortcutsBlocked(target: EventTarget | null): boolean {
  const blocks = (candidate: EventTarget | null) =>
    candidate instanceof Element &&
    !!candidate.closest(
      "input, textarea, select, button, [contenteditable]:not([contenteditable='false']), [role='dialog'], [role='menu'], [data-agent-chat], [data-timeline-shortcuts-disabled]",
    );
  const selection = document.getSelection();
  const selectionInsideAgentChat =
    !!selection &&
    !selection.isCollapsed &&
    [selection.anchorNode, selection.focusNode].some((node) => {
      const element = node instanceof Element ? node : node?.parentElement;
      return !!element?.closest("[data-agent-chat]");
    });
  return blocks(target) || blocks(document.activeElement) || selectionInsideAgentChat;
}

export function TimelinePanel() {
  const localDoc = useTimelineStore((s) => s.localDoc);
  const project = useProjectStore((s) => s.project);
  const pxPerFrame = useSequenceStore((s) => s.zoom);
  const activeTimelineId = useSequenceStore((s) => s.activeTimelineId);
  const timelines = useSequenceStore((s) => s.timelines);
  const setSequenceZoom = useSequenceStore((s) => s.setZoom);
  const currentFrame = usePlaybackStore((s) => s.currentFrame);
  const isPlaying = usePlaybackStore((s) => s.isPlaying);
  const setCurrentFrame = usePlaybackStore((s) => s.setCurrentFrame);
  const pausePlayback = usePlaybackStore((s) => s.pause);
  const togglePlayback = usePlaybackStore((s) => s.togglePlay);
  const setGestureActive = useTimelineStore((s) => s.setGestureActive);
  const agentHighlightItemIds = useTimelineStore((s) => s.agentHighlightItemIds);
  const confirmedRevision = useTimelineStore((s) => s.confirmedRevision);
  const selectedItemIds = useEditorContextStore((s) => s.selectedItemIds);
  const setSelectedItemIds = useEditorContextStore((s) => s.setSelectedItemIds);
  const setTimelineRevision = useEditorContextStore((s) => s.setTimelineRevision);
  const setPlayheadFrame = useEditorContextStore((s) => s.setPlayheadFrame);
  const setSelectedMediaId = useEditorContextStore((s) => s.setSelectedMediaId);
  const setSelectedCaptionContext = useEditorContextStore(
    (s) => s.setSelectedCaptionContext,
  );
  const requestMediaReveal = useEditorContextStore((s) => s.requestMediaReveal);
  const setItemPreviewPatch = usePreviewOverridesStore((state) => state.setItemPatch);
  const clearItemPreviewPatch = usePreviewOverridesStore((state) => state.clearItemPatch);
  const setCropEditingItem = usePreviewOverridesStore(
    (state) => state.setCropEditingItem,
  );
  const requestAgentPrompt = useEditorContextStore(
    (state) => state.requestAgentPrompt,
  );

  const [editMode, setEditMode] = useState<TimelineEditMode>("select");
  const [drag, setDrag] = useState<TimelineDragPreview | null>(null);
  const [marquee, setMarquee] = useState<MarqueePreview | null>(null);
  const [fadePreview, setFadePreview] = useState<FadePreview | null>(null);
  const [clipboard, setClipboard] = useState<TimelineClipboard | null>(null);
  const [captionEditor, setCaptionEditor] =
    useState<CaptionEditorState | null>(null);
  const [opError, setOpError] = useState<string | null>(null);
  const [snapEnabled, setSnapEnabled] = useState(true);
  const [sequenceBusy, setSequenceBusy] = useState(false);
  const [renamingTimelineId, setRenamingTimelineId] = useState<string | null>(null);
  const [timelineNameDraft, setTimelineNameDraft] = useState("");
  const [mediaDropPreview, setMediaDropPreview] = useState<{
    hoverTrackId: string;
    plan: InsertMediaPlan;
  } | null>(null);
  const setPxPerFrame = useCallback(
    (next: number | ((current: number) => number)) => {
      setSequenceZoom(typeof next === "function" ? next(useSequenceStore.getState().zoom) : next);
    },
    [setSequenceZoom],
  );
  const [captionMediaId, setCaptionMediaId] = useState("");
  const [transitionDrag, setTransitionDrag] = useState<
    TransitionDragPayload["kind"] | null
  >(null);
  const [audioSeparation, setAudioSeparation] = useState<
    Record<string, { phase: SeparateAudioPhase; message?: string }>
  >({});
  const [laneViewportHeight, setLaneViewportHeight] = useState(0);
  const errorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const dragCleanupRef = useRef<(() => void) | null>(null);
  const transitionIdsRef = useRef<Set<string>>(new Set());
  const lanesRef = useRef<HTMLDivElement>(null);
  const timelineScrollRef = useRef<HTMLDivElement>(null);
  const timelineScrollTopsRef = useRef<Map<string, number>>(new Map());
  const lastScrollLayoutRef = useRef<{
    overflow: boolean;
    timelineId: string | null;
  }>({ overflow: false, timelineId: null });

  const items = localDoc?.items ?? [];
  const tracks = localDoc?.tracks ?? [];
  const displayTracks = useMemo(
    () => (localDoc ? mainTimelineTracks(localDoc) : []),
    [localDoc],
  );
  const baseVideoTrackId =
    tracks.find((track) => track.kind === "video")?.trackId ?? null;
  const videoStackHeight = useMemo(
    () =>
      localDoc
        ? displayTracks
            .filter((track) => track.kind !== "audio")
            .reduce(
              (height, track) => height + trackVisualMetrics(localDoc, track).height,
              0,
            )
        : 0,
    [displayTracks, localDoc],
  );
  const audioStackHeight = useMemo(
    () =>
      localDoc
        ? displayTracks
            .filter((track) => track.kind === "audio")
            .reduce(
              (height, track) => height + trackVisualMetrics(localDoc, track).height,
              0,
            )
        : 0,
    [displayTracks, localDoc],
  );
  const trackVerticalLayout = useMemo(
    () =>
      calculateTrackVerticalLayout(
        laneViewportHeight,
        videoStackHeight,
        audioStackHeight,
      ),
    [audioStackHeight, laneViewportHeight, videoStackHeight],
  );
  const captionTracks = tracks.filter((track) => track.kind === "caption");
  const selectedItemId = selectedItemIds.at(-1) ?? null;
  const selectedItem = items.find((item) => item.itemId === selectedItemId) ?? null;
  const captionEditorItem =
    items.find((item) => item.itemId === captionEditor?.itemId) ?? null;
  const fps = localDoc?.metadata.fps ?? DEFAULT_FPS;
  const duration = localDoc ? docDurationInFrames(localDoc) : 0;
  const timelineVideoMedia = useMemo(() => {
    if (!localDoc) return [];
    const ids = new Set(
      localDoc.items
        .filter((item) => item.kind === "video" && item.mediaId)
        .map((item) => item.mediaId!),
    );
    return localDoc.media.filter(
      (media) => media.kind === "video" && ids.has(media.mediaId),
    );
  }, [localDoc]);

  useEffect(() => {
    setTimelineRevision(confirmedRevision);
  }, [confirmedRevision, setTimelineRevision]);

  useEffect(() => {
    setPlayheadFrame(currentFrame);
  }, [currentFrame, setPlayheadFrame]);

  useEffect(() => {
    if (!project || !activeTimelineId) return;
    scheduleSequenceViewSave(project.projectId, activeTimelineId, currentFrame, pxPerFrame);
  }, [activeTimelineId, currentFrame, project, pxPerFrame]);

  useLayoutEffect(() => {
    const scroller = timelineScrollRef.current;
    if (!scroller) return;
    const updateHeight = () => {
      const next = Math.max(0, scroller.clientHeight - RULER_HEIGHT_PX);
      setLaneViewportHeight((current) => (current === next ? current : next));
    };
    updateHeight();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", updateHeight);
      return () => window.removeEventListener("resize", updateHeight);
    }
    const observer = new ResizeObserver(updateHeight);
    observer.observe(scroller);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const scroller = timelineScrollRef.current;
    if (!scroller || laneViewportHeight <= 0) return;
    const timelineId = activeTimelineId ?? null;
    const previous = lastScrollLayoutRef.current;
    const timelineChanged = previous.timelineId !== timelineId;
    const overflowChanged = previous.overflow !== trackVerticalLayout.overflow;
    const maxScrollTop = Math.max(
      0,
      trackVerticalLayout.contentHeight - laneViewportHeight,
    );

    if (!trackVerticalLayout.overflow) {
      scroller.scrollTop = 0;
    } else if (timelineChanged) {
      const saved = timelineId
        ? timelineScrollTopsRef.current.get(timelineId)
        : undefined;
      scroller.scrollTop = Math.min(
        maxScrollTop,
        Math.max(0, saved ?? trackVerticalLayout.initialScrollTop),
      );
    } else if (overflowChanged) {
      scroller.scrollTop = trackVerticalLayout.initialScrollTop;
    } else if (scroller.scrollTop > maxScrollTop) {
      scroller.scrollTop = maxScrollTop;
    }

    lastScrollLayoutRef.current = {
      overflow: trackVerticalLayout.overflow,
      timelineId,
    };
  }, [
    activeTimelineId,
    laneViewportHeight,
    trackVerticalLayout.contentHeight,
    trackVerticalLayout.initialScrollTop,
    trackVerticalLayout.overflow,
  ]);

  const handleTimelineScroll = useCallback(
    (event: React.UIEvent<HTMLDivElement>) => {
      if (!activeTimelineId || !trackVerticalLayout.overflow) return;
      timelineScrollTopsRef.current.set(
        activeTimelineId,
        event.currentTarget.scrollTop,
      );
    },
    [activeTimelineId, trackVerticalLayout.overflow],
  );

  useEffect(() => {
    setSelectedMediaId(selectedItem?.mediaId);
    if (selectedItem?.kind !== "text" || !localDoc) {
      setSelectedCaptionContext(undefined);
      return;
    }
    const mediaId = autoCaptionMediaIdForTrack(localDoc, selectedItem.trackId);
    const videoTrackId = displayTracks.find(
      (track) =>
        track.kind === "video" &&
        captionProjectionsForVideoTrack(localDoc, track.trackId).some(
          (projection) => projection.item.itemId === selectedItem.itemId,
        ),
    )?.trackId;
    setSelectedCaptionContext({
      itemId: selectedItem.itemId,
      trackId: selectedItem.trackId,
      mediaId,
      videoTrackId,
    });
  }, [
    displayTracks,
    localDoc,
    selectedItem,
    setSelectedCaptionContext,
    setSelectedMediaId,
  ]);

  useEffect(() => {
    if (timelineVideoMedia.some((media) => media.mediaId === captionMediaId)) {
      return;
    }
    setCaptionMediaId(timelineVideoMedia[0]?.mediaId ?? "");
  }, [captionMediaId, timelineVideoMedia]);

  useEffect(
    () => () => {
      if (errorTimerRef.current) clearTimeout(errorTimerRef.current);
      dragCleanupRef.current?.();
      setGestureActive(false);
    },
    [setGestureActive],
  );

  useEffect(() => {
    const clearDropPreview = () => {
      setTransitionDrag(null);
      setMediaDropPreview(null);
    };
    window.addEventListener("dragend", clearDropPreview);
    window.addEventListener("drop", clearDropPreview);
    return () => {
      window.removeEventListener("dragend", clearDropPreview);
      window.removeEventListener("drop", clearDropPreview);
    };
  }, []);

  useEffect(() => {
    dragCleanupRef.current?.();
    setDrag(null);
    setMarquee(null);
    setFadePreview(null);
    setClipboard(null);
    setCaptionEditor(null);
    setSelectedItemIds([]);
    setGestureActive(false);
    usePreviewOverridesStore.getState().reset();
    transitionIdsRef.current = new Set(
      (useTimelineStore.getState().localDoc?.transitions ?? []).map(
        (transition) => transition.transitionId,
      ),
    );
  }, [activeTimelineId, project?.projectId, setGestureActive, setSelectedItemIds]);

  useEffect(() => {
    const itemIds = new Set((localDoc?.items ?? []).map((item) => item.itemId));
    setSelectedItemIds((selected) => {
      const next = selected.filter((itemId) => itemIds.has(itemId));
      return next.length === selected.length ? selected : next;
    });
  }, [localDoc, setSelectedItemIds]);

  const showError = useCallback((message: string) => {
    setOpError(message);
    if (errorTimerRef.current) clearTimeout(errorTimerRef.current);
    errorTimerRef.current = window.setTimeout(() => setOpError(null), 5000);
  }, []);

  const runSequenceAction = useCallback(
    async (action: () => Promise<string | null>) => {
      if (sequenceBusy) return;
      setSequenceBusy(true);
      try {
        const message = await action();
        if (message) showError(message);
      } finally {
        setSequenceBusy(false);
      }
    },
    [sequenceBusy, showError],
  );

  const beginTimelineRename = useCallback((timelineId: string, name: string) => {
    setRenamingTimelineId(timelineId);
    setTimelineNameDraft(name);
  }, []);

  const commitTimelineRename = useCallback(() => {
    if (!project || !renamingTimelineId) return;
    const timelineId = renamingTimelineId;
    const name = timelineNameDraft;
    setRenamingTimelineId(null);
    void runSequenceAction(() => renameSequence(project.projectId, timelineId, name));
  }, [project, renamingTimelineId, runSequenceAction, timelineNameDraft]);

  const confirmTimelineDelete = useCallback(
    (timelineId: string, name: string) => {
      if (!project) return;
      if (timelines.length <= 1) {
        showError("至少保留一条时间线");
        return;
      }
      if (!window.confirm(`确定删除时间线“${name}”吗？此操作不会删除素材文件。`)) return;
      void runSequenceAction(() => deleteSequence(project.projectId, timelineId));
    },
    [project, runSequenceAction, showError, timelines.length],
  );

  useEffect(() => {
    const next = new Set((localDoc?.transitions ?? []).map((transition) => transition.transitionId));
    const removed = [...transitionIdsRef.current].filter((transitionId) => !next.has(transitionId));
    if (removed.length > 0) {
      showError(`转场已移除：${removed.join("、")}（片段边界或服务端状态已变化）`);
    }
    transitionIdsRef.current = next;
  }, [localDoc?.transitions, showError]);

  const submitOne = useCallback(
    (op: Operation) => {
      if (!project) return;
      void submitOps(project.projectId, [op]).then((message) => {
        if (message) showError(message);
      });
    },
    [project, showError],
  );

  const submitMany = useCallback(
    (operations: Operation[]) => {
      if (!project || operations.length === 0) return;
      void submitOps(project.projectId, operations).then((message) => {
        if (message) showError(message);
      });
    },
    [project, showError],
  );

  const copySelection = useCallback(
    (fallbackItemId?: string): TimelineClipboard | null => {
      if (!localDoc) return null;
      const currentSelection = useEditorContextStore.getState().selectedItemIds;
      const copied = createTimelineClipboard(
        localDoc,
        currentSelection.length > 0
          ? currentSelection
          : fallbackItemId
            ? [fallbackItemId]
            : [],
      );
      if (copied) setClipboard(copied);
      return copied;
    },
    [localDoc],
  );

  const pasteItems = useCallback(
    (source: TimelineClipboard | null, targetFrame: number) => {
      if (!source || !localDoc || !project) {
        showError("没有可粘贴的时间线片段");
        return;
      }
      const result = buildPasteOperations(localDoc, source, targetFrame, newItemId);
      if ("error" in result) {
        showError(result.error);
        return;
      }
      setSelectedItemIds(result.itemIds);
      submitMany(result.operations);
    },
    [localDoc, project, setSelectedItemIds, showError, submitMany],
  );

  const duplicateSelection = useCallback(() => {
    const copied = copySelection();
    if (!copied) return;
    pasteItems(copied, duplicateTargetFrame(copied));
  }, [copySelection, pasteItems]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (timelineShortcutsBlocked(event.target)) return;
      const command = event.metaKey || event.ctrlKey;
      if (command && !event.altKey) {
        const key = event.key.toLocaleLowerCase();
        if (key === "c") {
          if (selectedItemIds.length > 0) {
            event.preventDefault();
            copySelection();
          }
          return;
        }
        if (key === "v") {
          if (clipboard) {
            event.preventDefault();
            pasteItems(clipboard, currentFrame);
          }
          return;
        }
        if (key === "d") {
          if (selectedItemIds.length > 0) {
            event.preventDefault();
            duplicateSelection();
          }
          return;
        }
      }
      // V/B 切换选择/刀片模式（对齐 chatcut 的编辑模式快捷键，加分项）。
      if (event.key === "v" || event.key === "V") {
        setEditMode("select");
        return;
      }
      if (event.key === "b" || event.key === "B") {
        setEditMode("blade");
        return;
      }
      if (event.key === " ") {
        if (localDoc && duration > 0) {
          event.preventDefault();
          if (!isPlaying && currentFrame >= duration) setCurrentFrame(0);
          togglePlayback();
        }
        return;
      }
      if (event.key === "Escape") {
        dragCleanupRef.current?.();
        setDrag(null);
        setMarquee(null);
        setFadePreview(null);
        usePreviewOverridesStore.getState().reset();
        setGestureActive(false);
        setSelectedItemIds([]);
        setCaptionEditor(null);
        return;
      }
      if (!project || (event.key !== "Delete" && event.key !== "Backspace")) return;
      const operation = buildSelectionDeleteOperation(selectedItemIds);
      if (!operation) return;
      event.preventDefault();
      setSelectedItemIds([]);
      submitOne(operation);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    selectedItemIds,
    clipboard,
    project,
    localDoc,
    duration,
    isPlaying,
    currentFrame,
    setCurrentFrame,
    setSelectedItemIds,
    setGestureActive,
    togglePlayback,
    submitOne,
    copySelection,
    pasteItems,
    duplicateSelection,
  ]);

  const splitFrame = normalizeFrame(currentFrame);
  const selectedTrack = selectedItem
    ? tracks.find((track) => track.trackId === selectedItem.trackId)
    : undefined;
  const canSplit =
    !!selectedItem &&
    !selectedTrack?.locked &&
    splitFrame > selectedItem.from &&
    splitFrame < itemEnd(selectedItem);

  async function handleTransitionDrop(
    event: React.DragEvent<HTMLDivElement>,
    from: Item,
    to: Item,
    existing?: Transition,
  ) {
    setTransitionDrag(null);
    if (!localDoc || !project) return;
    const raw = event.dataTransfer.getData(TRANSITION_DND_TYPE);
    let payload: TransitionDragPayload;
    try {
      payload = JSON.parse(raw) as TransitionDragPayload;
    } catch {
      showError("无法读取转场拖放数据");
      toast.error("转场添加失败：拖放数据无效");
      return;
    }
    if (
      payload.kind !== "none" &&
      payload.kind !== "crossfade" &&
      payload.kind !== "dipToBlack"
    ) {
      showError("不支持的转场类型");
      toast.error("转场添加失败：类型不受支持");
      return;
    }
    const track = localDoc.tracks.find((candidate) => candidate.trackId === from.trackId);
    const preferredFrames = Math.max(
      1,
      Math.min(localDoc.metadata.fps, to.durationInFrames),
    );
    const frames = existing?.frames ?? transitionFramesForDrop(localDoc, from, to);
    const reason =
      track?.locked
        ? "轨道已锁定"
        : payload.kind === "none"
          ? existing
            ? null
            : "该边界没有可移除的转场"
          : transitionDisableReason(localDoc, from, to, frames);
    if (reason) {
      showError(reason);
      toast.error(`转场操作失败：${reason}`);
      return;
    }
    const operation: Operation =
      payload.kind === "none"
        ? { op: "removeTransition", transitionId: existing!.transitionId }
        : existing
          ? {
              op: "updateTransition",
              transitionId: existing.transitionId,
              transitionPatch: { kind: payload.kind, frames },
            }
          : {
              op: "addTransition",
              transition: {
                transitionId: `tr_${from.itemId}_${to.itemId}`
                  .replaceAll(/[^A-Za-z0-9_-]/g, "_")
                  .slice(0, 128),
                fromItemId: from.itemId,
                toItemId: to.itemId,
                kind: payload.kind,
                frames,
              },
            };
    const message = await submitOps(project.projectId, [operation]);
    if (message) {
      showError(message);
      toast.error(`转场操作失败：${message}`);
      return;
    }
    toast.success(
      payload.kind === "none"
        ? "转场已删除"
        : existing
          ? "转场类型已替换"
          : frames < preferredFrames
            ? `转场已添加；源素材余量有限，时长已自动缩短为 ${frames} 帧`
            : "转场已添加",
    );
  }

  function updateMediaDropPreview(
    event: React.DragEvent<HTMLDivElement>,
    track: Track,
  ) {
    if (track.locked || !localDoc) {
      setMediaDropPreview(null);
      return;
    }
    const raw = event.dataTransfer.getData(MEDIA_DND_TYPE);
    if (!raw) return;
    try {
      const payload = JSON.parse(raw) as { mediaId: string; kind: MediaKind };
      const media = localDoc.media.find((candidate) => candidate.mediaId === payload.mediaId);
      if (!media || !itemKindAllowedOn(media.kind as ItemKind, track.kind)) {
        setMediaDropPreview(null);
        return;
      }
      const rect = event.currentTarget.getBoundingClientRect();
      let from = Math.max(0, Math.round((event.clientX - rect.left) / pxPerFrame));
      if (snapEnabled) {
        from = snapFrame(
          from,
          snapTargets(localDoc, track.trackId, "", currentFrame),
          pxPerFrame,
        ).frame;
      }
      setMediaDropPreview({
        hoverTrackId: track.trackId,
        plan: buildInsertMediaOps(localDoc, media, from, track.trackId),
      });
    } catch {
      setMediaDropPreview(null);
    }
  }

  function handleDrop(event: React.DragEvent<HTMLDivElement>, track: Track) {
    event.preventDefault();
    setMediaDropPreview(null);
    if (track.locked || !localDoc || !project) return;
    if (event.dataTransfer.types.includes(TRANSITION_DND_TYPE)) {
      setTransitionDrag(null);
      showError("请将转场拖到两个相邻视频片段之间的边界");
      return;
    }
    const raw = event.dataTransfer.getData(MEDIA_DND_TYPE);
    if (!raw) return;
    let payload: { mediaId: string; kind: MediaKind };
    try {
      payload = JSON.parse(raw) as typeof payload;
    } catch {
      return;
    }
    const media = localDoc.media.find((candidate) => candidate.mediaId === payload.mediaId);
    if (!media) return showError("素材不存在或已被删除");
    if (!isMediaReadyForTimeline(media)) {
      return showError("素材基础探测尚未完成，暂时不能加入时间线");
    }
    if (!itemKindAllowedOn(media.kind as ItemKind, track.kind)) {
      return showError(`${media.kind} 素材不能放到 ${track.kind} 轨道`);
    }
    const rect = event.currentTarget.getBoundingClientRect();
    let from = Math.max(0, Math.round((event.clientX - rect.left) / pxPerFrame));
    if (snapEnabled) {
      from = snapFrame(
        from,
        snapTargets(localDoc, track.trackId, "", currentFrame),
        pxPerFrame,
      ).frame;
    }
    const plan = buildInsertMediaOps(localDoc, media, from, track.trackId);
    submitMany(plan.operations);
  }

  function startDrag(
    event: React.PointerEvent,
    item: Item,
    mode: TimelineDragMode,
    locked: boolean,
  ) {
    if (event.button !== 0) return;
    event.stopPropagation();
    if (locked || !localDoc || !project) return;
    event.preventDefault();
    dragCleanupRef.current?.();
    const doc = localDoc;
    const projectId = project.projectId;
    const startX = event.clientX;
    const startY = event.clientY;
    let activated = mode !== "move";

    const stateAt = (clientX: number, clientY: number): TimelineDragPreview => {
      const hit = document.elementFromPoint(clientX, clientY)?.closest<HTMLElement>("[data-track-id]");
      const targetTrackId =
        mode === "move"
          ? compatibleTargetTrack(doc, item, hit?.dataset.trackId ?? null)
          : item.trackId;
      const raw = clampEditDelta(
        doc,
        item,
        mode,
        Math.round((clientX - startX) / pxPerFrame),
      );
      if (!snapEnabled) {
        return { itemId: item.itemId, mode, deltaFrames: raw, targetTrackId, snapTarget: null };
      }
      const boundary =
        mode === "trimEnd" ? itemEnd(item) + raw : item.from + raw;
      const snapped = snapFrame(
        boundary,
        snapTargets(doc, targetTrackId, item.itemId, currentFrame),
        pxPerFrame,
        8,
        mode === "move" ? item.durationInFrames : 0,
      );
      const base = mode === "trimEnd" ? itemEnd(item) : item.from;
      const delta = clampEditDelta(doc, item, mode, snapped.frame - base);
      return {
        itemId: item.itemId,
        mode,
        deltaFrames: delta,
        targetTrackId,
        snapTarget: snapped.target,
      };
    };

    if (activated) setGestureActive(true);
    const onMove = (moveEvent: PointerEvent) => {
      if (
        !activated &&
        !exceedsPointerThreshold(
          startX,
          startY,
          moveEvent.clientX,
          moveEvent.clientY,
          POINTER_DRAG_THRESHOLD_PX,
        )
      ) {
        return;
      }
      if (!activated) {
        activated = true;
        setGestureActive(true);
      }
      setDrag(stateAt(moveEvent.clientX, moveEvent.clientY));
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      if (dragCleanupRef.current === cleanup) dragCleanupRef.current = null;
    };
    const onUp = (upEvent: PointerEvent) => {
      if (!activated) {
        cleanup();
        setDrag(null);
        setGestureActive(false);
        return;
      }
      const final = stateAt(upEvent.clientX, upEvent.clientY);
      cleanup();
      setDrag(null);
      const changed =
        final.deltaFrames !== 0 ||
        (mode === "move" && final.targetTrackId !== item.trackId);
      if (changed) {
        void submitOps(projectId, [buildEditOperation(item, final)]).then((message) => {
          if (message) showError(message);
        });
      }
      setGestureActive(false);
    };
    const onCancel = () => {
      cleanup();
      setDrag(null);
      setGestureActive(false);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp, { once: true });
    window.addEventListener("pointercancel", onCancel, { once: true });
    dragCleanupRef.current = cleanup;
  }

  /** 刀片模式下点击片段任意位置直接分割（按点击处的像素位置换算成帧，
   *  复用 pxPerFrame 换算方式），不需要先选中+移动播放头。 */
  function handleClipPointerDown(
    event: React.PointerEvent<HTMLDivElement>,
    item: Item,
    locked: boolean,
  ) {
    if (editMode !== "blade") {
      const additive = event.metaKey || event.ctrlKey || event.shiftKey;
      if (additive) {
        event.preventDefault();
        event.stopPropagation();
        setSelectedItemIds((selected) =>
          updateItemSelection(selected, item.itemId, true),
        );
        return;
      }
      setSelectedItemIds((selected) =>
        selected.includes(item.itemId) ? selected : [item.itemId],
      );
      startDrag(event, item, "move", locked);
      return;
    }
    if (event.button !== 0) return;
    event.stopPropagation();
    setSelectedItemIds([item.itemId]);
    if (locked || !project) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const frame = item.from + Math.round((event.clientX - rect.left) / pxPerFrame);
    if (frame <= item.from || frame >= itemEnd(item)) return;
    void submitOps(project.projectId, [{ op: "split", itemId: item.itemId, frame }]).then(
      (message) => {
        if (message) showError(message);
      },
    );
  }

  function handleClipContextMenu(
    event: React.MouseEvent<HTMLElement>,
    item: Item,
  ) {
    event.stopPropagation();
    setSelectedItemIds((selected) =>
      selected.includes(item.itemId) ? selected : [item.itemId],
    );
  }

  function requestPreciseAgentAdjustment(item: Item) {
    const current = snapshotEditorContext();
    const selected = current.selectedItemIds.includes(item.itemId)
      ? current.selectedItemIds
      : [item.itemId];
    const context = { ...current, selectedItemIds: selected };
    setSelectedItemIds(selected);
    requestAgentPrompt(agentAdjustmentPrompt(context), context);
    if (!project?.sessionId) {
      showError("Agent 服务未启用；移动、裁剪、分割等直接时间线编辑仍可使用");
    }
  }

  function requestTrackAgentAdjustment(track: Track) {
    const context = snapshotEditorContext();
    requestAgentPrompt(trackAgentAdjustmentPrompt(context, track), context);
    if (!project?.sessionId) {
      showError("Agent 服务未启用；轨道静音、可见性、锁定和删除仍可直接操作");
    }
  }

  function enterCropEditing(item: Item) {
    pausePlayback();
    if (currentFrame < item.from || currentFrame >= itemEnd(item)) {
      setCurrentFrame(item.from);
    }
    setCropEditingItem(item.itemId);
  }

  function startAudioSeparation(item: Item) {
    if (!project) return;
    void separateAudioItem(project.projectId, item.itemId, (phase, message) => {
      setAudioSeparation((current) => ({
        ...current,
        [item.itemId]: { phase, message },
      }));
      if (phase === "success") toast.success(message ?? "音频分离完成");
      if (phase === "error") showError(message ?? "音频分离失败");
    });
  }

  function startFadeDrag(
    event: React.PointerEvent<HTMLButtonElement>,
    item: Item,
    edge: FadeEdge,
    locked: boolean,
  ) {
    if (event.button !== 0 || locked || !localDoc || !project) return;
    if (fadeDisabledReason(localDoc, item, edge)) return;
    event.preventDefault();
    event.stopPropagation();
    dragCleanupRef.current?.();
    setSelectedItemIds((selected) =>
      selected.includes(item.itemId) ? selected : [item.itemId],
    );
    const startX = event.clientX;
    const initial = item[edge] ?? 0;
    const projectId = project.projectId;
    const framesAt = (clientX: number) =>
      clampFadeFrames(
        item,
        edge,
        initial +
          Math.round(
            ((edge === "fadeInFrames" ? 1 : -1) * (clientX - startX)) /
              pxPerFrame,
          ),
      );
    const previewAt = (clientX: number) => {
      const frames = framesAt(clientX);
      setFadePreview({ itemId: item.itemId, edge, frames });
      setItemPreviewPatch(item.itemId, { [edge]: frames });
    };
    setGestureActive(true);
    previewAt(startX);
    const onMove = (moveEvent: PointerEvent) => previewAt(moveEvent.clientX);
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      clearItemPreviewPatch(item.itemId);
      setFadePreview(null);
      if (dragCleanupRef.current === cleanup) dragCleanupRef.current = null;
    };
    const onUp = (upEvent: PointerEvent) => {
      const frames = framesAt(upEvent.clientX);
      cleanup();
      if (frames !== initial) {
        void submitOps(projectId, [
          { op: "updateItem", itemId: item.itemId, patch: { [edge]: frames } },
        ]).then((message) => {
          if (message) showError(message);
        });
      }
      setGestureActive(false);
    };
    const onCancel = () => {
      cleanup();
      setGestureActive(false);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp, { once: true });
    window.addEventListener("pointercancel", onCancel, { once: true });
    dragCleanupRef.current = cleanup;
  }

  function startLaneGesture(event: React.PointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || event.currentTarget !== event.target) return;
    const lane = event.currentTarget;
    const startX = event.clientX;
    const startY = event.clientY;
    const additive = event.metaKey || event.ctrlKey || event.shiftKey;
    const baseSelection = additive
      ? [...useEditorContextStore.getState().selectedItemIds]
      : [];
    if (!additive) setSelectedItemIds([]);
    dragCleanupRef.current?.();
    let activated = false;

    const updateMarquee = (clientX: number, clientY: number) => {
      const viewportRect = normalizeSelectionRect(startX, startY, clientX, clientY);
      const bodyRect = lanesRef.current?.getBoundingClientRect();
      if (bodyRect) {
        setMarquee({
          left: viewportRect.left - bodyRect.left,
          top: viewportRect.top - bodyRect.top,
          width: viewportRect.right - viewportRect.left,
          height: viewportRect.bottom - viewportRect.top,
        });
      }
      const candidates = Array.from(
        lanesRef.current?.querySelectorAll<HTMLElement>("[data-timeline-item-id]") ?? [],
      ).map((element) => {
        const rect = element.getBoundingClientRect();
        return {
          itemId: element.dataset.timelineItemId!,
          rect: {
            left: rect.left,
            top: rect.top,
            right: rect.right,
            bottom: rect.bottom,
          } satisfies SelectionRect,
        };
      });
      setSelectedItemIds(
        selectionFromMarquee(baseSelection, candidates, viewportRect, additive),
      );
    };
    const onMove = (moveEvent: PointerEvent) => {
      if (
        !activated &&
        !exceedsPointerThreshold(
          startX,
          startY,
          moveEvent.clientX,
          moveEvent.clientY,
          POINTER_DRAG_THRESHOLD_PX,
        )
      ) {
        return;
      }
      if (!activated) {
        activated = true;
        setGestureActive(true);
      }
      updateMarquee(moveEvent.clientX, moveEvent.clientY);
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      if (dragCleanupRef.current === cleanup) dragCleanupRef.current = null;
    };
    const onUp = (upEvent: PointerEvent) => {
      if (activated) updateMarquee(upEvent.clientX, upEvent.clientY);
      cleanup();
      setMarquee(null);
      if (!activated) {
        const rect = lane.getBoundingClientRect();
        pausePlayback();
        setCurrentFrame(
          clampFrame((upEvent.clientX - rect.left) / pxPerFrame, duration),
        );
      }
      setGestureActive(false);
    };
    const onCancel = () => {
      cleanup();
      setMarquee(null);
      setGestureActive(false);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp, { once: true });
    window.addEventListener("pointercancel", onCancel, { once: true });
    dragCleanupRef.current = cleanup;
  }

  function clipGeometry(item: Item): { left: number; width: number } {
    let from = item.from;
    let end = itemEnd(item);
    if (drag?.itemId === item.itemId) {
      if (drag.mode === "move") {
        from += drag.deltaFrames;
        end += drag.deltaFrames;
      } else if (drag.mode === "trimStart") from += drag.deltaFrames;
      else end += drag.deltaFrames;
    }
    return { left: from * pxPerFrame, width: (end - from) * pxPerFrame };
  }

  function startSeek(event: React.PointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return;
    const row = event.currentTarget;
    const seekAt = (clientX: number) => {
      const rect = row.getBoundingClientRect();
      setCurrentFrame(
        clampFrame(
          (clientX - rect.left - TRACK_HEADER_PX) / pxPerFrame,
          duration,
        ),
      );
    };
    pausePlayback();
    seekAt(event.clientX);
    const onMove = (moveEvent: PointerEvent) => seekAt(moveEvent.clientX);
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

  function setCaptionTracksMuted(targets: Track[], muted: boolean) {
    const operations = targets
      .filter((track) => !track.locked && track.muted !== muted)
      .map((track) => ({
          op: "updateTrack" as const,
          trackId: track.trackId,
          trackPatch: { muted },
        }));
    if (
      operations.length === 0 &&
      targets.some((track) => track.locked && track.muted !== muted)
    ) {
      showError("字幕轨已锁定，无法切换 CC");
      return;
    }
    submitMany(operations);
  }

  function generateSelectedCaptions() {
    if (!localDoc || !captionMediaId) return;
    const media = localDoc.media.find(
      (candidate) =>
        candidate.mediaId === captionMediaId && candidate.kind === "video",
    );
    if (!media) return;
    const operations = buildTranscriptCaptionOps(localDoc, media);
    if (operations.length === 0) {
      showError("当前视频没有可用文字稿，或尚未放入时间线");
      return;
    }
    submitMany(operations);
  }

  function openCaptionStyleEditor() {
    if (!localDoc) return;
    const selectedCaption =
      selectedItem?.kind === "text" ? selectedItem : undefined;
    const firstForMedia = items.find(
      (item) =>
        item.kind === "text" &&
        autoCaptionMediaIdForTrack(localDoc, item.trackId) === captionMediaId,
    );
    const target = selectedCaption ?? firstForMedia;
    if (!target) {
      showError("请先生成字幕，再编辑模板和样式");
      return;
    }
    setSelectedItemIds([target.itemId]);
    setCaptionEditor({ itemId: target.itemId });
  }

  function openNewCaptionEditor() {
    if (!localDoc) return;
    const matchingAutoTrack = captionTracks.find(
      (track) =>
        captionMediaId &&
        autoCaptionMediaIdForTrack(localDoc, track.trackId) === captionMediaId,
    );
    const manualTrack = captionTracks.find(
      (track) => !isAutoCaptionTrackId(track.trackId),
    );
    setCaptionEditor({
      newCaptionTrackId:
        matchingAutoTrack?.trackId ??
        manualTrack?.trackId ??
        newTrackId(localDoc.tracks, "caption"),
    });
  }

  const laneMinWidth =
    Math.max(
      0,
      ...items.map((item) => {
        const geometry = clipGeometry(item);
        return geometry.left + geometry.width;
      }),
      duration * pxPerFrame,
      currentFrame * pxPerFrame,
    ) + 200;
  const stepFrames = rulerStepFrames(fps, pxPerFrame);
  const rulerTickCount = Math.min(1000, Math.ceil(laneMinWidth / (stepFrames * pxPerFrame)) + 1);
  const rulerTicks = Array.from({ length: rulerTickCount }, (_, index) => index * stepFrames);
  const minorStepFrames = Math.max(1, Math.round(stepFrames / 4));
  const minorTickCount = Math.min(
    4000,
    Math.ceil(laneMinWidth / (minorStepFrames * pxPerFrame)) + 1,
  );
  const minorTicks = Array.from(
    { length: minorTickCount },
    (_, index) => index * minorStepFrames,
  ).filter((frame) => frame % stepFrames !== 0);
  const majorGridWidth = Math.max(1, stepFrames * pxPerFrame);
  const minorGridWidth = Math.max(1, minorStepFrames * pxPerFrame);
  const showColdStartHint =
    !!localDoc &&
    items.length === 0 &&
    displayTracks.length === 1 &&
    displayTracks[0]?.trackId === baseVideoTrackId;

  function fitAllTimelineContent() {
    if (duration <= 0) return;
    const scroller = timelineScrollRef.current;
    const viewportWidth =
      (scroller?.clientWidth ?? 0) -
      TRACK_HEADER_PX -
      FIT_ALL_HORIZONTAL_MARGIN_PX;
    if (viewportWidth <= 0) return;
    setPxPerFrame(
      Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, viewportWidth / duration)),
    );
    if (scroller) scroller.scrollLeft = 0;
  }

  return (
    <div className="relative flex h-full flex-col bg-[#EDECEA]">
      <div
        className="flex h-9 shrink-0 items-end border-b border-[#D8D6D2] bg-[#F2F2F0] px-2"
        data-testid="timeline-tabs"
      >
        <div className="flex min-w-0 flex-1 items-end gap-0.5 overflow-x-auto">
          {timelines.map((timeline) => {
            const active = timeline.timelineId === activeTimelineId;
            const renaming = timeline.timelineId === renamingTimelineId;
            return (
              <div
                key={timeline.timelineId}
                className={`group flex h-8 min-w-28 max-w-52 shrink-0 items-center border-x border-t px-1 ${
                  active
                    ? "border-[#C9C7C2] bg-[#EDECEA] text-foreground"
                    : "border-transparent text-muted-foreground hover:bg-[#E9E8E5]"
                }`}
                data-active={active ? "true" : "false"}
              >
                {renaming ? (
                  <input
                    autoFocus
                    aria-label={`重命名 ${timeline.name}`}
                    className="h-6 min-w-0 flex-1 border border-[#B8B6B1] bg-white px-1.5 text-xs outline-none focus:border-slate-500"
                    value={timelineNameDraft}
                    onChange={(event) => setTimelineNameDraft(event.target.value)}
                    onBlur={commitTimelineRename}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") commitTimelineRename();
                      if (event.key === "Escape") setRenamingTimelineId(null);
                    }}
                  />
                ) : (
                  <button
                    type="button"
                    className="min-w-0 flex-1 truncate px-1 text-left text-xs"
                    disabled={sequenceBusy}
                    aria-label={`切换到 ${timeline.name}`}
                    onClick={() => {
                      if (!project || active) return;
                      void runSequenceAction(() =>
                        switchSequence(project.projectId, timeline.timelineId),
                      );
                    }}
                    onDoubleClick={() =>
                      beginTimelineRename(timeline.timelineId, timeline.name)
                    }
                  >
                    {timeline.name}
                  </button>
                )}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button
                      type="button"
                      className="flex size-6 shrink-0 items-center justify-center rounded-[4px] opacity-60 hover:bg-black/5 group-hover:opacity-100"
                      aria-label={`${timeline.name} 更多操作`}
                      disabled={sequenceBusy}
                    >
                      <Ellipsis className="size-3.5" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start">
                    <DropdownMenuItem
                      onSelect={() =>
                        beginTimelineRename(timeline.timelineId, timeline.name)
                      }
                    >
                      <Pencil />
                      重命名时间线
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      disabled={timelines.length <= 1}
                      onSelect={() =>
                        confirmTimelineDelete(timeline.timelineId, timeline.name)
                      }
                    >
                      <Trash2 />
                      删除时间线
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            );
          })}
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          className="mb-0.5 ml-1 shrink-0"
          aria-label="新建时间线"
          title="新建时间线"
          disabled={!project || sequenceBusy}
          onClick={() => {
            if (!project) return;
            void runSequenceAction(() => createSequence(project.projectId));
          }}
        >
          <Plus />
        </Button>
      </div>
      <div className="relative grid h-10 shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center border-b border-[#D8D6D2] bg-[#F2F2F0] px-2">
        <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
          <Button
            variant={editMode === "select" ? "secondary" : "ghost"}
            size="icon-sm"
            title="选择模式 (V)"
            onClick={() => setEditMode("select")}
          >
            <MousePointer2 />
          </Button>
          <Button
            variant={editMode === "blade" ? "secondary" : "ghost"}
            size="icon-sm"
            title="刀片编辑模式 (B)：点击片段任意位置直接分割"
            onClick={() => setEditMode("blade")}
          >
            <Slice />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={!canSplit}
            title={canSplit ? "在播放头位置分割" : "选中片段并把播放头置于片段内部"}
            onClick={() =>
              selectedItem &&
              submitOne({
                op: "split",
                itemId: selectedItem.itemId,
                frame: splitFrame,
              })
            }
          >
            <Scissors />
          </Button>
          <Button
            variant={snapEnabled ? "secondary" : "ghost"}
            size="icon-sm"
            title={snapEnabled ? "关闭吸附" : "开启吸附"}
            onClick={() => setSnapEnabled((enabled) => !enabled)}
          >
            <Magnet />
          </Button>
        </div>

        <div className="flex items-center px-4">
          <div className="flex items-baseline gap-1.5 whitespace-nowrap font-mono text-[13px]">
            <span className="font-medium text-foreground">
              {formatTimelineClock(currentFrame, fps)}
            </span>
            <span className="text-muted-foreground">/</span>
            <span className="text-muted-foreground">
              {formatTimelineClock(duration, fps)}
            </span>
          </div>
        </div>

        <div className="flex min-w-0 items-center justify-end gap-1 overflow-x-auto">
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={pxPerFrame <= MIN_ZOOM}
            title="缩小时间线"
            onClick={() =>
              setPxPerFrame((value) =>
                Math.max(MIN_ZOOM, value - ZOOM_STEP),
              )
            }
          >
            <ZoomOut />
          </Button>
          <input
            aria-label="时间线缩放"
            title="拖动调整时间线缩放"
            type="range"
            min={MIN_ZOOM}
            max={MAX_ZOOM}
            step={ZOOM_STEP}
            value={pxPerFrame}
            className="w-20 accent-slate-700 dark:accent-slate-300"
            onChange={(event) => setPxPerFrame(Number(event.target.value))}
          />
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={pxPerFrame >= MAX_ZOOM}
            title="放大时间线"
            onClick={() =>
              setPxPerFrame((value) =>
                Math.min(MAX_ZOOM, value + ZOOM_STEP),
              )
            }
          >
            <ZoomIn />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="适配全部内容"
            title="适配全部内容"
            disabled={duration <= 0}
            onClick={fitAllTimelineContent}
          >
            <Maximize2 />
          </Button>
          <Separator orientation="vertical" className="mx-1 h-5" />
          <Popover>
            <PopoverTrigger asChild>
              <Button
                variant={
                  captionTracks.some((track) => !track.muted)
                    ? "secondary"
                    : "ghost"
                }
                size="icon-sm"
                aria-label="字幕设置"
                title="字幕生成、样式和显示"
              >
                <Captions />
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-72 space-y-3 p-3" align="end">
              <div>
                <strong className="text-xs font-medium">字幕</strong>
                <p className="mt-0.5 text-[10px] text-muted-foreground">
                  字幕保存在独立 C 轨，并显示在视频轨上方。
                </p>
              </div>
              <label className="block text-xs">
                来源视频
                <select
                  aria-label="字幕来源视频"
                  className="mt-1 h-8 w-full rounded border bg-background px-2 text-xs"
                  value={captionMediaId}
                  disabled={timelineVideoMedia.length === 0}
                  onChange={(event) => setCaptionMediaId(event.target.value)}
                >
                  {timelineVideoMedia.length === 0 ? (
                    <option value="">时间线暂无视频</option>
                  ) : (
                    timelineVideoMedia.map((media) => (
                      <option key={media.mediaId} value={media.mediaId}>
                        {media.displayName ??
                          media.blobKey?.split("/").pop() ??
                          media.mediaId}
                      </option>
                    ))
                  )}
                </select>
              </label>
              <Button
                size="sm"
                variant="outline"
                className="w-full"
                disabled={!localDoc || !project}
                onClick={openNewCaptionEditor}
              >
                <Plus />
                新增字幕
              </Button>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  className="flex-1"
                  disabled={!captionMediaId}
                  onClick={generateSelectedCaptions}
                >
                  <Captions />
                  生成 / 更新
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1"
                  disabled={captionTracks.length === 0}
                  onClick={openCaptionStyleEditor}
                >
                  <Pencil />
                  模板 / 样式
                </Button>
              </div>
              <label className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  aria-label="显示整套字幕"
                  checked={
                    captionTracks.length > 0 &&
                    captionTracks.some((track) => !track.muted)
                  }
                  disabled={captionTracks.length === 0}
                  onChange={(event) =>
                    setCaptionTracksMuted(
                      captionTracks,
                      !event.target.checked,
                    )
                  }
                />
                显示整套字幕
              </label>
            </PopoverContent>
          </Popover>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="时间线更多设置"
                title="更多设置"
              >
                <Ellipsis />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => setPxPerFrame(1)}>
                <ZoomIn />
                缩放重置为 100%
              </DropdownMenuItem>
              {selectedItem?.kind === "text" && (
                <DropdownMenuItem
                  onSelect={() =>
                    setCaptionEditor({ itemId: selectedItem.itemId })
                  }
                >
                  <Pencil />
                  编辑选中字幕
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        {opError && (
          <span
            className="absolute left-2 top-full z-50 max-w-[60%] truncate border border-destructive/20 bg-background px-2 py-1 text-xs text-destructive"
            title={opError}
          >
            {opError}
          </span>
        )}
      </div>

      <div
        ref={timelineScrollRef}
        data-track-overflow={trackVerticalLayout.overflow ? "true" : "false"}
        className="min-h-0 flex-1 overflow-x-auto bg-[#EDECEA]"
        style={{
          overflowY: trackVerticalLayout.overflow ? "auto" : "hidden",
        }}
        onScroll={handleTimelineScroll}
      >
        <div
          className="relative flex h-full min-h-0 flex-col"
          style={{ minWidth: laneMinWidth + TRACK_HEADER_PX }}
        >
          <div
            className="sticky top-0 z-40 flex h-8 shrink-0 border-b border-[#D8D6D2] bg-[#EDECEA]"
            onPointerDown={startSeek}
          >
            <div
              className="sticky left-0 z-50 shrink-0 border-r border-[#D8D6D2] bg-[#EDECEA]"
              style={{ width: TRACK_HEADER_PX }}
              aria-hidden="true"
            />
            <div className="relative flex-1" style={{ minWidth: laneMinWidth }}>
              {minorTicks.map((frame) => (
                <span
                  key={`minor-${frame}`}
                  className="pointer-events-none absolute bottom-0 h-1.5 border-l border-slate-400/45 dark:border-slate-600/60"
                  style={{ left: frame * pxPerFrame }}
                />
              ))}
              {rulerTicks.map((frame) => (
                <div
                  key={frame}
                  className="pointer-events-none absolute inset-y-0 border-l border-slate-500/60 dark:border-slate-500/80"
                  style={{ left: frame * pxPerFrame }}
                >
                  <span className="absolute left-1.5 top-1 whitespace-nowrap font-mono text-[11px] leading-none text-slate-600 dark:text-slate-300">
                    {formatTimelineClock(frame, fps)}
                  </span>
                </div>
              ))}
            </div>
          </div>
          <div
            ref={lanesRef}
            data-testid="timeline-lanes"
            className="relative flex flex-1 flex-col bg-[#E8E7E4]"
            style={{
              boxSizing: "border-box",
              minHeight: trackVerticalLayout.contentHeight,
              paddingTop: trackVerticalLayout.topOffset,
            }}
          >
          {showColdStartHint && (
            <div
              data-testid="cold-start-drop-hint"
              className="pointer-events-none absolute z-10 flex h-11 items-center justify-center text-xs text-muted-foreground"
              style={{
                left: TRACK_HEADER_PX,
                right: 0,
                top: trackVerticalLayout.topOffset,
              }}
            >
              将素材拖到这里开始剪辑
            </div>
          )}
          {displayTracks.map((track) => {
            const isBaseVideoTrack = track.trackId === baseVideoTrackId;
            const linkedCaptionTracks =
              track.kind === "video" && localDoc
                ? captionTracksForVideoTrack(localDoc, track.trackId)
                : [];
            const captionsMuted =
              linkedCaptionTracks.length > 0 &&
              linkedCaptionTracks.every((captionTrack) => captionTrack.muted);
            const captionsLocked = linkedCaptionTracks.some(
              (captionTrack) => captionTrack.locked,
            );
            const trackItems = items.filter(
              (item) => item.trackId === track.trackId,
            );
            const trackMetrics = trackVisualMetrics(localDoc!, track);
            const hasContent = trackMetrics.layout !== "empty";
            const isMediaDropTarget =
              !!mediaDropPreview &&
              (mediaDropPreview.plan.createdTrack
                ? mediaDropPreview.hoverTrackId === track.trackId
                : mediaDropPreview.plan.track.trackId === track.trackId);
            return (
            <div
              key={track.trackId}
              data-track-layout={trackMetrics.layout}
              data-av-boundary={isBaseVideoTrack ? "video" : undefined}
              className={`group/track flex shrink-0 items-stretch border-b border-[#D8D6D2] transition-colors ${
                hasContent
                  ? "bg-[#E8E7E4] hover:bg-[#E3E1DD]"
                  : "bg-[#E8E7E4] hover:bg-[#E3E1DD]"
              }`}
              style={{
                height: trackMetrics.height,
              }}
            >
              <div
                data-testid={`track-header-${track.trackId}`}
                className="sticky left-0 z-30 flex shrink-0 items-center gap-1 border-r border-[#D8D6D2] bg-[#E8E7E4] px-2 transition-colors group-hover/track:bg-[#E3E1DD]"
                style={{ width: TRACK_HEADER_PX }}
                title={`内部轨道 ID：${track.trackId}`}
              >
                <span
                  className={`flex h-5 min-w-7 shrink-0 items-center justify-center rounded-[4px] px-1 text-[11px] font-semibold ${
                    track.kind === "video"
                      ? "bg-sky-200/80 text-sky-800 dark:bg-sky-900/70 dark:text-sky-200"
                      : track.kind === "audio"
                        ? "bg-emerald-200/80 text-emerald-800 dark:bg-emerald-900/70 dark:text-emerald-200"
                        : "bg-violet-200/70 text-violet-800 dark:bg-violet-900/60 dark:text-violet-200"
                  }`}
                >
                  {trackDisplayCode(track, tracks)}
                </span>
                <span className="min-w-0 flex-1 truncate text-[12px] text-slate-600 dark:text-slate-300">
                  {track.kind === "caption"
                    ? "字幕轨"
                    : track.name ?? `${TRACK_KIND_LABEL[track.kind]}轨`}
                </span>
                <div className="flex shrink-0 items-center gap-0.5">
                  {track.kind === "video" && (
                    <button
                      type="button"
                      className="flex size-7 items-center justify-center rounded-[5px] text-slate-600 hover:bg-slate-400/25 hover:text-slate-900 disabled:opacity-40 dark:text-slate-300 dark:hover:bg-slate-700 dark:hover:text-white"
                      title={track.hidden ? "显示视频轨画面" : "隐藏视频轨画面"}
                      aria-label={track.hidden ? "显示视频轨画面" : "隐藏视频轨画面"}
                      disabled={!!track.locked}
                      onClick={() =>
                        submitOne({
                          op: "updateTrack",
                          trackId: track.trackId,
                          trackPatch: { hidden: !track.hidden },
                        })
                      }
                    >
                      {track.hidden ? (
                        <EyeOff className="size-4" />
                      ) : (
                        <Eye className="size-4" />
                      )}
                    </button>
                  )}
                  <button
                    type="button"
                    className="flex size-7 items-center justify-center rounded-[5px] text-slate-600 hover:bg-slate-400/25 hover:text-slate-900 disabled:opacity-40 dark:text-slate-300 dark:hover:bg-slate-700 dark:hover:text-white"
                    title={
                      track.kind === "video"
                        ? track.muted
                          ? "恢复视频轨内嵌音频"
                          : "静音视频轨内嵌音频"
                        : track.kind === "caption"
                          ? track.muted
                            ? "显示字幕轨"
                            : "隐藏字幕轨"
                          : track.muted
                            ? "取消静音轨道"
                            : "静音轨道"
                    }
                    aria-label={
                      track.kind === "video"
                        ? track.muted
                          ? "恢复视频轨内嵌音频"
                          : "静音视频轨内嵌音频"
                        : track.kind === "caption"
                          ? track.muted
                            ? "显示字幕轨"
                            : "隐藏字幕轨"
                          : track.muted
                            ? "取消静音轨道"
                            : "静音轨道"
                    }
                    disabled={!!track.locked}
                    onClick={() =>
                      submitOne({
                        op: "updateTrack",
                        trackId: track.trackId,
                        trackPatch: { muted: !track.muted },
                      })
                    }
                  >
                    {track.kind === "caption" ? (
                      track.muted ? (
                        <EyeOff className="size-4" />
                      ) : (
                        <Captions className="size-4" />
                      )
                    ) : track.muted ? (
                      <VolumeX className="size-4" />
                    ) : (
                      <Volume2 className="size-4" />
                    )}
                  </button>
                  <button
                    type="button"
                    className="flex size-7 items-center justify-center rounded-[5px] text-slate-600 hover:bg-slate-400/25 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-700 dark:hover:text-white"
                    title={track.locked ? "解锁轨道" : "锁定轨道"}
                    aria-label={track.locked ? "解锁轨道" : "锁定轨道"}
                    onClick={() =>
                      submitOne({
                        op: "updateTrack",
                        trackId: track.trackId,
                        trackPatch: { locked: !track.locked },
                      })
                    }
                  >
                    {track.locked ? (
                      <Lock className="size-4" />
                    ) : (
                      <LockOpen className="size-4 opacity-65" />
                    )}
                  </button>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        className="flex size-7 items-center justify-center rounded-[5px] text-slate-600 hover:bg-slate-400/25 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-700 dark:hover:text-white"
                        aria-label={`${trackDisplayCode(track, tracks)} 更多操作`}
                        title="更多轨道操作"
                      >
                        <Ellipsis className="size-4" />
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        onSelect={() => {
                          if (!localDoc) return;
                          const index =
                            localDoc.tracks.reduce(
                              (last, candidate, candidateIndex) =>
                                candidate.kind === "video" ? candidateIndex : last,
                              -1,
                            ) + 1;
                          submitOne({
                            op: "addTrack",
                            track: {
                              trackId: newTrackId(localDoc.tracks, "video"),
                              kind: "video",
                              name: "视频轨",
                              createdBy: "user",
                            },
                            index,
                          });
                        }}
                      >
                        <Plus />
                        在上方添加视频轨
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onSelect={() => {
                          if (!localDoc) return;
                          const lastAudio = localDoc.tracks.reduce(
                            (last, candidate, candidateIndex) =>
                              candidate.kind === "audio" ? candidateIndex : last,
                            -1,
                          );
                          submitOne({
                            op: "addTrack",
                            track: {
                              trackId: newTrackId(localDoc.tracks, "audio"),
                              kind: "audio",
                              name: "音频轨",
                              createdBy: "user",
                            },
                            index: lastAudio >= 0 ? lastAudio + 1 : localDoc.tracks.length,
                          });
                        }}
                      >
                        <Plus />
                        在下方添加音频轨
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onSelect={() => {
                          const next = window.prompt("轨道名称", track.name ?? "");
                          if (next?.trim()) {
                            submitOne({
                              op: "updateTrack",
                              trackId: track.trackId,
                              trackPatch: { name: next.trim() },
                            });
                          }
                        }}
                      >
                        <Pencil />
                        重命名轨道
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      {track.kind === "video" && (
                        <DropdownMenuItem
                          disabled={
                            linkedCaptionTracks.length === 0 || captionsLocked
                          }
                          onSelect={() =>
                            setCaptionTracksMuted(
                              linkedCaptionTracks,
                              !captionsMuted,
                            )
                          }
                        >
                          <Captions />
                          {captionsMuted ? "显示字幕" : "隐藏字幕"}
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuItem
                        onSelect={() => requestTrackAgentAdjustment(track)}
                      >
                        <Pencil />
                        让 Agent 调整轨道…
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        disabled={isBaseVideoTrack}
                        className="focus:bg-destructive/10 focus:text-destructive"
                        onSelect={() => {
                          const count = trackItems.length;
                          if (
                            window.confirm(
                              count
                                ? `删除轨道会同时删除 ${count} 个片段，确定？`
                                : `确定删除轨道 ${track.trackId}？`,
                            )
                          ) {
                            submitOne({
                              op: "removeTrack",
                              trackId: track.trackId,
                            });
                          }
                        }}
                      >
                        <Trash2 />
                        删除轨道
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
              <div
                data-track-id={track.trackId}
                className={`relative flex-1 bg-[#E8E7E4] transition-colors ${
                  (drag?.mode === "move" && drag.targetTrackId === track.trackId) ||
                  isMediaDropTarget
                    ? "bg-sky-100/70 dark:bg-sky-950/40"
                    : ""
                }`}
                style={{
                  minWidth: laneMinWidth,
                  backgroundImage: `linear-gradient(to right, color-mix(in oklab, var(--border) 30%, transparent) 1px, transparent 1px), linear-gradient(to right, color-mix(in oklab, var(--border) 14%, transparent) 1px, transparent 1px)`,
                  backgroundSize: `${majorGridWidth}px 100%, ${minorGridWidth}px 100%`,
                }}
                onPointerDown={startLaneGesture}
                onDragOver={(event) => {
                  const kind = transitionDragKind(event.dataTransfer.types);
                  if (kind) {
                    event.preventDefault();
                    setTransitionDrag(kind);
                    return;
                  }
                  if (!track.locked) {
                    event.preventDefault();
                    updateMediaDropPreview(event, track);
                  }
                }}
                onDragLeave={(event) => {
                  if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
                    setMediaDropPreview(null);
                  }
                }}
                onDrop={(event) => handleDrop(event, track)}
              >
                {isMediaDropTarget && mediaDropPreview && (
                  <div
                    className="pointer-events-none absolute left-2 top-1 z-30 border border-sky-300 bg-sky-50/90 px-1.5 py-0.5 text-[10px] text-sky-800"
                    data-testid="media-drop-track-preview"
                  >
                    将放入{" "}
                    {mediaDropPreview.plan.createdTrack
                      ? `${mediaDropPreview.plan.track.kind === "video" ? "V" : "A"}${
                          displayTracks.filter(
                            (candidate) =>
                              candidate.kind === mediaDropPreview.plan.track.kind,
                          ).length + 1
                        }（自动创建）`
                      : trackDisplayCode(mediaDropPreview.plan.track, tracks)}
                  </div>
                )}
                {track.kind === "audio" &&
                  track.ducking &&
                  sidechainIntervals(localDoc!, track.ducking).map((interval) => (
                    <div
                      key={`${interval.start}-${interval.end}`}
                      data-testid={`duck-envelope-${track.trackId}-${interval.start}`}
                      className="pointer-events-none absolute bottom-1 z-20 border-t border-dashed border-amber-300 bg-amber-300/10"
                      style={{
                        left:
                          Math.max(0, interval.start - track.ducking!.attackFrames) *
                          pxPerFrame,
                        width:
                          (interval.end -
                            interval.start +
                            track.ducking!.attackFrames +
                            track.ducking!.releaseFrames) *
                          pxPerFrame,
                        height: `${Math.max(10, (1 - track.ducking!.gain) * 70)}%`,
                      }}
                      title={`自动闪避 ${track.ducking!.gain.toFixed(2)}`}
                    />
                  ))}
                {track.kind === "video" &&
                  localDoc &&
                  transitionBoundaries(localDoc, track.trackId).map(
                    ({ from, to, transition }) => (
                      <div key={`${from.itemId}:${to.itemId}`}>
                        {transition && (
                          <div
                            data-testid={`transition-region-${transition.transitionId}`}
                            className="pointer-events-none absolute inset-y-1 z-20 rounded-[4px] border border-violet-400/70 bg-violet-500/20 px-1 text-[9px] text-violet-900 dark:text-violet-100"
                            style={{
                              left: to.from * pxPerFrame,
                              width: Math.max(2, transition.frames * pxPerFrame),
                            }}
                            title={`${transition.kind} ${transition.frames} 帧`}
                          >
                            {transition.kind === "crossfade" ? "叠化" : "黑场"}{" "}
                            {transition.frames}f
                          </div>
                        )}
                        <TransitionDropTarget
                          doc={localDoc}
                          track={track}
                          from={from}
                          to={to}
                          existing={transition}
                          pxPerFrame={pxPerFrame}
                          dragKind={transitionDrag}
                          onDrop={(event, boundaryFrom, boundaryTo, current) =>
                            void handleTransitionDrop(
                              event,
                              boundaryFrom,
                              boundaryTo,
                              current,
                            )
                          }
                          onBlocked={(reason) => {
                            showError(reason);
                            toast.error(`转场操作失败：${reason}`);
                          }}
                          onRemove={(current) => {
                            if (!project) return;
                            void submitOps(project.projectId, [
                              {
                                op: "removeTransition",
                                transitionId: current.transitionId,
                              },
                            ]).then((message) => {
                              if (message) {
                                showError(message);
                                toast.error(`转场删除失败：${message}`);
                              } else {
                                toast.success("转场已删除");
                              }
                            });
                          }}
                        />
                      </div>
                    ),
                  )}
                {items
                  .filter((item) => {
                    if (drag?.itemId !== item.itemId) return item.trackId === track.trackId;
                    return drag.targetTrackId === track.trackId;
                  })
                  .map((item) => {
                    const geometry = clipGeometry(item);
                    const dragging = drag?.itemId === item.itemId;
                    const selected = selectedItemIds.includes(item.itemId);
                    const trimTargetFrame =
                      dragging && drag.mode === "trimStart"
                        ? item.from + drag.deltaFrames
                        : dragging && drag.mode === "trimEnd"
                          ? itemEnd(item) + drag.deltaFrames
                          : null;
                    const agentTouched = agentHighlightItemIds.includes(item.itemId);
                    const media = item.mediaId
                      ? localDoc?.media.find((candidate) => candidate.mediaId === item.mediaId)
                      : undefined;
                    let displayedItem =
                      fadePreview?.itemId === item.itemId
                        ? { ...item, [fadePreview.edge]: fadePreview.frames }
                        : item;
                    if (dragging && drag.mode === "trimStart") {
                      displayedItem = {
                        ...displayedItem,
                        from: item.from + drag.deltaFrames,
                        durationInFrames:
                          item.durationInFrames - drag.deltaFrames,
                        trimStart:
                          (item.trimStart ?? 0) +
                          Math.round(drag.deltaFrames * effectiveSpeed(item)),
                      };
                    } else if (dragging && drag.mode === "trimEnd") {
                      displayedItem = {
                        ...displayedItem,
                        durationInFrames:
                          item.durationInFrames + drag.deltaFrames,
                      };
                    }
                    const itemCanSplit =
                      !track.locked &&
                      splitFrame > item.from &&
                      splitFrame < itemEnd(item);
                    return (
                      <ContextMenu key={item.itemId}>
                        <ContextMenuTrigger asChild>
                          {item.kind === "text" ? (
                            <CaptionClip
                              item={item}
                              left={geometry.left}
                              width={geometry.width}
                              selected={selected}
                              muted={!!track.muted}
                              locked={!!track.locked}
                              onOpenEditor={() => {
                                setSelectedItemIds([item.itemId]);
                                setCaptionEditor({ itemId: item.itemId });
                              }}
                              onKeyDown={(event) => {
                                if (event.key !== "Enter" && event.key !== " ") return;
                                event.preventDefault();
                                event.stopPropagation();
                                setSelectedItemIds([item.itemId]);
                              }}
                              onContextMenu={(event) =>
                                handleClipContextMenu(event, item)
                              }
                              onPointerDown={(event) => {
                                if (event.button !== 0) return;
                                event.preventDefault();
                                event.stopPropagation();
                                setSelectedItemIds((selection) =>
                                  updateItemSelection(
                                    selection,
                                    item.itemId,
                                    event.metaKey ||
                                      event.ctrlKey ||
                                      event.shiftKey,
                                  ),
                                );
                              }}
                            />
                          ) : (
                            <TimelineClip
                              item={displayedItem}
                              media={media}
                              trackKind={track.kind}
                              trackLocked={!!track.locked}
                              trackMuted={!!track.muted}
                              editMode={editMode}
                              left={geometry.left}
                              width={geometry.width}
                              pxPerFrame={pxPerFrame}
                              selected={selected}
                              dragging={dragging}
                              agentTouched={agentTouched}
                              trimTargetFrame={trimTargetFrame}
                              fps={fps}
                              fadePreviewEdge={
                                fadePreview?.itemId === item.itemId
                                  ? fadePreview.edge
                                  : undefined
                              }
                              fadeInDisabledReason={
                                localDoc
                                  ? fadeDisabledReason(localDoc, item, "fadeInFrames")
                                  : null
                              }
                              fadeOutDisabledReason={
                                localDoc
                                  ? fadeDisabledReason(localDoc, item, "fadeOutFrames")
                                  : null
                              }
                              onKeyDown={(event) => {
                                if (event.key !== "Enter" && event.key !== " ") return;
                                event.preventDefault();
                                event.stopPropagation();
                                setSelectedItemIds((selection) =>
                                  updateItemSelection(
                                    selection,
                                    item.itemId,
                                    event.metaKey ||
                                      event.ctrlKey ||
                                      event.shiftKey,
                                  ),
                                );
                              }}
                              onContextMenu={(event) =>
                                handleClipContextMenu(event, item)
                              }
                              onPointerDown={(event) =>
                                handleClipPointerDown(event, item, !!track.locked)
                              }
                              onTrimPointerDown={(event, mode) =>
                                startDrag(event, item, mode, !!track.locked)
                              }
                              onFadePointerDown={(event, edge) =>
                                startFadeDrag(event, item, edge, !!track.locked)
                              }
                            />
                          )}
                        </ContextMenuTrigger>
                        <ContextMenuContent>
                          {item.kind === "text" ? (
                            <>
                              <ContextMenuItem
                                onSelect={() =>
                                  setCaptionEditor({ itemId: item.itemId })
                                }
                              >
                                <Pencil />
                                编辑字幕
                              </ContextMenuItem>
                              <ContextMenuItem onSelect={() => copySelection(item.itemId)}>
                                <Copy />
                                复制
                                <ContextMenuShortcut>⌘/Ctrl+C</ContextMenuShortcut>
                              </ContextMenuItem>
                              <ContextMenuSeparator />
                              <ContextMenuItem
                                variant="destructive"
                                disabled={!!track.locked}
                                onSelect={() => {
                                  const selection =
                                    useEditorContextStore.getState().selectedItemIds;
                                  const operation = buildSelectionDeleteOperation(
                                    selection.includes(item.itemId)
                                      ? selection
                                      : [item.itemId],
                                  );
                                  if (operation) submitOne(operation);
                                }}
                              >
                                <Trash2 />
                                删除
                              </ContextMenuItem>
                            </>
                          ) : (
                            <>
                              <ContextMenuItem
                                disabled={!itemCanSplit}
                                onSelect={() =>
                                  submitOne({
                                    op: "split",
                                    itemId: item.itemId,
                                    frame: splitFrame,
                                  })
                                }
                              >
                                <Scissors />
                                分割
                              </ContextMenuItem>
                              <ContextMenuItem onSelect={() => copySelection(item.itemId)}>
                                <Copy />
                                复制
                                <ContextMenuShortcut>⌘/Ctrl+C</ContextMenuShortcut>
                              </ContextMenuItem>
                              <ContextMenuItem
                                disabled={!clipboard}
                                onSelect={() => pasteItems(clipboard, currentFrame)}
                              >
                                <ClipboardPaste />
                                粘贴
                                <ContextMenuShortcut>⌘/Ctrl+V</ContextMenuShortcut>
                              </ContextMenuItem>
                              <ContextMenuItem
                                variant="destructive"
                                disabled={!!track.locked}
                                onSelect={() => {
                                  const selection =
                                    useEditorContextStore.getState().selectedItemIds;
                                  const operation = buildSelectionDeleteOperation(
                                    selection.includes(item.itemId)
                                      ? selection
                                      : [item.itemId],
                                  );
                                  if (operation) submitOne(operation);
                                }}
                              >
                                <Trash2 />
                                删除
                              </ContextMenuItem>
                              <ContextMenuSeparator />
                              <ContextMenuItem
                                disabled={!item.mediaId}
                                onSelect={() =>
                                  item.mediaId && requestMediaReveal(item.mediaId)
                                }
                              >
                                <Search />
                                在素材中查找
                              </ContextMenuItem>
                              <ContextMenuItem
                                onSelect={() =>
                                  requestPreciseAgentAdjustment(item)
                                }
                              >
                                <Pencil />
                                让 Agent 调整精确参数…
                              </ContextMenuItem>
                              {(item.kind === "video" || item.kind === "image") && (
                                <ContextMenuItem
                                  disabled={!!track.locked}
                                  onSelect={() => enterCropEditing(item)}
                                >
                                  <Crop />
                                  进入画面裁剪
                                </ContextMenuItem>
                              )}
                              {item.kind === "video" && (
                                <ContextMenuItem
                                  disabled={
                                    !!track.locked ||
                                    media?.probe?.hasAudio !== true ||
                                    audioSeparation[item.itemId]?.phase === "loading"
                                  }
                                  title={
                                    track.locked
                                      ? "轨道已锁定"
                                      : media?.probe?.hasAudio !== true
                                        ? "该视频没有可分离的音轨"
                                        : audioSeparation[item.itemId]?.message
                                  }
                                  onSelect={() => startAudioSeparation(item)}
                                >
                                  {audioSeparation[item.itemId]?.phase === "loading" ? (
                                    <LoaderCircle className="animate-spin" />
                                  ) : (
                                    <Unlink />
                                  )}
                                  {audioSeparation[item.itemId]?.phase === "loading"
                                    ? "正在分离音频…"
                                    : audioSeparation[item.itemId]?.phase === "success"
                                      ? "音频已分离"
                                      : "分离音频"}
                                </ContextMenuItem>
                              )}
                            </>
                          )}
                        </ContextMenuContent>
                      </ContextMenu>
                    );
                  })}
              </div>
            </div>
            );
          })}
          {marquee && (
            <div
              data-testid="timeline-marquee"
              className="pointer-events-none absolute z-40 border border-blue-400 bg-blue-400/15"
              style={marquee}
            />
          )}
          </div>
          {localDoc && (
            <>
              <div
                data-testid="timeline-playhead"
                className="pointer-events-none absolute top-0 z-50 w-0"
                style={{
                  left: TRACK_HEADER_PX + currentFrame * pxPerFrame,
                  height: RULER_HEIGHT_PX + trackVerticalLayout.contentHeight,
                }}
              >
                <span className="absolute left-0 top-0 -translate-x-1/2 border-x-[5px] border-t-[7px] border-x-transparent border-t-slate-900 dark:border-t-slate-100" />
                <span className="absolute bottom-0 left-0 top-1.5 w-px bg-slate-900/85 dark:bg-slate-100/85" />
              </div>
              {drag?.snapTarget !== null && drag?.snapTarget !== undefined && (
                <div
                  className="pointer-events-none absolute bottom-0 top-8 z-20 w-px bg-sky-500/80"
                  style={{ left: TRACK_HEADER_PX + drag.snapTarget * pxPerFrame }}
                />
              )}
            </>
          )}
        </div>
      </div>

      {captionEditor &&
        localDoc &&
        (!captionEditor.itemId || captionEditorItem?.kind === "text") && (
          <CaptionPanel
            doc={localDoc}
            item={captionEditorItem}
            newCaptionTrackId={captionEditor.newCaptionTrackId}
            initialFrame={currentFrame}
            onClose={() => setCaptionEditor(null)}
            onSubmit={submitMany}
          />
        )}
    </div>
  );
}
