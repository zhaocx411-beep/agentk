import { create } from "zustand";

import { api } from "@/remote";
import type { TranscriptEntry } from "@/remote";

import { useTimelineStore } from "./timelineStore";

export type TranscriptLoadStatus = "idle" | "loading" | "ready" | "error";

interface TranscriptState {
  projectId: string | null;
  timelineRevision: number | null;
  status: TranscriptLoadStatus;
  entries: TranscriptEntry[];
  warnings: string[];
  error: string | null;
  selectedOccurrenceIds: Set<string>;
  anchorOccurrenceId: string | null;
  setSelection(ids: Iterable<string>, anchorOccurrenceId?: string | null): void;
  clearSelection(): void;
}

let generation = 0;
let activeProjectId: string | null = null;
let activeController: AbortController | null = null;

export const useTranscriptStore = create<TranscriptState>((set) => ({
  projectId: null,
  timelineRevision: null,
  status: "idle",
  entries: [],
  warnings: [],
  error: null,
  selectedOccurrenceIds: new Set(),
  anchorOccurrenceId: null,
  setSelection(ids, anchorOccurrenceId) {
    set({
      selectedOccurrenceIds: new Set(ids),
      anchorOccurrenceId:
        anchorOccurrenceId === undefined ? useTranscriptStore.getState().anchorOccurrenceId : anchorOccurrenceId,
    });
  },
  clearSelection() {
    set({ selectedOccurrenceIds: new Set(), anchorOccurrenceId: null });
  },
}));

export function resetTranscriptStore(projectId: string | null = null): void {
  generation += 1;
  activeController?.abort();
  activeController = null;
  activeProjectId = projectId;
  useTranscriptStore.setState({
    projectId,
    timelineRevision: null,
    status: "idle",
    entries: [],
    warnings: [],
    error: null,
    selectedOccurrenceIds: new Set(),
    anchorOccurrenceId: null,
  });
}

export function invalidateTranscriptView(projectId: string): void {
  if (activeProjectId !== projectId) return;
  generation += 1;
  activeController?.abort();
  activeController = null;
  useTranscriptStore.setState({
    timelineRevision: null,
    status: "idle",
    entries: [],
    warnings: [],
    error: null,
    selectedOccurrenceIds: new Set(),
    anchorOccurrenceId: null,
  });
}

export async function refreshTranscriptView(
  projectId: string,
  expectedTimelineRevision: number,
  options: { force?: boolean } = {},
): Promise<void> {
  if (activeProjectId === null) {
    activeProjectId = projectId;
    useTranscriptStore.setState({ projectId });
  }
  if (activeProjectId !== projectId) return;
  const current = useTranscriptStore.getState();
  if (
    !options.force &&
    current.status === "ready" &&
    current.timelineRevision === expectedTimelineRevision
  ) {
    return;
  }
  const requestGeneration = ++generation;
  activeController?.abort();
  const controller = new AbortController();
  activeController = controller;
  useTranscriptStore.setState({
    status: "loading",
    error: null,
    timelineRevision: expectedTimelineRevision,
    entries: [],
    warnings: [],
    selectedOccurrenceIds: new Set(),
    anchorOccurrenceId: null,
  });
  try {
    const view = await api.getTranscriptView(projectId, { signal: controller.signal });
    if (
      controller.signal.aborted ||
      requestGeneration !== generation ||
      activeProjectId !== projectId ||
      useTimelineStore.getState().confirmedRevision !== expectedTimelineRevision
    ) {
      return;
    }
    if (view.timelineRevision !== expectedTimelineRevision) {
      useTranscriptStore.setState({
        status: "error",
        error: "时间线版本已变化，正在等待权威状态校准",
        entries: [],
        warnings: view.warnings ?? [],
      });
      return;
    }
    useTranscriptStore.setState({
      projectId,
      timelineRevision: view.timelineRevision,
      status: "ready",
      entries: view.entries ?? [],
      warnings: view.warnings ?? [],
      error: null,
    });
  } catch (error) {
    if (
      controller.signal.aborted ||
      requestGeneration !== generation ||
      (error instanceof Error && error.name === "AbortError")
    ) {
      return;
    }
    useTranscriptStore.setState({
      status: "error",
      error: error instanceof Error ? error.message : "文字稿加载失败",
      entries: [],
      warnings: [],
    });
  } finally {
    if (activeController === controller) activeController = null;
  }
}
