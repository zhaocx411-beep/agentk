import type { Operation, TimelineDoc } from "./types";
import { autoCaptionTrackForMedia } from "../captionTracks";

/** 删除素材及其全部时间线实例。removeItems 必须排在 removeMedia 前，
 * 以满足前后端 STILL_REFERENCED 约束并保持单次原子提交。 */
export function buildDeleteMediaOps(doc: TimelineDoc, mediaId: string): Operation[] {
  const itemIds = doc.items
    .filter((item) => item.mediaId === mediaId)
    .map((item) => item.itemId);
  const captionTrack = autoCaptionTrackForMedia(doc, mediaId);
  return [
    ...(itemIds.length > 0 ? [{ op: "removeItems" as const, itemIds }] : []),
    ...(captionTrack
      ? [{ op: "removeTrack" as const, trackId: captionTrack.trackId }]
      : []),
    { op: "removeMedia", mediaId },
  ];
}
