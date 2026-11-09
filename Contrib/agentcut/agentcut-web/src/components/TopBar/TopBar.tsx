// 顶部栏（M1：项目名、保存状态、导出）。
// Backlog 不画：撤销历史列表、分享、模板、订阅升级（设计 1.2 节）。
//
// 导出接入真实后端（设计 10 节 / 阶段 D）：点击 → POST /renders → 短轮询
// GET /renders/{renderKey}（2.5s 一次，renderStore.ts）→ 展示进度/完成/失败。
import {
  Check,
  Copy,
  Download,
  Loader2,
  Pencil,
  Plus,
  Redo2,
  RefreshCcw,
  Share2,
  Undo2,
  X,
} from "lucide-react";
import { useEffect, useState } from "react";

import { ExportDialog } from "@/components/ExportDialog/ExportDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError, api } from "@/remote";
import { redoTimeline, undoTimeline } from "@/store/historyActions";
import { useHistoryStore } from "@/store/historyStore";
import { useProjectStore } from "@/store/projectStore";
import { useRenderStore } from "@/store/renderStore";
import { discardFailedChangesAndReload } from "@/store/submitOps";
import { useTimelineStore } from "@/store/timelineStore";

const SYNC_LABEL: Record<string, string> = {
  synced: "已保存",
  pending: "保存中…",
  conflict: "有冲突",
  offline: "离线",
};

