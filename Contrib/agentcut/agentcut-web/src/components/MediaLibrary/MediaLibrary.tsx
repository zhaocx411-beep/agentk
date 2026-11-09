// 中栏素材/项目文字稿面板。
// 本阶段接通：真实上传（multipart POST /uploads）+ 列表读 store 的
// localDoc.media（乐观视图）并支持按文件名/mediaId/类型实时过滤。
import {
  Brain,
  Captions,
  CheckCircle2,
  CircleAlert,
  Download,
  FileText,
  Film,
  Image as ImageIcon,
  ListPlus,
  Loader2,
  MoreVertical,
  Music,
  Pencil,
  RefreshCw,
  Search,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TranscriptPanel } from "@/components/Transcript/TranscriptPanel";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  isMediaReadyForTimeline,
  mediaListNeedsProcessingPoll,
  mediaMenuAvailability,
  processingView,
  startMediaProcessingPoller,
} from "@/domain/mediaProcessing";
import { buildDeleteMediaOps } from "@/domain/timeline/deleteMedia";
import { buildAppendMediaOps } from "@/domain/timeline/insertMedia";
import { buildTranscriptCaptionOps } from "@/domain/transcriptCaptions";
import type { Media } from "@/domain/timeline/types";
import { THUMBNAIL_FRAME_COUNT } from "@/domain/timeline/types";
import {
  MEDIA_DND_TYPE,
  TRANSITION_DND_KIND_TYPE,
  TRANSITION_DND_TYPE,
} from "@/lib/dnd";
import { formatFrames, mediaKindOfFile, probeDurationBestEffort } from "@/lib/media";
import { ApiError, api } from "@/remote";
import { useEditorContextStore } from "@/store/editorContextStore";
import { usePlaybackStore } from "@/store/playbackStore";
import { useProjectStore } from "@/store/projectStore";
import { submitOps } from "@/store/submitOps";
import { useTimelineStore } from "@/store/timelineStore";
import {
  invalidateTranscriptView,
  refreshTranscriptView,
  useTranscriptStore,
} from "@/store/transcriptStore";

const KIND_ICON = { video: Film, audio: Music, image: ImageIcon } as const;
const TRANSITION_RESOURCES = [
  {
    kind: "crossfade",
    name: "交叉叠化",
    description: "在相邻画面间平滑混合",
  },
  {
    kind: "dipToBlack",
    name: "黑场转场",
    description: "淡入黑场后切换到下一画面",
  },
  {
    kind: "none",
    name: "无转场",
    description: "拖到已有转场的边界以移除",
  },
] as const;

/** 素材显示名：displayName（重命名写入）优先；否则取受管对象/URI 文件名，
 *  最后只显示类别名，避免把内部 mediaId 暴露给用户。 */
function displayName(
  media: Pick<Media, "blobKey" | "displayName" | "kind" | "uri">,
): string {
  if (media.displayName) return media.displayName;
  if (media.blobKey) {
    const base = media.blobKey.split("/").pop();
    if (base) return base;
  }
  try {
    const base = new URL(media.uri, "http://agentcut.local").pathname.split("/").pop();
    if (base && !base.startsWith("med_") && !base.startsWith("media_")) return base;
  } catch {
    // 非标准 URI（如 mock://）继续使用无内部 ID 的类别名。
  }
  return { video: "未命名视频", audio: "未命名音频", image: "未命名图片" }[media.kind];
}

const STATUS_CLASS = {
  neutral: "border-muted-foreground/30 bg-muted text-muted-foreground",
  success: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  warning: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  danger: "border-destructive/30 bg-destructive/10 text-destructive",
} as const;

