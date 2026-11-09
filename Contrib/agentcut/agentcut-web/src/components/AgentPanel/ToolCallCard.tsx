import { CheckCircle2, Loader2, Wrench, XCircle } from "lucide-react";

export interface ToolRun {
  toolCallId: string;
  toolName: string;
  args?: unknown;
  status: "requested" | "running" | "succeeded" | "failed";
  result?: unknown;
  error?: string;
}

const TOOL_LABELS: Record<string, string> = {
  "contrib.agentcut.timeline_get": "读取时间线",
  "contrib.agentcut.timeline_edit_clips": "编辑片段",
  "contrib.agentcut.timeline_edit_tracks": "编辑轨道",
  "contrib.agentcut.timeline_set_captions": "编辑字幕",
  "contrib.agentcut.timeline_edit_media": "编辑素材",
};

export function ToolCallCard({ run }: { run: ToolRun }) {
  const label = TOOL_LABELS[run.toolName] ?? run.toolName;
  const summary = resultSummary(run.result);
  return (
    <div className="mr-auto w-[88%] rounded-md border bg-muted/30 p-2.5 text-xs">
      <div className="flex items-center gap-2">
        {run.status === "running" || run.status === "requested" ? (
          <Loader2 className="size-3.5 animate-spin text-primary" />
        ) : run.status === "succeeded" ? (
          <CheckCircle2 className="size-3.5 text-emerald-500" />
        ) : (
          <XCircle className="size-3.5 text-destructive" />
        )}
        <Wrench className="size-3.5 text-muted-foreground" />
        <strong>{label}</strong>
        <span className="ml-auto text-muted-foreground">
          {{
            requested: "等待执行",
            running: "执行中",
            succeeded: "已完成",
            failed: "失败",
          }[run.status]}
        </span>
      </div>
      {argsSummary(run.args) && (
        <div className="mt-1.5 truncate text-muted-foreground">{argsSummary(run.args)}</div>
      )}
      {summary && <div className="mt-1.5 text-emerald-700 dark:text-emerald-300">{summary}</div>}
      {run.error && <div className="mt-1.5 text-destructive">{run.error}</div>}
    </div>
  );
}

function argsSummary(args: unknown): string {
  if (!args || typeof args !== "object") return "";
  const value = args as Record<string, unknown>;
  if (Array.isArray(value.operations)) return `${value.operations.length} 个操作`;
  try {
    const json = JSON.stringify(args);
    return json.length > 100 ? `${json.slice(0, 100)}…` : json;
  } catch {
    return "";
  }
}

function resultSummary(result: unknown): string {
  if (!result || typeof result !== "object") return "";
  const value = result as Record<string, unknown>;
  if (value.ok === false) return String(value.error ?? value.code ?? "操作未成功");
  const summary = value.summary as Record<string, unknown> | undefined;
  if (!summary) return value.revision ? `时间线已更新到 revision ${value.revision}` : "";
  const changed = Object.entries(summary)
    .filter(([, item]) => Array.isArray(item) && item.length > 0)
    .map(([key, item]) => `${key}: ${(item as unknown[]).length}`);
  return changed.length > 0 ? changed.join(" · ") : "操作完成";
}
