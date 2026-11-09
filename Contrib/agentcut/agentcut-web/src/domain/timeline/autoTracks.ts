import { applyOps } from "./applyOps";
import type { Operation, TimelineDoc } from "./types";

export function emptyAutoTrackIds(doc: TimelineDoc): string[] {
  const baseVideoTrackId = doc.tracks.find((track) => track.kind === "video")?.trackId;
  const occupied = new Set(doc.items.map((item) => item.trackId));
  return doc.tracks
    .filter(
      (track) =>
        (track.kind === "video" || track.kind === "audio") &&
        track.createdBy === "auto" &&
        !track.locked &&
        track.trackId !== baseVideoTrackId &&
        !occupied.has(track.trackId),
    )
    .map((track) => track.trackId);
}

/**
 * Auto tracks are cleaned in the same user transaction after its primary
 * operations have reached a valid document. Manual/legacy/locked tracks and
 * the base video track are never removed.
 */
export function withAutoTrackCleanup(
  doc: TimelineDoc,
  operations: Operation[],
): Operation[] {
  if (operations.length === 0) return operations;
  let next: TimelineDoc;
  try {
    next = applyOps(doc, operations).doc;
  } catch {
    return operations;
  }
  const cleanup = emptyAutoTrackIds(next);
  if (cleanup.length === 0) return operations;
  return [
    ...operations,
    ...cleanup.map((trackId): Operation => ({ op: "removeTrack", trackId })),
  ];
}
