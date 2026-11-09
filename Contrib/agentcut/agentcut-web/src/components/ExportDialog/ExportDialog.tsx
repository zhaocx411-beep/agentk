import {
  AlertTriangle,
  CheckCircle2,
  Download,
  Film,
  Loader2,
  RotateCcw,
  X,
} from "lucide-react";
import { useMemo } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { useProjectStore } from "@/store/projectStore";
import { useRenderStore } from "@/store/renderStore";
import { useTimelineStore } from "@/store/timelineStore";

import { formatExportDuration, validateExport } from "./exportValidation";

export interface ExportDialogProps {
  open: boolean;
  onOpenChange(open: boolean): void;
}

function WarningList({ warnings }: { warnings: string[] }) {
  if (warnings.length === 0) return null;
  return (
    <section className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3">
      <div className="flex items-center gap-2 text-sm font-medium text-amber-700 dark:text-amber-300">
        <AlertTriangle className="size-4" aria-hidden="true" />
        导出提示
      </div>
      <ul className="mt-2 list-disc space-y-1 pl-5 text-xs text-muted-foreground">
        {warnings.map((warning, index) => (
          <li key={`${warning}-${index}`}>{warning}</li>
        ))}
      </ul>
    </section>
  );
}

export function ExportDialog({ open, onOpenChange }: ExportDialogProps) {
  const project = useProjectStore((state) => state.project);
  const doc = useTimelineStore((state) => state.localDoc);
  const confirmedRevision = useTimelineStore((state) => state.confirmedRevision);
  const pendingCount = useTimelineStore((state) => state.pendingOps.length);
  const syncStatus = useTimelineStore((state) => state.syncStatus);

  const phase = useRenderStore((state) => state.phase);
  const renderProjectId = useRenderStore((state) => state.projectId);
  const renderRevision = useRenderStore((state) => state.revision);
  const progress = useRenderStore((state) => state.progress);
  const outputUri = useRenderStore((state) => state.outputUri);
  const warnings = useRenderStore((state) => state.warnings);
  const workflowStatus = useRenderStore((state) => state.workflowStatus);
  const serverError = useRenderStore((state) => state.serverError);
  const errorMessage = useRenderStore((state) => state.errorMessage);
  const start = useRenderStore((state) => state.start);
  const reset = useRenderStore((state) => state.reset);

  const validation = useMemo(() => validateExport(doc), [doc]);
  const waitingForSync = pendingCount > 0 || syncStatus !== "synced";
  const canStart =
    !!project &&
    !!doc &&
    validation.errors.length === 0 &&
    !waitingForSync &&
    phase === "idle";
  const fps = doc?.metadata.fps ?? 0;
  const percent =
    progress && progress.total > 0
      ? Math.min(100, Math.max(0, (progress.done / progress.total) * 100))
      : 0;

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen && phase === "failed") reset();
    onOpenChange(nextOpen);
  }

  function handleStart() {
    if (!canStart || !project) return;
    start(project.projectId);
  }

  function handleComplete() {
    reset();
    onOpenChange(false);
  }

  const isActive = phase === "starting" || phase === "running";

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent aria-describedby="export-dialog-description">
        <div className="flex items-start justify-between gap-3">
          <div>
            <DialogTitle>导出成片</DialogTitle>
            <DialogDescription id="export-dialog-description" className="mt-1 text-xs">
              {isActive
                ? "导出在后台进行，关闭窗口不会中断。"
                : phase === "completed"
                  ? "成片已经准备好。"
                  : phase === "failed"
                    ? "导出未完成，你可以修正问题后重试。"
                    : "确认项目参数后开始生成 MP4。"}
            </DialogDescription>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            title="关闭导出窗口"
            aria-label="关闭导出窗口"
            onClick={() => handleOpenChange(false)}
          >
            <X data-slot="icon" />
          </Button>
        </div>

        {phase === "idle" && (
          <div className="mt-4 space-y-4">
            <dl className="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg bg-muted/60 p-3 text-sm">
              <div>
                <dt className="text-xs text-muted-foreground">画幅</dt>
                <dd className="mt-0.5 font-medium">
                  {doc ? `${doc.metadata.width} × ${doc.metadata.height}` : "—"}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">帧率</dt>
                <dd className="mt-0.5 font-medium">{fps > 0 ? `${fps} fps` : "—"}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">时长</dt>
                <dd className="mt-0.5 font-medium">
                  {formatExportDuration(validation.durationInFrames, fps)}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">格式</dt>
                <dd className="mt-0.5 font-medium">MP4</dd>
              </div>
            </dl>

            {validation.errors.length > 0 && (
              <section
                className="rounded-lg border border-destructive/30 bg-destructive/10 p-3"
                aria-label="导出校验错误"
              >
                <div className="flex items-center gap-2 text-sm font-medium text-destructive">
                  <AlertTriangle className="size-4" aria-hidden="true" />
                  暂时无法导出
                </div>
                <ul className="mt-2 list-disc space-y-1 pl-5 text-xs text-destructive">
                  {validation.errors.map((error) => (
                    <li key={error}>{error}</li>
                  ))}
                </ul>
              </section>
            )}

            {waitingForSync && (
              <div className="flex items-center gap-2 rounded-lg bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                等待修改同步完成
              </div>
            )}
            <WarningList warnings={validation.warnings} />

            <p className="text-xs text-muted-foreground">最终效果以导出结果为准。</p>
            <div className="flex items-center justify-between gap-3 border-t pt-4">
              <span className="text-xs text-muted-foreground">
                服务端版本 {confirmedRevision}
              </span>
              <Button type="button" disabled={!canStart} onClick={handleStart}>
                <Film data-slot="icon" />
                开始导出
              </Button>
            </div>
          </div>
        )}

        {isActive && (
          <div className="mt-5 space-y-4">
            <div className="flex items-center justify-between gap-3 text-sm">
              <div className="flex items-center gap-2 font-medium">
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                {phase === "starting" ? "正在创建导出任务" : "正在导出"}
              </div>
              {progress && (
                <span className="tabular-nums text-muted-foreground">
                  {progress.done}/{progress.total}
                </span>
              )}
            </div>
            <div
              className="h-2 overflow-hidden rounded-full bg-muted"
              role="progressbar"
              aria-label="导出进度"
              aria-valuemin={0}
              aria-valuemax={progress?.total ?? 1}
              aria-valuenow={progress?.done ?? 0}
            >
              <div
                className="h-full rounded-full bg-primary transition-[width] duration-300"
                style={{ width: `${percent}%` }}
              />
            </div>
            <p className="min-h-5 text-sm text-muted-foreground">
              {progress?.current || (phase === "starting" ? "正在连接渲染服务…" : "处理中…")}
            </p>
            <WarningList warnings={warnings} />
            <div className="flex items-center justify-between border-t pt-4 text-xs text-muted-foreground">
              <span>{renderRevision === null ? "等待版本确认" : `导出版本 ${renderRevision}`}</span>
              <Button type="button" variant="outline" size="sm" onClick={() => handleOpenChange(false)}>
                后台继续
              </Button>
            </div>
          </div>
        )}

        {phase === "completed" && outputUri && (
          <div className="mt-4 space-y-4">
            <div className="flex items-center gap-2 text-sm font-medium text-emerald-600 dark:text-emerald-400">
              <CheckCircle2 className="size-4" aria-hidden="true" />
              导出完成
            </div>
            <video
              className="max-h-[42dvh] w-full rounded-lg bg-black"
              src={outputUri}
              controls
              playsInline
            />
            <WarningList warnings={warnings} />
            <div className="flex items-center justify-between gap-3 border-t pt-4">
              <Button asChild variant="outline">
                <a href={`${outputUri}?download=1`}>
                  <Download data-slot="icon" />
                  下载成片
                </a>
              </Button>
              <Button type="button" onClick={handleComplete}>
                完成
              </Button>
            </div>
          </div>
        )}

        {phase === "failed" && (
          <div className="mt-4 space-y-4">
            <section className="rounded-lg border border-destructive/30 bg-destructive/10 p-3">
              <div className="flex items-center gap-2 text-sm font-medium text-destructive">
                <AlertTriangle className="size-4" aria-hidden="true" />
                导出失败
              </div>
              <p className="mt-2 break-words text-sm text-destructive">
                {serverError || errorMessage || "渲染失败，请重试"}
              </p>
              {workflowStatus && (
                <p className="mt-2 text-xs text-muted-foreground">
                  Workflow 状态：{workflowStatus}
                </p>
              )}
            </section>
            <WarningList warnings={warnings} />
            <div className="flex items-center justify-end gap-2 border-t pt-4">
              <Button type="button" variant="ghost" onClick={() => handleOpenChange(false)}>
                关闭
              </Button>
              <Button
                type="button"
                disabled={!project || (renderProjectId !== null && renderProjectId !== project.projectId)}
                onClick={() => project && start(project.projectId)}
              >
                <RotateCcw data-slot="icon" />
                重试
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
