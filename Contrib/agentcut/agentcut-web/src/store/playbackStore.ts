// 播放头状态：Preview 和 Timeline 共享同一个时钟，这样 Timeline 上的播放头线
// 和 Preview 里实际播放的帧号永远一致。故意不在这里存 durationInFrames——
// 那是 timelineStore.localDoc 的派生值，存在这里容易和文档状态漂移。
import { create } from "zustand";

import { normalizeFrame } from "@/domain/timeline/time";

export interface PlaybackState {
  /** 当前播放位置（文档 fps 的整数帧空间）。 */
  currentFrame: number;
  isPlaying: boolean;
  /**
   * 素材源预览：素材库里点击某条素材时设置，预览窗口切换为直接播放该
   * 素材原片（原生 <video controls>），不经过时间线。null = 时间线预览。
   * 只存 mediaId 不存 Media 对象，渲染时从 localDoc.media 现查，避免持有
   * 已被删除素材的陈旧副本。
   */
  sourcePreviewMediaId: string | null;
  setCurrentFrame(frame: number): void;
  play(): void;
  pause(): void;
  togglePlay(): void;
  setSourcePreview(mediaId: string | null): void;
}

export const usePlaybackStore = create<PlaybackState>((set) => ({
  currentFrame: 0,
  isPlaying: false,
  sourcePreviewMediaId: null,
  // 定位播放头（点标尺/拖进度条）同样回到时间线预览。
  setCurrentFrame: (frame) =>
    set({ currentFrame: normalizeFrame(frame), sourcePreviewMediaId: null }),
  // 时间线开始播放/定位时自动退出源预览，两种模式不并存。
  play: () => set({ isPlaying: true, sourcePreviewMediaId: null }),
  pause: () => set({ isPlaying: false }),
  togglePlay: () =>
    set((s) => ({ isPlaying: !s.isPlaying, sourcePreviewMediaId: null })),
  setSourcePreview: (mediaId) =>
    set({ sourcePreviewMediaId: mediaId, isPlaying: false }),
}));