export function TopBar() {
  const syncStatus = useTimelineStore((s) => s.syncStatus);
  const pendingCount = useTimelineStore((s) => s.pendingOps.length);
  const hasFailedPending = useTimelineStore((s) =>
    s.pendingOps.some((batch) => batch.status === "failed"),
  );
  const project = useProjectStore((s) => s.project);
  const projects = useProjectStore((s) => s.projects);
  const projectLoading = useProjectStore((s) => s.loading);
  const switchProject = useProjectStore((s) => s.switchProject);
  const createProject = useProjectStore((s) => s.createProject);
  const renameCurrent = useProjectStore((s) => s.renameCurrent);
  const undoCount = useHistoryStore((s) => s.undoStack.length);
  const redoCount = useHistoryStore((s) => s.redoStack.length);
  const phase = useRenderStore((s) => s.phase);
  const progress = useRenderStore((s) => s.progress);
  const [exportOpen, setExportOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [shareOpen, setShareOpen] = useState(false);
  const [shareLoading, setShareLoading] = useState(false);
  const [shareUrl, setShareUrl] = useState("");
  const [shareError, setShareError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [recoveringTimeline, setRecoveringTimeline] = useState(false);

  const busy = phase === "starting" || phase === "running";

  useEffect(() => setTitleDraft(project?.title ?? ""), [project?.title]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const key = event.key.toLowerCase();
      const isUndo = key === "z" && !event.shiftKey;
      const isRedo = key === "y" || (key === "z" && event.shiftKey);
      if (
        !project ||
        pendingCount > 0 ||
        !(event.metaKey || event.ctrlKey) ||
        (!isUndo && !isRedo)
      ) {
        return;
      }
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)
      ) {
        return;
      }
      event.preventDefault();
      if (isRedo) void redoTimeline(project.projectId);
      else void undoTimeline(project.projectId);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [project, pendingCount]);

  function handleExportClick() {
    if (!project) return;
    setExportOpen(true);
  }

  async function handleTimelineRecovery() {
    if (!project || recoveringTimeline) return;
    if (
      hasFailedPending &&
      !window.confirm("本地保存失败的修改将被放弃且无法撤销。确定重新加载服务端时间线？")
    ) {
      return;
    }
    setRecoveringTimeline(true);
    try {
      await discardFailedChangesAndReload(project.projectId);
    } finally {
      setRecoveringTimeline(false);
    }
  }

  async function handleShareClick() {
    if (!project || shareLoading) return;
    setShareOpen(true);
    setShareLoading(true);
    setShareUrl("");
    setShareError(null);
    setCopied(false);
    try {
      const share = await api.createProjectShare(project.projectId);
      setShareUrl(new URL(share.url, window.location.origin).toString());
    } catch (err) {
      setShareError(
        err instanceof ApiError && (err.status === 404 || err.status === 501)
          ? "后端分享接口尚未部署，前端入口和调用契约已就绪。"
          : err instanceof ApiError
            ? `生成分享链接失败：${err.body.error}`
            : err instanceof Error
              ? err.message
              : "生成分享链接失败",
      );
    } finally {
      setShareLoading(false);
    }
  }

  async function copyShareUrl() {
    if (!shareUrl) return;
    try {
      await navigator.clipboard.writeText(shareUrl);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setShareError("复制失败，请手动选择链接复制。");
    }
  }

  return (
    <>
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-[#D8D6D2] bg-[#F2F2F0] px-4">
      {renaming ? (
        <Input
          autoFocus
          className="h-8 w-52"
          value={titleDraft}
          maxLength={120}
          onChange={(event) => setTitleDraft(event.target.value)}
          onBlur={() => {
            setRenaming(false);
            if (titleDraft.trim()) void renameCurrent(titleDraft);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur();
            if (event.key === "Escape") {
              setTitleDraft(project?.title ?? "");
              setRenaming(false);
            }
          }}
        />
      ) : (
        <div className="flex items-center gap-1">
          <select
            aria-label="切换项目"
            className="h-8 max-w-52 rounded-md border bg-background px-2 text-sm font-semibold"
            value={project?.projectId ?? ""}
            disabled={projectLoading || pendingCount > 0}
            onChange={(event) => void switchProject(event.target.value)}
          >
            {!project && <option value="">加载中…</option>}
            {projects.map((item) => (
              <option key={item.projectId} value={item.projectId}>
                {item.title}
              </option>
            ))}
          </select>
          <Button
            variant="ghost"
            size="icon-sm"
            title="重命名项目"
            aria-label="重命名项目"
            disabled={!project || projectLoading || pendingCount > 0}
            onClick={() => setRenaming(true)}
          >
            <Pencil />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            title="新建项目"
            aria-label="新建项目"
            disabled={projectLoading || pendingCount > 0}
            onClick={() => {
              const title = window.prompt("新项目名称", "未命名项目");
              if (title?.trim()) void createProject(title.trim());
            }}
          >
            <Plus />
          </Button>
        </div>
      )}
      <Badge variant="secondary" className="text-xs font-normal">
        {SYNC_LABEL[syncStatus] ?? syncStatus}
      </Badge>
      {(hasFailedPending || syncStatus === "conflict" || syncStatus === "offline") && (
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          disabled={!project || recoveringTimeline}
          title={
            hasFailedPending
              ? "放弃本地失败修改并重新加载服务端时间线"
              : "重新加载服务端时间线"
          }
          onClick={() => void handleTimelineRecovery()}
        >
          {recoveringTimeline ? (
            <Loader2 data-slot="icon" className="animate-spin" />
          ) : (
            <RefreshCcw data-slot="icon" />
          )}
          {hasFailedPending ? "放弃修改并重新加载" : "重新加载时间线"}
        </Button>
      )}
      <Button
        variant="ghost"
        size="icon-sm"
        title="撤销（Cmd+Z）"
        aria-label="撤销（Cmd+Z）"
        disabled={!project || undoCount === 0 || pendingCount > 0}
        onClick={() => project && void undoTimeline(project.projectId)}
      >
        <Undo2 />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        title="重做（Cmd+Shift+Z）"
        aria-label="重做（Cmd+Shift+Z）"
        disabled={!project || redoCount === 0 || pendingCount > 0}
        onClick={() => project && void redoTimeline(project.projectId)}
      >
        <Redo2 />
      </Button>
      <div className="flex-1" />

      {busy && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-xs text-muted-foreground"
          title={progress?.current || "导出在后台进行"}
          onClick={() => setExportOpen(true)}
        >
          <Loader2 data-slot="icon" className="animate-spin" />
          导出中
          {progress ? ` ${progress.done}/${progress.total}` : ""}
        </Button>
      )}

      <Button
        variant="outline"
        size="sm"
        disabled={!project || shareLoading}
        title="生成只读分享链接"
        onClick={() => void handleShareClick()}
      >
        {shareLoading ? <Loader2 data-slot="icon" className="animate-spin" /> : <Share2 />}
        分享
      </Button>
      <Button
        size="sm"
        disabled={!project}
        title={busy ? "查看后台导出进度" : "打开导出窗口"}
        onClick={handleExportClick}
      >
        <Download data-slot="icon" />
        导出
      </Button>
      </header>

      {shareOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"
          role="presentation"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setShareOpen(false);
          }}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="share-dialog-title"
            className="w-full max-w-md rounded-xl border bg-background p-5 shadow-2xl"
          >
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 id="share-dialog-title" className="font-semibold">
                  分享项目
                </h2>
                <p className="mt-1 text-xs text-muted-foreground">
                  生成由后端控制访问权限的只读链接。
                </p>
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                title="关闭分享窗口"
                aria-label="关闭分享窗口"
                onClick={() => setShareOpen(false)}
              >
                <X />
              </Button>
            </div>
            <div className="mt-4">
              {shareLoading && (
                <div className="flex items-center gap-2 rounded-md bg-muted p-3 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" />
                  正在生成分享链接…
                </div>
              )}
              {shareError && (
                <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                  {shareError}
                </div>
              )}
              {shareUrl && (
                <div className="flex gap-2">
                  <Input readOnly value={shareUrl} aria-label="只读分享链接" />
                  <Button variant="outline" onClick={() => void copyShareUrl()}>
                    {copied ? <Check data-slot="icon" /> : <Copy data-slot="icon" />}
                    {copied ? "已复制" : "复制"}
                  </Button>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
      <ExportDialog open={exportOpen} onOpenChange={setExportOpen} />
    </>
  );
}
