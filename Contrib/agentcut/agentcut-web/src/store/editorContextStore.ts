import type { SetStateAction } from "react";
import { create } from "zustand";
import { useSequenceStore } from "./sequenceStore";

export interface EditorContextSnapshot {
  timelineId?: string;
  timelineRevision: number;
  selectedItemIds: string[];
  playheadFrame: number;
  selectedMediaId?: string;
  selectedCaptionItemId?: string;
  selectedCaptionTrackId?: string;
  selectedCaptionMediaId?: string;
  selectedCaptionVideoTrackId?: string;
}

export interface CaptionSelectionContext {
  itemId: string;
  trackId: string;
  mediaId?: string;
  videoTrackId?: string;
}

export interface AgentPromptRequest {
  text: string;
  editorContext: EditorContextSnapshot;
  nonce: number;
}

interface EditorContextState extends EditorContextSnapshot {
  mediaRevealRequest: { mediaId: string; nonce: number } | null;
  agentPromptRequest: AgentPromptRequest | null;
  setTimelineRevision(revision: number): void;
  setSelectedItemIds(selection: SetStateAction<string[]>): void;
  setPlayheadFrame(frame: number): void;
  setSelectedMediaId(mediaId?: string): void;
  setSelectedCaptionItemId(itemId?: string): void;
  setSelectedCaptionContext(context?: CaptionSelectionContext): void;
  requestMediaReveal(mediaId: string): void;
  requestAgentPrompt(text: string, editorContext: EditorContextSnapshot): void;
  reset(): void;
}

const initialContext: EditorContextSnapshot = {
  timelineRevision: 0,
  selectedItemIds: [],
  playheadFrame: 0,
};

export const useEditorContextStore = create<EditorContextState>((set) => ({
  ...initialContext,
  mediaRevealRequest: null,
  agentPromptRequest: null,
  setTimelineRevision: (timelineRevision) => set({ timelineRevision }),
  setSelectedItemIds: (selection) =>
    set((state) => ({
      selectedItemIds: [
        ...(typeof selection === "function"
          ? selection(state.selectedItemIds)
          : selection),
      ],
    })),
  setPlayheadFrame: (playheadFrame) => set({ playheadFrame }),
  setSelectedMediaId: (selectedMediaId) => set({ selectedMediaId }),
  setSelectedCaptionItemId: (selectedCaptionItemId) =>
    set({
      selectedCaptionItemId,
      ...(!selectedCaptionItemId
        ? {
            selectedCaptionTrackId: undefined,
            selectedCaptionMediaId: undefined,
            selectedCaptionVideoTrackId: undefined,
          }
        : {}),
    }),
  setSelectedCaptionContext: (context) =>
    set({
      selectedCaptionItemId: context?.itemId,
      selectedCaptionTrackId: context?.trackId,
      selectedCaptionMediaId: context?.mediaId,
      selectedCaptionVideoTrackId: context?.videoTrackId,
    }),
  requestMediaReveal: (mediaId) =>
    set((state) => ({
      mediaRevealRequest: {
        mediaId,
        nonce: (state.mediaRevealRequest?.nonce ?? 0) + 1,
      },
    })),
  requestAgentPrompt: (text, editorContext) =>
    set((state) => ({
      agentPromptRequest: {
        text,
        editorContext: {
          ...editorContext,
          selectedItemIds: [...editorContext.selectedItemIds],
        },
        nonce: (state.agentPromptRequest?.nonce ?? 0) + 1,
      },
    })),
  reset: () =>
    set({
      ...initialContext,
      selectedMediaId: undefined,
      selectedCaptionItemId: undefined,
      selectedCaptionTrackId: undefined,
      selectedCaptionMediaId: undefined,
      selectedCaptionVideoTrackId: undefined,
      mediaRevealRequest: null,
      agentPromptRequest: null,
    }),
}));

/** Capture an immutable message-time view so later editor changes cannot alter a request. */
export function snapshotEditorContext(): EditorContextSnapshot {
  const {
    timelineRevision,
    selectedItemIds,
    playheadFrame,
    selectedMediaId,
    selectedCaptionItemId,
    selectedCaptionTrackId,
    selectedCaptionMediaId,
    selectedCaptionVideoTrackId,
  } = useEditorContextStore.getState();
  return {
    ...(useSequenceStore.getState().activeTimelineId
      ? { timelineId: useSequenceStore.getState().activeTimelineId! }
      : {}),
    timelineRevision,
    selectedItemIds: [...selectedItemIds],
    playheadFrame,
    ...(selectedMediaId ? { selectedMediaId } : {}),
    ...(selectedCaptionItemId ? { selectedCaptionItemId } : {}),
    ...(selectedCaptionTrackId ? { selectedCaptionTrackId } : {}),
    ...(selectedCaptionMediaId ? { selectedCaptionMediaId } : {}),
    ...(selectedCaptionVideoTrackId ? { selectedCaptionVideoTrackId } : {}),
  };
}