function WaveformPreview({ peaks }: { peaks: number[] }) {
  const bars = Array.from({ length: Math.min(32, peaks.length) }, (_, index) => {
    const sourceIndex = Math.min(
      peaks.length - 1,
      Math.floor((index * peaks.length) / Math.min(32, peaks.length)),
    );
    return Math.max(0, Math.min(1, peaks[sourceIndex] ?? 0));
  });
  return (
    <div
      className="flex h-7 w-full items-center gap-px overflow-hidden px-1"
      aria-label="真实音频波形"
      title="素材真实音频波形"
    >
      {bars.map((peak, index) => (
        <span
          // 同一素材的波形桶顺序固定，index 是稳定键。
          key={index}
          className="min-w-px flex-1 rounded-full bg-emerald-500/80"
          style={{ height: `${Math.max(2, peak * 24)}px` }}
        />
      ))}
    </div>
  );
}

function formatMs(ms: number): string {
  const totalSeconds = Math.max(0, ms) / 1000;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds - minutes * 60;
  return `${String(minutes).padStart(2, "0")}:${seconds.toFixed(3).padStart(6, "0")}`;
}

function processingSignature(media: Media | undefined): string {
  return JSON.stringify(media?.processing ?? null);
}

export function MediaLibrary() {
  const project = useProjectStore((s) => s.project);
  const localDoc = useTimelineStore((s) => s.localDoc);
  const setSourcePreview = usePlaybackStore((s) => s.setSourcePreview);
  const mediaRevealRequest = useEditorContextStore(
    (state) => state.mediaRevealRequest,
  );

  const fileInputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [activeTab, setActiveTab] = useState<"mine" | "transitions" | "transcript">(
    "mine",
  );
  const [renamingMediaId, setRenamingMediaId] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [openMenuId, setOpenMenuId] = useState<string | null>(null);
  const [retryingMediaId, setRetryingMediaId] = useState<string | null>(null);
  const forcedPollRef = useRef(
    new Map<string, { revision: number; processingSignature: string }>(),
  );
  const [forcedPollVersion, setForcedPollVersion] = useState(0);
  const [detail, setDetail] = useState<{
    kind: "transcript" | "understanding";
    mediaId: string;
  } | null>(null);

  const mediaList = useMemo(() => localDoc?.media ?? [], [localDoc?.media]);
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredMedia = normalizedQuery
    ? mediaList.filter((media) => {
        const kindLabel = { video: "视频 video", audio: "音频 audio", image: "图片 image" }[
          media.kind
        ];
        return [displayName(media), media.mediaId, kindLabel].some((value) =>
          value.toLocaleLowerCase().includes(normalizedQuery),
        );
      })
    : mediaList;
  const fps = localDoc?.metadata.fps ?? 30;
  const pollingNeeded =
    mediaListNeedsProcessingPoll(mediaList) || forcedPollRef.current.size > 0;
  const detailMedia = detail
    ? mediaList.find((media) => media.mediaId === detail.mediaId) ?? null
    : null;
  const projectId = project?.projectId;

  useEffect(() => {
    forcedPollRef.current.clear();
    setForcedPollVersion((version) => version + 1);
  }, [projectId]);

  useEffect(() => {
    if (
      !mediaRevealRequest ||
      !mediaList.some((media) => media.mediaId === mediaRevealRequest.mediaId)
    ) {
      return;
    }
    setActiveTab("mine");
    setQuery("");
    const timer = window.setTimeout(() => {
      const target = Array.from(
        document.querySelectorAll<HTMLElement>("[data-media-library-id]"),
      ).find(
        (element) =>
          element.dataset.mediaLibraryId === mediaRevealRequest.mediaId,
      );
      target?.scrollIntoView?.({ block: "nearest" });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [mediaList, mediaRevealRequest]);

  useEffect(() => {
    if (!projectId || !pollingNeeded) return;
    const controller = new AbortController();
    let active = true;
    const stop = startMediaProcessingPoller({
      getMedia: () => useTimelineStore.getState().localDoc?.media ?? [],
      shouldPoll: () =>
        mediaListNeedsProcessingPoll(useTimelineStore.getState().localDoc?.media ?? []) ||
        forcedPollRef.current.size > 0,
      refresh: async () => {
        try {
          const snapshot = await api.getTimeline(projectId, { signal: controller.signal });
          if (
            !active ||
            controller.signal.aborted ||
            useProjectStore.getState().project?.projectId !== projectId
          ) {
            return;
          }
          const applied = useTimelineStore
            .getState()
            .applyPollingSnapshot(snapshot.doc, snapshot.revision);
          if (applied !== "applied") return;
          let forceChanged = false;
          for (const [mediaId, start] of forcedPollRef.current) {
            const media = snapshot.doc.media.find((candidate) => candidate.mediaId === mediaId);
            if (
              snapshot.revision > start.revision &&
              processingSignature(media) !== start.processingSignature
            ) {
              forcedPollRef.current.delete(mediaId);
              forceChanged = true;
            }
          }
          if (forceChanged) setForcedPollVersion((version) => version + 1);
        } catch {
          // 保留当前可编辑状态；下一轮继续尝试，不用错误条反复打扰用户。
        }
      },
    });
    return () => {
      active = false;
      controller.abort();
      stop();
    };
  }, [forcedPollVersion, pollingNeeded, projectId]);

  function handleDeleteMedia(mediaId: string, name: string) {
    if (!project || !localDoc) return;
    const referencedItemIds = localDoc.items
      .filter((item) => item.mediaId === mediaId)
      .map((item) => item.itemId);
    const detail =
      referencedItemIds.length > 0
        ? `\n该素材正在被 ${referencedItemIds.length} 个时间线片段使用，继续将同时删除这些片段。`
        : "";
    if (!window.confirm(`确定删除素材「${name}」？${detail}\n删除后将回收上传文件，无法通过撤销恢复素材。`)) {
      return;
    }
    setError(null);
    void submitOps(project.projectId, buildDeleteMediaOps(localDoc, mediaId)).then((msg) => {
      if (!msg) return;
      setError(
        msg.includes("referenced")
          ? "素材引用状态已变化，请重试删除"
          : `删除失败：${msg}`,
      );
    });
  }

  /** 添加到时间线：优先追加到默认兼容轨道；没有兼容未锁定轨道时，
   *  在同一批次内自动创建轨道并插入素材。 */
  function handleAddToTimeline(m: Media) {
    if (!project || !localDoc) return;
    if (!isMediaReadyForTimeline(m)) {
      setError("基础探测尚未完成，暂时不能添加到时间线");
      return;
    }
    setError(null);
    void submitOps(project.projectId, buildAppendMediaOps(localDoc, m)).then((msg) => {
      if (msg) setError(`添加到时间线失败：${msg}`);
    });
  }

  /** 下载：走 handleMediaContent 的 ?download=1，两种 Blob 后端都支持
   *  （不能直接用 media.uri——blob 后端下 uri 是外部对象直连地址，不认
   *  这个查询参数）。 */
  function handleDownload(m: Media) {
    if (!project) return;
    const url = `/v1/projects/${project.projectId}/media/${m.mediaId}/content?download=1`;
    const a = document.createElement("a");
    a.href = url;
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  function startRename(m: Media) {
    setRenamingMediaId(m.mediaId);
    setRenameDraft(displayName(m));
  }

  function commitRename() {
    const mediaId = renamingMediaId;
    const draft = renameDraft;
    setRenamingMediaId(null);
    if (!project || !mediaId) return;
    const trimmed = draft.trim();
    if (!trimmed) return;
    void submitOps(project.projectId, [
      { op: "updateMedia", mediaId, mediaPatch: { displayName: trimmed } },
    ]).then((msg) => {
      if (msg) setError(`重命名失败：${msg}`);
    });
  }

  function handleGenerateCaptions(media: Media) {
    if (!project || !localDoc) return;
    if (!localDoc.items.some((item) => item.mediaId === media.mediaId)) {
      setError("请先将素材添加到时间线，再根据片段位置生成字幕");
      return;
    }
    const operations = buildTranscriptCaptionOps(localDoc, media);
    if (operations.length === 0) {
      setError("该素材没有带时间戳的文字稿");
      return;
    }
    setError(null);
    void submitOps(project.projectId, operations).then((message) => {
      if (message) setError(`生成字幕轨失败：${message}`);
    });
  }

  async function handleRetryPreparation(media: Media) {
    if (!project || retryingMediaId) return;
    const requestProjectId = project.projectId;
    setRetryingMediaId(media.mediaId);
    setError(null);
    try {
      await api.prepareMedia(requestProjectId, media.mediaId);
      if (useProjectStore.getState().project?.projectId !== requestProjectId) return;
      const current = useTimelineStore.getState();
      forcedPollRef.current.set(media.mediaId, {
        revision: current.confirmedRevision,
        processingSignature: processingSignature(
          current.localDoc?.media.find((candidate) => candidate.mediaId === media.mediaId),
        ),
      });
      setForcedPollVersion((version) => version + 1);
      const snapshot = await api.getTimeline(requestProjectId);
      if (useProjectStore.getState().project?.projectId !== requestProjectId) return;
      useTimelineStore.getState().applyPollingSnapshot(snapshot.doc, snapshot.revision);
    } catch (err) {
      setError(
        err instanceof ApiError
          ? `重新解析失败：${err.body.error}`
          : err instanceof Error
            ? `重新解析失败：${err.message}`
            : "重新解析失败",
      );
    } finally {
      setRetryingMediaId(null);
    }
  }

  async function handleFile(file: File) {
    if (!project) return;
    const requestProjectId = project.projectId;
    setError(null);

    const kind = mediaKindOfFile(file);
    if (!kind) {
      setError(`不支持的文件类型：${file.type || "未知"}（仅支持视频/音频/图片）`);
      return;
    }

    setUploading(true);
    try {
      let durationMs: number | undefined;
      if (kind === "video" || kind === "audio") {
        // 浏览器可能不支持文件编码；失败时省略提示值，服务端 ffprobe
        // 仍会完成权威探测，客户端能力不能阻断上传。
        durationMs = await probeDurationBestEffort(file, kind);
      }
      const uploaded = await api.upload(requestProjectId, { file, kind, durationMs });
      if (useProjectStore.getState().project?.projectId !== requestProjectId) return;
      // upload 响应已经包含完成基础探测并登记的 Media：先合并到 confirmed
      // 文档立即展示，再全量拉取可能已开始回填的异步阶段。
      const store = useTimelineStore.getState();
      if (store.doc) {
        const doc = structuredClone(store.doc);
        const index = doc.media.findIndex((media) => media.mediaId === uploaded.media.mediaId);
        if (index >= 0) doc.media[index] = uploaded.media;
        else doc.media.push(uploaded.media);
        const applied = store.applyPollingSnapshot(doc, uploaded.revision);
        if (applied === "deferred") {
          forcedPollRef.current.set(uploaded.media.mediaId, {
            revision: store.confirmedRevision,
            processingSignature: processingSignature(
              store.localDoc?.media.find(
                (candidate) => candidate.mediaId === uploaded.media.mediaId,
              ),
            ),
          });
          setForcedPollVersion((version) => version + 1);
        }
      }
      try {
        const snapshot = await api.getTimeline(requestProjectId);
        if (useProjectStore.getState().project?.projectId !== requestProjectId) return;
        useTimelineStore
          .getState()
          .applyPollingSnapshot(snapshot.doc, snapshot.revision);
      } catch {
        setError("素材已上传，后台解析状态将在连接恢复后自动同步");
      }
    } catch (err) {
      if (err instanceof ApiError) {
        setError(`上传失败：${err.body.error}`);
      } else {
        setError(err instanceof Error ? err.message : "上传失败，请重试");
      }
    } finally {
      setUploading(false);
    }
  }

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => {
        const tab = value as "mine" | "transitions" | "transcript";
        setActiveTab(tab);
        if (tab === "transcript" && projectId) {
          const revision = useTimelineStore.getState().confirmedRevision;
          const current = useTranscriptStore.getState();
          if (current.projectId !== projectId || current.timelineRevision !== revision) {
            invalidateTranscriptView(projectId);
            void refreshTranscriptView(projectId, revision);
          }
        }
      }}
      className="flex h-full min-h-0 flex-col gap-0 bg-[#F2F2F0]"
    >
      <div className="flex items-center gap-2 border-b p-2">
        <TabsList className="w-full">
          <TabsTrigger value="mine" className="flex-1 text-xs">
            素材
          </TabsTrigger>
          <TabsTrigger value="transitions" className="flex-1 text-xs">
            转场
          </TabsTrigger>
          <TabsTrigger value="transcript" className="flex-1 text-xs">
            文字稿
          </TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="mine" className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b p-2">
        <div className="relative flex-1">
          <Search className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-7 text-xs"
            placeholder="按文件名或类型搜索"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <input
          ref={fileInputRef}
          type="file"
          accept="video/*,audio/*,image/*"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0];
            // 允许重复选同一个文件：先清空再处理。
            e.target.value = "";
            if (file) void handleFile(file);
          }}
        />
        <Button
          variant="outline"
          size="icon-sm"
          disabled={!project || uploading}
          aria-label={uploading ? "正在上传素材" : "上传素材"}
          title={uploading ? "上传中…" : "上传素材"}
          onClick={() => fileInputRef.current?.click()}
        >
          {uploading ? (
            <Loader2 data-slot="icon" className="animate-spin" />
          ) : (
            <Upload data-slot="icon" />
          )}
        </Button>
      </div>
      {error && (
        <div className="border-b bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {error}
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {mediaList.length === 0 ? (
          <div className="p-4 text-center text-xs text-muted-foreground">
            还没有素材，点击右上角上传
          </div>
        ) : filteredMedia.length === 0 ? (
          <div className="p-4 text-center text-xs text-muted-foreground">
            没有匹配“{query}”的素材
          </div>
        ) : (
          <ul className="flex flex-col gap-1 p-2">
            {filteredMedia.map((m) => {
              const Icon = KIND_ICON[m.kind];
              const readyForTimeline = isMediaReadyForTimeline(m);
              const status = processingView(m);
              const menu = mediaMenuAvailability(m);
              return (
                <li
                  key={m.mediaId}
                  data-media-library-id={m.mediaId}
                  draggable={readyForTimeline}
                  title={
                    readyForTimeline
                      ? "点击在右侧预览原片；拖到下方时间线放入对应轨道；右键更多操作"
                      : "基础探测尚未完成；可预览，暂不能加入时间线"
                  }
                  onClick={() => setSourcePreview(m.mediaId)}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    setOpenMenuId(m.mediaId);
                  }}
                  onDragStart={(e) => {
                    if (!readyForTimeline) {
                      e.preventDefault();
                      return;
                    }
                    e.dataTransfer.effectAllowed = "copy";
                    e.dataTransfer.setData(
                      MEDIA_DND_TYPE,
                      JSON.stringify({ mediaId: m.mediaId, kind: m.kind }),
                    );
                  }}
                  className={`group flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-xs hover:bg-accent ${
                    readyForTimeline ? "active:cursor-grabbing" : "opacity-80"
                  } ${
                    mediaRevealRequest?.mediaId === m.mediaId
                      ? "bg-accent ring-2 ring-primary/60"
                      : ""
                  }`}
                >
                  <div className="flex size-9 shrink-0 cursor-grab items-center justify-center overflow-hidden rounded bg-muted">
                    {m.thumbnailUri ? (
                      // 视频缩略图是 THUMBNAIL_FRAME_COUNT 帧横向拼接的 sprite；
                      // 这里只需要露出第一帧，把背景图宽度撑到 N 倍容器宽再固定
                      // 左对齐即可（比 <img> + object-fit 更好控制裁切位置）。
                      <div
                        className="size-full bg-center bg-no-repeat"
                        style={{
                          backgroundImage: `url(${m.thumbnailUri})`,
                          backgroundPosition: "left center",
                          backgroundSize:
                            m.kind === "video" ? `${THUMBNAIL_FRAME_COUNT * 100}% 100%` : "cover",
                        }}
                      />
                    ) : m.kind === "audio" && m.waveformPeaks?.length ? (
                      <WaveformPreview peaks={m.waveformPeaks} />
                    ) : (
                      <Icon className="size-4 text-muted-foreground" />
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    {renamingMediaId === m.mediaId ? (
                      <Input
                        autoFocus
                        className="h-6 text-xs"
                        value={renameDraft}
                        maxLength={256}
                        onClick={(e) => e.stopPropagation()}
                        onChange={(e) => setRenameDraft(e.target.value)}
                        onBlur={commitRename}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") e.currentTarget.blur();
                          if (e.key === "Escape") {
                            setRenamingMediaId(null);
                          }
                        }}
                      />
                    ) : (
                      <div className="truncate" title={m.uri}>
                        {displayName(m)}
                      </div>
                    )}
                    <div className="text-muted-foreground">
                      {m.kind === "image" ? "图片" : formatFrames(m.durationInFrames, fps)}
                    </div>
                    {status && (
                      <div className="mt-1 flex min-w-0 flex-col gap-0.5">
                        <Badge
                          variant="outline"
                          className={`h-4 max-w-full px-1.5 text-[10px] ${STATUS_CLASS[status.tone]}`}
                          title={[status.stage, ...status.failedStages, status.error]
                            .filter(Boolean)
                            .join("；")}
                        >
                          {status.tone === "success" ? (
                            <CheckCircle2 />
                          ) : status.tone === "danger" || status.tone === "warning" ? (
                            <CircleAlert />
                          ) : (
                            <Loader2 className="animate-spin" />
                          )}
                          <span className="truncate">{status.label}</span>
                        </Badge>
                        {status.stage && (
                          <span className="truncate text-[10px] text-muted-foreground">
                            {status.stage}
                          </span>
                        )}
                        {status.failedStages.length > 0 && (
                          <span
                            className="truncate text-[10px] text-destructive"
                            title={status.error}
                          >
                            {status.failedStages.join("、")}
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                  <DropdownMenu
                    open={openMenuId === m.mediaId}
                    onOpenChange={(open) => setOpenMenuId(open ? m.mediaId : null)}
                  >
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        className="shrink-0 rounded p-1 opacity-0 hover:bg-accent group-hover:opacity-100 data-open:opacity-100"
                        aria-label={`打开${displayName(m)}的更多操作`}
                        title="更多操作（也可右键素材条目打开）"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <MoreVertical className="size-3.5 text-muted-foreground" />
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent onClick={(e) => e.stopPropagation()}>
                      <DropdownMenuItem
                        disabled={!menu.addToTimeline}
                        title={
                          menu.addToTimeline
                            ? "将素材追加到默认轨道；没有兼容轨道时自动创建"
                            : "等待基础探测完成"
                        }
                        onSelect={() => handleAddToTimeline(m)}
                      >
                        <ListPlus />
                        添加到时间线
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={!menu.viewTranscript}
                        title={menu.viewTranscript ? "查看带时间戳的文字稿" : "尚无文字稿"}
                        onSelect={() => setDetail({ kind: "transcript", mediaId: m.mediaId })}
                      >
                        <FileText />
                        查看文字稿
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={!menu.generateCaptions}
                        title={
                          menu.generateCaptions
                            ? "按文字稿真实时间戳创建或更新自动字幕轨"
                            : "尚无带时间戳的文字稿"
                        }
                        onSelect={() => handleGenerateCaptions(m)}
                      >
                        <Captions />
                        从文字稿生成字幕轨
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={!menu.viewUnderstanding}
                        title={menu.viewUnderstanding ? "查看内容理解结果" : "尚无内容理解结果"}
                        onSelect={() => setDetail({ kind: "understanding", mediaId: m.mediaId })}
                      >
                        <Brain />
                        查看内容理解
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={!menu.retry || retryingMediaId !== null}
                        title={menu.retry ? "重新启动后台素材解析" : "当前无需重新解析"}
                        onSelect={() => void handleRetryPreparation(m)}
                      >
                        <RefreshCw
                          className={retryingMediaId === m.mediaId ? "animate-spin" : undefined}
                        />
                        重新解析
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onSelect={() => handleDownload(m)}>
                        <Download />
                        下载
                      </DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => startRename(m)}>
                        <Pencil />
                        重命名
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onSelect={() => handleDeleteMedia(m.mediaId, displayName(m))}
                      >
                        <Trash2 />
                        删除素材
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </li>
              );
            })}
          </ul>
        )}
      </div>
      </TabsContent>
      <TabsContent value="transitions" className="min-h-0 flex-1 overflow-y-auto">
        <div className="border-b px-3 py-2">
          <p className="text-xs font-medium">Transitions</p>
          <p className="mt-0.5 text-[10px] text-muted-foreground">
            将资源拖到时间线上两个相邻视频片段的边界。
          </p>
        </div>
        <ul className="flex flex-col gap-1 p-2">
          {TRANSITION_RESOURCES.map((resource) => (
            <li
              key={resource.kind}
              data-testid={`transition-resource-${resource.kind}`}
              draggable
              className="flex cursor-grab items-center gap-2 rounded-md px-2 py-2 text-xs hover:bg-accent active:cursor-grabbing"
              title={`${resource.description}；拖到相邻视频片段边界`}
              onDragStart={(event) => {
                event.dataTransfer.effectAllowed = "copy";
                event.dataTransfer.setData(
                  TRANSITION_DND_TYPE,
                  JSON.stringify({ kind: resource.kind }),
                );
                event.dataTransfer.setData(
                  TRANSITION_DND_KIND_TYPE[resource.kind],
                  resource.kind,
                );
              }}
            >
              <div className="flex size-9 shrink-0 items-center justify-center rounded bg-muted">
                {resource.kind === "none" ? (
                  <X className="size-4 text-muted-foreground" />
                ) : (
                  <Film className="size-4 text-fuchsia-500" />
                )}
              </div>
              <div className="min-w-0">
                <div className="font-medium">{resource.name}</div>
                <div className="text-[10px] text-muted-foreground">
                  {resource.description}
                </div>
              </div>
            </li>
          ))}
        </ul>
      </TabsContent>
      <TabsContent value="transcript" className="flex min-h-0 flex-1 flex-col">
        <TranscriptPanel />
      </TabsContent>
      {detail && detailMedia && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-4"
          role="presentation"
          onMouseDown={() => setDetail(null)}
        >
          <section
            role="dialog"
            aria-modal="true"
            aria-label={detail.kind === "transcript" ? "素材文字稿" : "素材内容理解"}
            className="flex max-h-[70vh] w-full max-w-lg flex-col overflow-hidden rounded-lg border bg-background shadow-xl"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <header className="flex items-center gap-2 border-b px-4 py-3">
              {detail.kind === "transcript" ? (
                <FileText className="size-4" />
              ) : (
                <Brain className="size-4" />
              )}
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{displayName(detailMedia)}</div>
                <div className="text-xs text-muted-foreground">
                  {detail.kind === "transcript" ? "文字稿" : "内容理解"}
                </div>
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="关闭详情"
                title="关闭"
                onClick={() => setDetail(null)}
              >
                <X />
              </Button>
            </header>
            <ScrollArea className="min-h-0 flex-1 p-4">
              {detail.kind === "transcript" ? (
                <ol className="space-y-2 text-sm">
                  {(detailMedia.transcript?.segments ?? []).map((segment, index) => (
                    <li key={`${segment.startMs}-${segment.endMs}-${index}`} className="flex gap-3">
                      <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                        {formatMs(segment.startMs)}–{formatMs(segment.endMs)}
                      </span>
                      <span>{segment.text}</span>
                    </li>
                  ))}
                </ol>
              ) : (
                <p className="whitespace-pre-wrap text-sm leading-6">
                  {detailMedia.understandingText}
                </p>
              )}
            </ScrollArea>
          </section>
        </div>
      )}
    </Tabs>
  );
}
