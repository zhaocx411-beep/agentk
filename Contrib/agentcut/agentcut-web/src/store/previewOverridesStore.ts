import { create } from "zustand";

import type { ItemPatch, TrackPatch } from "@/domain/timeline/types";

interface PreviewOverridesState {
  itemPatches: Record<string, ItemPatch>;
  trackVolumes: Record<string, number>;
  trackPatches: Record<string, TrackPatch>;
  cropEditingItemId: string | null;
  setItemPatch(itemId: string, patch: ItemPatch): void;
  clearItemPatch(itemId: string): void;
  setTrackVolume(trackId: string, volume: number): void;
  clearTrackVolume(trackId: string): void;
  setTrackPatch(trackId: string, patch: TrackPatch): void;
  clearTrackPatch(trackId: string): void;
  setCropEditingItem(itemId: string | null): void;
  reset(): void;
}

export const usePreviewOverridesStore = create<PreviewOverridesState>((set) => ({
  itemPatches: {},
  trackVolumes: {},
  trackPatches: {},
  cropEditingItemId: null,
  setItemPatch(itemId, patch) {
    set((state) => {
      const previous = state.itemPatches[itemId];
      return {
        itemPatches: {
          ...state.itemPatches,
          [itemId]: {
            ...previous,
            ...patch,
            ...(previous?.transform && patch.transform
              ? {
                  transform: {
                    ...previous.transform,
                    ...patch.transform,
                  },
                }
              : {}),
          },
        },
      };
    });
  },
  clearItemPatch(itemId) {
    set((state) => {
      const itemPatches = { ...state.itemPatches };
      delete itemPatches[itemId];
      return { itemPatches };
    });
  },
  setTrackVolume(trackId, volume) {
    set((state) => ({
      trackVolumes: { ...state.trackVolumes, [trackId]: volume },
      trackPatches: {
        ...state.trackPatches,
        [trackId]: { ...state.trackPatches[trackId], volume },
      },
    }));
  },
  clearTrackVolume(trackId) {
    set((state) => {
      const trackVolumes = { ...state.trackVolumes };
      delete trackVolumes[trackId];
      const trackPatches = { ...state.trackPatches };
      if (trackPatches[trackId]) {
        const { volume: _volume, ...remaining } = trackPatches[trackId];
        if (Object.keys(remaining).length === 0) delete trackPatches[trackId];
        else trackPatches[trackId] = remaining;
      }
      return { trackVolumes, trackPatches };
    });
  },
  setTrackPatch(trackId, patch) {
    set((state) => ({
      trackPatches: {
        ...state.trackPatches,
        [trackId]: { ...state.trackPatches[trackId], ...patch },
      },
    }));
  },
  clearTrackPatch(trackId) {
    set((state) => {
      const trackPatches = { ...state.trackPatches };
      delete trackPatches[trackId];
      const trackVolumes = { ...state.trackVolumes };
      delete trackVolumes[trackId];
      return { trackPatches, trackVolumes };
    });
  },
  setCropEditingItem(itemId) {
    set({ cropEditingItemId: itemId });
  },
  reset() {
    set({
      itemPatches: {},
      trackVolumes: {},
      trackPatches: {},
      cropEditingItemId: null,
    });
  },
}));

