import { Captions, ChevronDown, Loader2, Search, Trash2, TriangleAlert } from "lucide-react";
import {
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  activeTranscriptOccurrence,
  estimatedSelectedFrames,
  occurrenceRange,
  selectedTranscriptRanges,
} from "@/domain/transcriptView";
import { buildTranscriptCaptionOps } from "@/domain/transcriptCaptions";
import type { Media } from "@/domain/timeline/types";
import type { TranscriptEntry } from "@/remote";
import { usePlaybackStore } from "@/store/playbackStore";
import { useProjectStore } from "@/store/projectStore";
import { submitOps, submitTranscriptDelete } from "@/store/submitOps";
import {
  refreshTranscriptView,
  useTranscriptStore,
} from "@/store/transcriptStore";
import { useTimelineStore } from "@/store/timelineStore";

const INITIAL_RENDER_LIMIT = 300;
const RENDER_CHUNK = 300;
const MANUAL_FOLLOW_PAUSE_MS = 3000;

function mediaName(media: Media | undefined): string {
  if (!media) return "未知素材";
  if (media.displayName?.trim()) return media.displayName.trim();
  const managedName = media.blobKey?.split("/").pop();
  if (managedName) return managedName;
  try {
    const name = new URL(media.uri, "http://agentcut.local").pathname.split("/").pop();
    if (name && !name.startsWith("med_") && !name.startsWith("media_")) return name;
  } catch {
    // 非标准 URI 使用类型名称。
  }
  return { video: "未命名视频", audio: "未命名音频", image: "未命名图片" }[media.kind];
}

function formatDuration(frames: number, fps: number): string {
  return `${(frames / Math.max(1, fps)).toFixed(2)} 秒`;
}

function HighlightedText({ text, query }: { text: string; query: string }) {
  const normalized = query.trim();
  if (!normalized) return text;
  const start = text.toLocaleLowerCase().indexOf(normalized.toLocaleLowerCase());
  if (start < 0) return text;
  return (
    <>
      {text.slice(0, start)}
      <mark className="rounded-sm bg-amber-300/60 text-inherit">
        {text.slice(start, start + normalized.length)}
      </mark>
      {text.slice(start + normalized.length)}
    </>
  );
}

