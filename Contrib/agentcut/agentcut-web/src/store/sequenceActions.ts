import { api, type TimelineSnapshot } from "@/remote";

import { resetAgentSync } from "./agentSync";
import { useEditorContextStore } from "./editorContextStore";
import { useHistoryStore } from "./historyStore";
import { usePlaybackStore } from "./playbackStore";
import { usePreviewOverridesStore } from "./previewOverridesStore";
import {
  flushSequenceViewSave,
  resetSequenceViewSave,
  useSequenceStore,
} from "./sequenceStore";
import { resetSubmitQueue, setSubmitQueueProject } from "./submitOps";
import { useTimelineStore } from "./timelineStore";
import { refreshTranscriptView, resetTranscriptStore } from "./transcriptStore";

function editingBlocked(): string | null {
  const timeline = useTimelineStore.getState();
  if (timeline.gestureActive) return "请先结束当前拖拽或裁切操作";
  if (timeline.pendingOps.length > 0) return "请等待当前修改保存完成";
  return null;
}

function applyActiveSnapshot(projectId: string, snapshot: TimelineSnapshot): void {
  // Revisions are sequence-local. Reset before loading so timelineStore's
  // same-sequence stale-snapshot guard cannot reject a valid lower revision.
  useTimelineStore.getState().resetTimeline();
  useTimelineStore.getState().setConfirmed(snapshot.doc, snapshot.revision);
  useSequenceStore.getState().hydrate(projectId, snapshot);
  const sequenceState = useSequenceStore.getState();
  const active = sequenceState.timelines.find(
    (timeline) => timeline.timelineId === sequenceState.activeTimelineId,
  );
  usePlaybackStore.getState().pause();
  usePlaybackStore.getState().setSourcePreview(null);
  usePlaybackStore.getState().setCurrentFrame(active?.currentFrame ?? 0);
  useEditorContextStore.getState().reset();
  useHistoryStore.getState().clear();
  usePreviewOverridesStore.getState().reset();
  resetAgentSync(projectId);
  resetTranscriptStore(projectId);
  void refreshTranscriptView(projectId, snapshot.revision);
  setSubmitQueueProject(projectId, sequenceState.activeTimelineId);
}

async function prepareLifecycleMutation(): Promise<string | null> {
  const blocked = editingBlocked();
  if (blocked) return blocked;
  try {
    await flushSequenceViewSave();
  } catch (error) {
    return `保存当前时间线视图失败：${error instanceof Error ? error.message : String(error)}`;
  }
  resetSequenceViewSave();
  usePlaybackStore.getState().pause();
  resetSubmitQueue("时间线切换，本地待提交修改已取消");
  setSubmitQueueProject(null);
  return null;
}

function restoreQueue(projectId: string): void {
  setSubmitQueueProject(projectId, useSequenceStore.getState().activeTimelineId);
}

export async function createSequence(projectId: string): Promise<string | null> {
  const blocked = await prepareLifecycleMutation();
  if (blocked) return blocked;
  try {
    const timelines = useSequenceStore.getState().timelines;
    const nextNumber =
      Math.max(
        timelines.length,
        ...timelines.map((timeline) => {
          const match = /^剪辑\s+(\d+)$/.exec(timeline.name.trim());
          return match ? Number(match[1]) : 0;
        }),
      ) + 1;
    const snapshot = await api.createTimeline(projectId, {
      requestId: crypto.randomUUID(),
      name: `剪辑 ${nextNumber}`,
    });
    applyActiveSnapshot(projectId, snapshot);
    return null;
  } catch (error) {
    restoreQueue(projectId);
    return error instanceof Error ? error.message : String(error);
  }
}

export async function switchSequence(
  projectId: string,
  timelineId: string,
): Promise<string | null> {
  if (timelineId === useSequenceStore.getState().activeTimelineId) return null;
  const blocked = await prepareLifecycleMutation();
  if (blocked) return blocked;
  try {
    const snapshot = await api.switchTimeline(projectId, timelineId, {
      requestId: crypto.randomUUID(),
    });
    applyActiveSnapshot(projectId, snapshot);
    return null;
  } catch (error) {
    restoreQueue(projectId);
    return error instanceof Error ? error.message : String(error);
  }
}

export async function renameSequence(
  projectId: string,
  timelineId: string,
  name: string,
): Promise<string | null> {
  const trimmed = name.trim();
  if (!trimmed) return "时间线名称不能为空";
  const blocked = editingBlocked();
  if (blocked) return blocked;
  try {
    const snapshot = await api.renameTimeline(projectId, timelineId, {
      requestId: crypto.randomUUID(),
      name: trimmed,
    });
    useSequenceStore.getState().hydrate(projectId, snapshot);
    return null;
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
}

export async function deleteSequence(
  projectId: string,
  timelineId: string,
): Promise<string | null> {
  if (useSequenceStore.getState().timelines.length <= 1) return "至少保留一条时间线";
  const previousActive = useSequenceStore.getState().activeTimelineId;
  const blocked = await prepareLifecycleMutation();
  if (blocked) return blocked;
  try {
    const snapshot = await api.deleteTimeline(projectId, timelineId, {
      requestId: crypto.randomUUID(),
    });
    if (snapshot.activeTimelineId !== previousActive) {
      applyActiveSnapshot(projectId, snapshot);
    } else {
      useSequenceStore.getState().hydrate(projectId, snapshot);
      restoreQueue(projectId);
    }
    return null;
  } catch (error) {
    restoreQueue(projectId);
    return error instanceof Error ? error.message : String(error);
  }
}
