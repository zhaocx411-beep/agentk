import { create } from "zustand";

import type { Operation, TimelineDoc } from "@/domain/timeline/types";

export interface HistoryEntry {
  id: string;
  label: string;
  kind?: "ops" | "snapshot";
  forward?: Operation[];
  inverse?: Operation[];
  beforeDoc?: TimelineDoc;
  afterDoc?: TimelineDoc;
}

export function snapshotHistoryEntry(
  label: string,
  beforeDoc: TimelineDoc,
  afterDoc: TimelineDoc,
): HistoryEntry {
  return {
    id: crypto.randomUUID(),
    kind: "snapshot",
    label,
    beforeDoc: structuredClone(beforeDoc),
    afterDoc: structuredClone(afterDoc),
  };
}

interface HistoryState {
  undoStack: HistoryEntry[];
  redoStack: HistoryEntry[];
  push(entry: HistoryEntry): void;
  takeUndo(): HistoryEntry | null;
  takeRedo(): HistoryEntry | null;
  restoreUndo(entry: HistoryEntry): void;
  restoreRedo(entry: HistoryEntry): void;
  clear(): void;
}

export const useHistoryStore = create<HistoryState>((set, get) => ({
  undoStack: [],
  redoStack: [],
  push(entry) {
    set((s) => ({ undoStack: [...s.undoStack, entry].slice(-100), redoStack: [] }));
  },
  takeUndo() {
    const stack = get().undoStack;
    const entry = stack.at(-1) ?? null;
    if (entry) set({ undoStack: stack.slice(0, -1) });
    return entry;
  },
  takeRedo() {
    const stack = get().redoStack;
    const entry = stack.at(-1) ?? null;
    if (entry) set({ redoStack: stack.slice(0, -1) });
    return entry;
  },
  restoreUndo(entry) {
    set((s) => ({ undoStack: [...s.undoStack, entry] }));
  },
  restoreRedo(entry) {
    set((s) => ({ redoStack: [...s.redoStack, entry] }));
  },
  clear() {
    set({ undoStack: [], redoStack: [] });
  },
}));
