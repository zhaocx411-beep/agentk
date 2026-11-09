import { create } from "zustand";

import { api, type TimelineSnapshot, type TimelineSummary } from "@/remote";

const DEFAULT_ZOOM = 1;

function legacySummary(projectId: string, snapshot: TimelineSnapshot): TimelineSummary {
  return {
    timelineId: snapshot.activeTimelineId ?? `tl_legacy_${projectId}`,
    name: "剪辑 1",
    revision: snapshot.revision,
    durationInFrames: snapshot.durationInFrames,
    currentFrame: 0,
    zoom: DEFAULT_ZOOM,
    createdAtUnixMs: 0,
    updatedAtUnixMs: 0,
  };
}

export interface SequenceState {
  activeTimelineId: string | null;
  timelines: TimelineSummary[];
  zoom: number;
  hydrate(projectId: string, snapshot: TimelineSnapshot): void;
  updateActive(revision: number, durationInFrames: number): void;
  setZoom(zoom: number): void;
  reset(): void;
}

export const useSequenceStore = create<SequenceState>((set) => ({
  activeTimelineId: null,
  timelines: [],
  zoom: DEFAULT_ZOOM,
  hydrate(projectId, snapshot) {
    const fallback = legacySummary(projectId, snapshot);
    const timelines = snapshot.timelines?.length ? snapshot.timelines : [fallback];
    const activeTimelineId = snapshot.activeTimelineId ?? timelines[0]?.timelineId ?? fallback.timelineId;
    const active = timelines.find((timeline) => timeline.timelineId === activeTimelineId) ?? timelines[0] ?? fallback;
    set({
      activeTimelineId: active.timelineId,
      timelines: structuredClone(timelines),
      zoom: active.zoom > 0 ? active.zoom : DEFAULT_ZOOM,
    });
  },
  updateActive(revision, durationInFrames) {
    set((state) => ({
      timelines: state.timelines.map((timeline) =>
        timeline.timelineId === state.activeTimelineId
          ? { ...timeline, revision, durationInFrames, updatedAtUnixMs: Date.now() }
          : timeline,
      ),
    }));
  },
  setZoom(zoom) {
    if (!Number.isFinite(zoom) || zoom <= 0) return;
    set((state) => ({
      zoom,
      timelines: state.timelines.map((timeline) =>
        timeline.timelineId === state.activeTimelineId ? { ...timeline, zoom } : timeline,
      ),
    }));
  },
  reset() {
    set({ activeTimelineId: null, timelines: [], zoom: DEFAULT_ZOOM });
  },
}));

interface PendingViewSave {
  projectId: string;
  timelineId: string;
  currentFrame: number;
  zoom: number;
}

let pendingViewSave: PendingViewSave | null = null;
let viewSaveTimer: ReturnType<typeof setTimeout> | null = null;

async function persistPendingView(): Promise<void> {
  const pending = pendingViewSave;
  pendingViewSave = null;
  if (!pending) return;
  const saved = await api.updateTimelineView(pending.projectId, pending.timelineId, {
    requestId: crypto.randomUUID(),
    currentFrame: pending.currentFrame,
    zoom: pending.zoom,
  });
  useSequenceStore.setState((state) => ({
    timelines: state.timelines.map((timeline) =>
      timeline.timelineId === saved.timelineId ? saved : timeline,
    ),
  }));
}

export function scheduleSequenceViewSave(
  projectId: string,
  timelineId: string,
  currentFrame: number,
  zoom: number,
): void {
  pendingViewSave = { projectId, timelineId, currentFrame, zoom };
  if (viewSaveTimer) clearTimeout(viewSaveTimer);
  viewSaveTimer = setTimeout(() => {
    viewSaveTimer = null;
    void persistPendingView().catch((error: unknown) => {
      console.error("failed to save timeline view", error);
    });
  }, 500);
}

export async function flushSequenceViewSave(): Promise<void> {
  if (viewSaveTimer) {
    clearTimeout(viewSaveTimer);
    viewSaveTimer = null;
  }
  await persistPendingView();
}

export function resetSequenceViewSave(): void {
  if (viewSaveTimer) clearTimeout(viewSaveTimer);
  viewSaveTimer = null;
  pendingViewSave = null;
}
