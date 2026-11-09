import { ApiError, api } from "@/remote";

import { markAgentTurnConcurrentMutation } from "./agentSync";
import { snapshotHistoryEntry, useHistoryStore } from "./historyStore";
import { useSequenceStore } from "./sequenceStore";
import { invalidateTranscriptView, refreshTranscriptView } from "./transcriptStore";
import { useTimelineStore } from "./timelineStore";

export type SeparateAudioPhase = "loading" | "success" | "error";

const POLL_INTERVAL_MS = 1500;
const MAX_POLLS = 400;
const activeSeparations = new Map<string, Promise<string | null>>();

export function separateAudioItem(
  projectId: string,
  itemId: string,
  onPhase?: (phase: SeparateAudioPhase, message?: string) => void,
): Promise<string | null> {
  const key = `${projectId}\u0000${itemId}`;
  const active = activeSeparations.get(key);
  if (active) {
    onPhase?.("loading", "音频分离正在进行");
    return active;
  }
  const task = runAudioSeparation(projectId, itemId, onPhase);
  activeSeparations.set(key, task);
  void task.finally(() => {
    if (activeSeparations.get(key) === task) activeSeparations.delete(key);
  });
  return task;
}

async function runAudioSeparation(
  projectId: string,
  itemId: string,
  onPhase?: (phase: SeparateAudioPhase, message?: string) => void,
): Promise<string | null> {
  const timeline = useTimelineStore.getState();
  if (!timeline.doc || timeline.pendingOps.length > 0 || timeline.gestureActive) {
    const message = "仍有编辑正在保存，请稍后再分离音频";
    onPhase?.("error", message);
    return message;
  }
  const beforeDoc = structuredClone(timeline.doc);
  const baseRevision = timeline.confirmedRevision;
  const timelineId = useSequenceStore.getState().activeTimelineId ?? undefined;
  markAgentTurnConcurrentMutation(projectId);
  onPhase?.("loading");
  try {
    await api.startAudioSeparation(projectId, itemId, {
      clientMutationId: crypto.randomUUID(),
      timelineId,
      baseRevision,
    });
    for (let attempt = 0; attempt < MAX_POLLS; attempt += 1) {
      const status = await api.getAudioSeparation(projectId, itemId, timelineId);
      if (status.status === "running") {
        await sleep(POLL_INTERVAL_MS);
        continue;
      }
      if (status.status === "failed") {
        const message =
          status.code === "REVISION_CONFLICT"
            ? "时间线已变化，分离结果未写入，请重试"
            : status.error || "音频分离失败";
        onPhase?.("error", message);
        return message;
      }
      if ((useSequenceStore.getState().activeTimelineId ?? undefined) !== timelineId) {
        return "已切换到其他时间线；分离结果未写入当前编辑视图";
      }
      const current = useTimelineStore.getState();
      if (status.result.revision <= baseRevision) {
        const fresh = await api.getTimeline(projectId);
        useTimelineStore.getState().setConfirmed(fresh.doc, fresh.revision);
        onPhase?.("success", "音频已分离");
        return null;
      }
      if (
        current.confirmedRevision !== baseRevision ||
        current.pendingOps.length > 0
      ) {
        const message = "时间线在分离期间发生变化，已保留服务端结果并重新加载";
        const fresh = await api.getTimeline(projectId);
        useTimelineStore.getState().setConfirmed(fresh.doc, fresh.revision);
        onPhase?.("error", message);
        return message;
      }
      useTimelineStore
        .getState()
        .setConfirmed(status.result.doc, status.result.revision);
      useHistoryStore
        .getState()
        .push(snapshotHistoryEntry("分离音频", beforeDoc, status.result.doc));
      invalidateTranscriptView(projectId);
      void refreshTranscriptView(projectId, status.result.revision, { force: true });
      onPhase?.("success", "音频分离完成");
      return null;
    }
    throw new Error("音频分离超时，请稍后重试");
  } catch (error) {
    const message =
      error instanceof ApiError
        ? error.body.error
        : error instanceof Error
          ? error.message
          : "音频分离失败";
    onPhase?.("error", message);
    return message;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