export function TranscriptPanel() {
  const project = useProjectStore((state) => state.project);
  const projectLoading = useProjectStore((state) => state.loading);
  const timelineRevision = useTimelineStore((state) => state.confirmedRevision);
  const localDoc = useTimelineStore((state) => state.localDoc);
  const pendingOps = useTimelineStore((state) => state.pendingOps);
  const currentFrame = usePlaybackStore((state) => state.currentFrame);
  const setCurrentFrame = usePlaybackStore((state) => state.setCurrentFrame);
  const transcript = useTranscriptStore();

  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query.trim().toLocaleLowerCase());
  const [selectedMediaId, setSelectedMediaId] = useState<string | null>(null);
  const [renderLimit, setRenderLimit] = useState(INITIAL_RENDER_LIMIT);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [captionSubmitting, setCaptionSubmitting] = useState(false);
  const draggingRef = useRef(false);
  const dragAnchorRef = useRef<string | null>(null);
  const manualFollowUntilRef = useRef(0);
  const entryRefs = useRef(new Map<string, HTMLButtonElement>());

  const projectId = project?.projectId ?? null;
  const entries = transcript.entries;
  const selected = transcript.selectedOccurrenceIds;
  const clearTranscriptSelection = transcript.clearSelection;
  const videoMedia = useMemo(
    () => (localDoc?.media ?? []).filter((media) => media.kind === "video"),
    [localDoc?.media],
  );
  const videoMediaIds = useMemo(
    () => new Set(videoMedia.map((media) => media.mediaId)),
    [videoMedia],
  );
  const videoEntries = useMemo(
    () => entries.filter((entry) => videoMediaIds.has(entry.mediaId)),
    [entries, videoMediaIds],
  );
  const scopedEntries = useMemo(
    () =>
      selectedMediaId
        ? videoEntries.filter((entry) => entry.mediaId === selectedMediaId)
        : [],
    [selectedMediaId, videoEntries],
  );
  const activeOccurrenceId = useMemo(
    () => activeTranscriptOccurrence(scopedEntries, currentFrame),
    [currentFrame, scopedEntries],
  );
  const filteredEntries = useMemo(
    () =>
      deferredQuery
        ? scopedEntries.filter((entry) =>
            entry.text.toLocaleLowerCase().includes(deferredQuery),
          )
        : scopedEntries,
    [deferredQuery, scopedEntries],
  );
  const renderedEntries = filteredEntries.slice(0, renderLimit);
  const estimatedFrames = useMemo(
    () => estimatedSelectedFrames(scopedEntries, selected),
    [scopedEntries, selected],
  );
  const selectedRanges = useMemo(
    () => selectedTranscriptRanges(scopedEntries, selected),
    [scopedEntries, selected],
  );
  const failedSave = pendingOps.some((batch) => batch.status === "failed");
  const processingMedia = localDoc?.media.filter(
    (media) =>
      media.kind === "video" &&
      media.mediaId === selectedMediaId &&
      (media.processing?.transcript === "pending" ||
        media.processing?.transcript === "running"),
  );
  const failedMedia = localDoc?.media.filter(
    (media) =>
      media.kind === "video" &&
      media.mediaId === selectedMediaId &&
      media.processing?.transcript === "failed",
  );

  useEffect(() => {
    if (!projectId || projectLoading) return;
    if (
      transcript.projectId !== projectId ||
      transcript.timelineRevision !== timelineRevision ||
      transcript.status === "idle"
    ) {
      void refreshTranscriptView(projectId, timelineRevision);
    }
  }, [
    projectId,
    projectLoading,
    timelineRevision,
    transcript.projectId,
    transcript.status,
    transcript.timelineRevision,
  ]);

  useEffect(() => {
    setRenderLimit(INITIAL_RENDER_LIMIT);
    setConfirmingDelete(false);
    setActionError(null);
  }, [deferredQuery, projectId, selectedMediaId, transcript.timelineRevision]);

  useEffect(() => {
    if (selectedMediaId && videoMedia.some((media) => media.mediaId === selectedMediaId)) {
      return;
    }
    setSelectedMediaId(videoMedia[0]?.mediaId ?? null);
  }, [selectedMediaId, videoMedia]);

  useEffect(() => {
    clearTranscriptSelection();
  }, [clearTranscriptSelection, selectedMediaId]);

  useEffect(() => {
    const finishDrag = () => {
      draggingRef.current = false;
      dragAnchorRef.current = null;
    };
    window.addEventListener("pointerup", finishDrag);
    return () => window.removeEventListener("pointerup", finishDrag);
  }, []);

  useEffect(() => {
    if (!activeOccurrenceId) return;
    const filteredIndex = filteredEntries.findIndex(
      (entry) => entry.occurrenceId === activeOccurrenceId,
    );
    if (filteredIndex >= renderLimit) {
      setRenderLimit(
        Math.ceil((filteredIndex + 1) / RENDER_CHUNK) * RENDER_CHUNK,
      );
      return;
    }
    if (Date.now() < manualFollowUntilRef.current) return;
    entryRefs.current
      .get(activeOccurrenceId)
      ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [activeOccurrenceId, filteredEntries, renderLimit]);

  const mediaById = useMemo(
    () => new Map((localDoc?.media ?? []).map((media) => [media.mediaId, media])),
    [localDoc?.media],
  );
  const instanceLabels = useMemo(() => {
    const result = new Map<string, number>();
    const counts = new Map<string, number>();
    for (const entry of scopedEntries) {
      if (result.has(entry.itemId)) continue;
      const next = (counts.get(entry.mediaId) ?? 0) + 1;
      counts.set(entry.mediaId, next);
      result.set(entry.itemId, next);
    }
    return result;
  }, [scopedEntries]);

  function setRangeSelection(fromId: string, toId: string) {
    transcript.setSelection(occurrenceRange(scopedEntries, fromId, toId), fromId);
  }

  function handlePointerSelection(
    entry: TranscriptEntry,
    event: React.PointerEvent<HTMLButtonElement>,
  ) {
    if (event.button !== 0) return;
    setConfirmingDelete(false);
    if (event.shiftKey && transcript.anchorOccurrenceId) {
      setRangeSelection(transcript.anchorOccurrenceId, entry.occurrenceId);
      draggingRef.current = true;
      dragAnchorRef.current = transcript.anchorOccurrenceId;
      return;
    }
    if (event.metaKey || event.ctrlKey) {
      const next = new Set(selected);
      if (next.has(entry.occurrenceId)) next.delete(entry.occurrenceId);
      else next.add(entry.occurrenceId);
      transcript.setSelection(next, entry.occurrenceId);
      return;
    }
    transcript.setSelection([entry.occurrenceId], entry.occurrenceId);
    draggingRef.current = true;
    dragAnchorRef.current = entry.occurrenceId;
  }

  function handlePointerEnter(entry: TranscriptEntry, event: React.PointerEvent) {
    if (!draggingRef.current || event.buttons !== 1 || !dragAnchorRef.current) return;
    setRangeSelection(dragAnchorRef.current, entry.occurrenceId);
  }

  function handleEntryKeyDown(
    entry: TranscriptEntry,
    event: React.KeyboardEvent<HTMLButtonElement>,
  ) {
    const visibleIndex = filteredEntries.findIndex(
      (candidate) => candidate.occurrenceId === entry.occurrenceId,
    );
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      const next = filteredEntries[visibleIndex + direction];
      if (!next) return;
      if (event.shiftKey) {
        setRangeSelection(
          transcript.anchorOccurrenceId ?? entry.occurrenceId,
          next.occurrenceId,
        );
      } else {
        transcript.setSelection([next.occurrenceId], next.occurrenceId);
      }
      entryRefs.current.get(next.occurrenceId)?.focus();
      return;
    }
    if (event.key === " ") {
      event.preventDefault();
      if (event.metaKey || event.ctrlKey) {
        const next = new Set(selected);
        if (next.has(entry.occurrenceId)) next.delete(entry.occurrenceId);
        else next.add(entry.occurrenceId);
        transcript.setSelection(next, entry.occurrenceId);
      } else {
        transcript.setSelection([entry.occurrenceId], entry.occurrenceId);
      }
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      setCurrentFrame(entry.fromFrame);
    }
    if ((event.key === "Delete" || event.key === "Backspace") && selected.size > 0) {
      event.preventDefault();
      setConfirmingDelete(true);
    }
  }

  async function confirmDelete() {
    if (!projectId || selectedRanges.length === 0) return;
    setActionError(null);
    const message = await submitTranscriptDelete(projectId, selectedRanges);
    if (!message) {
      setConfirmingDelete(false);
      return;
    }
    const understandable = message.toLocaleLowerCase().includes("locked")
      ? "删除范围会影响锁定轨道，请先解锁相关轨道后重试"
      : message.includes("未应用，请重试")
        ? "时间线已变化，旧文字选择已失效；已刷新，请重新选择"
        : message;
    setActionError(understandable);
  }

  async function generateCaptions() {
    if (!projectId || !localDoc || !selectedMediaId) return;
    const operations = localDoc.media
      .filter((media) => media.mediaId === selectedMediaId && media.kind === "video")
      .flatMap((media) => buildTranscriptCaptionOps(localDoc, media));
    if (operations.length === 0) {
      setActionError("没有可用于字幕的有效时间戳，请等待转写完成或检查素材");
      return;
    }
    setCaptionSubmitting(true);
    setActionError(null);
    const message = await submitOps(projectId, operations);
    if (message) setActionError(`生成字幕失败：${message}`);
    setCaptionSubmitting(false);
  }

  function pauseAutoFollow() {
    manualFollowUntilRef.current = Date.now() + MANUAL_FOLLOW_PAUSE_MS;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-2 border-b p-2">
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-xs font-medium">视频文字稿</span>
          <select
            aria-label="选择视频文字稿素材"
            className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
            value={selectedMediaId ?? ""}
            disabled={videoMedia.length === 0}
            onChange={(event) => setSelectedMediaId(event.target.value || null)}
          >
            {videoMedia.length === 0 ? (
              <option value="">暂无视频素材</option>
            ) : (
              videoMedia.map((media) => (
                <option key={media.mediaId} value={media.mediaId}>
                  {mediaName(media)}
                </option>
              ))
            )}
          </select>
          <Button
            variant="outline"
            size="icon-sm"
            aria-label="生成或更新自动字幕"
            title="根据当前视频文字稿生成或更新自动字幕"
            disabled={
              scopedEntries.length === 0 ||
              captionSubmitting ||
              failedSave ||
              projectLoading
            }
            onClick={() => void generateCaptions()}
          >
            {captionSubmitting ? (
              <Loader2 data-slot="icon" className="animate-spin" />
            ) : (
              <Captions data-slot="icon" />
            )}
          </Button>
        </div>
        <div className="relative min-w-0">
          <Search className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            aria-label="搜索文字稿"
            className="h-8 pl-7 text-xs"
            placeholder="搜索文字稿"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
      </div>

      {(actionError || transcript.error) && (
        <div
          className="border-b bg-destructive/10 px-3 py-2 text-xs text-destructive"
          role="alert"
        >
          {actionError ?? transcript.error}
        </div>
      )}
      {transcript.warnings.length > 0 && (
        <div className="border-b bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
          <TriangleAlert className="mr-1 inline size-3.5" />
          {transcript.warnings.join("；")}
        </div>
      )}

      {selected.size > 0 && (
        <div className="flex items-center gap-2 border-b bg-muted/40 px-2 py-2 text-xs">
          <span className="min-w-0 flex-1">
            已选 {selected.size} 项，预计删除 {formatDuration(estimatedFrames, localDoc?.metadata.fps ?? 30)}
          </span>
          <Button
            size="xs"
            variant="destructive"
            disabled={failedSave || projectLoading}
            onClick={() => setConfirmingDelete(true)}
          >
            <Trash2 data-slot="icon" />
            删字剪视频
          </Button>
        </div>
      )}

      {confirmingDelete && selected.size > 0 && (
        <div className="border-b bg-destructive/5 p-3 text-xs" role="alertdialog">
          <p>
            将删除 {selected.size} 个文字单位，预计 {formatDuration(estimatedFrames, localDoc?.metadata.fps ?? 30)}
            。操作采用全局 ripple，所有未锁定轨道将同步收拢。
          </p>
          <div className="mt-2 flex justify-end gap-2">
            <Button size="xs" variant="outline" onClick={() => setConfirmingDelete(false)}>
              取消
            </Button>
            <Button
              size="xs"
              variant="destructive"
              disabled={projectLoading}
              onClick={() => void confirmDelete()}
            >
              确认删除
            </Button>
          </div>
        </div>
      )}

      <div
        className="min-h-0 flex-1 overflow-y-auto"
        onWheel={pauseAutoFollow}
        onTouchMove={pauseAutoFollow}
        onPointerDown={(event) => {
          if (event.target === event.currentTarget) pauseAutoFollow();
        }}
      >
        {transcript.status === "loading" ? (
          <div className="flex items-center justify-center gap-2 p-6 text-xs text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            正在加载项目文字稿…
          </div>
        ) : transcript.status === "error" ? (
          <div className="p-5 text-center text-xs text-muted-foreground">
            <p>文字稿加载失败</p>
            <Button
              className="mt-2"
              size="xs"
              variant="outline"
              disabled={!projectId}
              onClick={() => {
                if (projectId) void refreshTranscriptView(projectId, timelineRevision, { force: true });
              }}
            >
              重试
            </Button>
          </div>
        ) : videoMedia.length === 0 ? (
          <div className="p-5 text-center text-xs text-muted-foreground">
            文字稿编辑仅显示视频素材；当前项目还没有视频。
          </div>
        ) : scopedEntries.length === 0 && (processingMedia?.length ?? 0) > 0 ? (
          <div className="p-5 text-center text-xs text-muted-foreground">
            <Loader2 className="mx-auto mb-2 size-4 animate-spin" />
            正在处理 {processingMedia?.length} 个素材的文字稿
          </div>
        ) : scopedEntries.length === 0 && (failedMedia?.length ?? 0) > 0 ? (
          <div className="p-5 text-center text-xs text-destructive">
            文字稿处理失败：{failedMedia?.[0]?.processing?.error ?? "请在素材菜单中重新解析"}
          </div>
        ) : scopedEntries.length === 0 ? (
          <div className="p-5 text-center text-xs text-muted-foreground">
            当前视频在时间线上没有可见文字稿
          </div>
        ) : filteredEntries.length === 0 ? (
          <div className="p-5 text-center text-xs text-muted-foreground">
            没有匹配“{query}”的文字
          </div>
        ) : (
          <div
            role="listbox"
            aria-label="当前视频文字稿"
            aria-multiselectable="true"
            className="p-2"
          >
            {renderedEntries.map((entry, index) => {
              const previous = renderedEntries[index - 1];
              const startsItem = !previous || previous.itemId !== entry.itemId;
              const isSelected = selected.has(entry.occurrenceId);
              const isActive = activeOccurrenceId === entry.occurrenceId;
              return (
                <div key={entry.occurrenceId}>
                  {startsItem && (
                    <div className="mb-1 mt-2 flex items-center gap-1.5 border-t pt-2 text-[11px] text-muted-foreground first:mt-0 first:border-t-0 first:pt-0">
                      <span className="truncate font-medium text-foreground">
                        {mediaName(mediaById.get(entry.mediaId))}
                      </span>
                      <span>实例 {instanceLabels.get(entry.itemId) ?? 1}</span>
                      <span>·</span>
                      <span>轨道 {entry.trackId}</span>
                    </div>
                  )}
                  <button
                    ref={(node) => {
                      if (node) entryRefs.current.set(entry.occurrenceId, node);
                      else entryRefs.current.delete(entry.occurrenceId);
                    }}
                    type="button"
                    role="option"
                    aria-selected={isSelected}
                    data-occurrence-id={entry.occurrenceId}
                    className={`my-0.5 inline rounded px-1 py-0.5 text-left text-sm leading-6 outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring ${
                      isSelected
                        ? "bg-primary/20 text-foreground"
                        : isActive
                          ? "bg-emerald-500/20 text-foreground"
                          : "hover:bg-accent"
                    }`}
                    onPointerDown={(event) => handlePointerSelection(entry, event)}
                    onPointerEnter={(event) => handlePointerEnter(entry, event)}
                    onClick={() => setCurrentFrame(entry.fromFrame)}
                    onKeyDown={(event) => handleEntryKeyDown(entry, event)}
                  >
                    <HighlightedText text={entry.text} query={query} />
                    {entry.granularity === "segment" && (
                      <Badge
                        variant="outline"
                        className="ml-1 h-4 px-1 text-[9px] align-middle"
                        title="该素材缺少词级时间戳，整段作为选择单位"
                      >
                        段落级
                      </Badge>
                    )}
                  </button>
                </div>
              );
            })}
            {filteredEntries.length > renderedEntries.length && (
              <Button
                className="mt-2 w-full"
                variant="ghost"
                size="xs"
                onClick={() => setRenderLimit((limit) => limit + RENDER_CHUNK)}
              >
                <ChevronDown data-slot="icon" />
                加载更多（剩余 {filteredEntries.length - renderedEntries.length}）
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
