import { ApiError, api } from "@/remote";

import { useHistoryStore } from "./historyStore";
import { markAgentTurnConcurrentMutation } from "./agentSync";
import { submitOps } from "./submitOps";
import { useSequenceStore } from "./sequenceStore";
import { invalidateTranscriptView, refreshTranscriptView } from "./transcriptStore";
import { useTimelineStore } from "./timelineStore";
import { toast } from "./toastStore";

async function restoreSnapshot(
  projectId: string,
  doc: import("@/domain/timeline/types").TimelineDoc | undefined,
): Promise<string | null> {
  if (!doc) return "恢复记录缺少时间线快照";
  const timeline = useTimelineStore.getState();
  if (timeline.pendingOps.length > 0 || timeline.gestureActive) {
    return "仍有编辑正在保存，请稍后再恢复";
  }
  markAgentTurnConcurrentMutation(projectId);
  try {
    const response = await api.restoreTimeline(projectId, {
      clientMutationId: crypto.randomUUID(),
      timelineId: useSequenceStore.getState().activeTimelineId ?? undefined,
      baseRevision: timeline.confirmedRevision,
      doc,
    });
    useTimelineStore.getState().setConfirmed(response.doc, response.revision);
    invalidateTranscriptView(projectId);
    void refreshTranscriptView(projectId, response.revision, { force: true });
    return null;
  } catch (error) {
    if (
      error instanceof ApiError &&
      error.status === 409 &&
      error.body.code === "REVISION_CONFLICT"
    ) {
      return "时间线已被其他操作修改，恢复已取消";
    }
    return error instanceof ApiError
      ? error.body.error
      : error instanceof Error
        ? error.message
        : "恢复失败";
  }
}

export async function undoTimeline(projectId: string): Promise<void> {
  const history = useHistoryStore.getState();
  const entry = history.takeUndo();
  if (!entry) return;
  const error =
    entry.kind === "snapshot"
      ? await restoreSnapshot(projectId, entry.beforeDoc)
      : await submitOps(projectId, entry.inverse ?? [], { recordHistory: false });
  if (error) {
    useHistoryStore.getState().restoreUndo(entry);
    toast.error(`撤销失败：${error}`);
    return;
  }
  useHistoryStore.getState().restoreRedo(entry);
  toast.success(`已撤销：${entry.label}`);
}

export async function redoTimeline(projectId: string): Promise<void> {
  const history = useHistoryStore.getState();
  const entry = history.takeRedo();
  if (!entry) return;
  const error =
    entry.kind === "snapshot"
      ? await restoreSnapshot(projectId, entry.afterDoc)
      : await submitOps(projectId, entry.forward ?? [], { recordHistory: false });
  if (error) {
    useHistoryStore.getState().restoreRedo(entry);
    toast.error(`重做失败：${error}`);
    return;
  }
  useHistoryStore.getState().restoreUndo(entry);
  toast.success(`已重做：${entry.label}`);
}
