// MediaLibrary → TimelinePanel 拖拽的自定义 MIME type，两个组件是 App.tsx
// 下的兄弟节点（不是父子），用 DataTransfer 传递 mediaId/kind 而不是提到共享
// store（拖拽本身是短命的浏览器原生手势，不需要长期状态）。
export const MEDIA_DND_TYPE = "application/x-agentcut-media";

export interface MediaDragPayload {
  mediaId: string;
  kind: "video" | "audio" | "image";
}

/** Library transition resources use a distinct payload and are never media. */
export const TRANSITION_DND_TYPE = "application/x-agentcut-transition";

export interface TransitionDragPayload {
  kind: "none" | "crossfade" | "dipToBlack";
}

/** Drag-over data is protected in browsers, so marker MIME types expose only
 * the resource kind while the JSON payload remains authoritative on drop. */
export const TRANSITION_DND_KIND_TYPE = {
  none: "application/x-agentcut-transition-none",
  crossfade: "application/x-agentcut-transition-crossfade",
  dipToBlack: "application/x-agentcut-transition-dip-to-black",
} as const;
