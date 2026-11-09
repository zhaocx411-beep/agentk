import type { Item, TimelineDoc, Track } from "./timeline/types";
import { itemEnd } from "./timeline/types";

const AUTO_CAPTION_PREFIX = "cauto_";

function fnv1a(value: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}

/** Stable base ID shared by caption generation and UI projection. */
export function autoCaptionTrackBaseId(mediaId: string): string {
  return `${AUTO_CAPTION_PREFIX}${fnv1a(mediaId).toString(36)}`;
}

export function isAutoCaptionTrackId(trackId: string): boolean {
  return trackId.startsWith(AUTO_CAPTION_PREFIX);
}

function candidateRank(trackId: string, base: string): number | null {
  if (trackId === base) return 1;
  const prefix = `${base}_`;
  if (!trackId.startsWith(prefix)) return null;
  const suffix = trackId.slice(prefix.length);
  const rank = Number(suffix);
  return Number.isInteger(rank) && rank >= 2 && String(rank) === suffix
    ? rank
    : null;
}

/** Reuses an existing auto track, including collision-suffixed IDs. */
export function autoCaptionTrackId(doc: TimelineDoc, mediaId: string): string {
  const base = autoCaptionTrackBaseId(mediaId);
  for (let rank = 1; ; rank += 1) {
    const candidate = rank === 1 ? base : `${base}_${rank}`;
    const existing = doc.tracks.find((track) => track.trackId === candidate);
    if (!existing || existing.kind === "caption") return candidate;
  }
}

export function autoCaptionTrackForMedia(
  doc: TimelineDoc,
  mediaId: string,
): Track | undefined {
  const base = autoCaptionTrackBaseId(mediaId);
  return doc.tracks
    .filter(
      (track) =>
        track.kind === "caption" && candidateRank(track.trackId, base) !== null,
    )
    .toSorted(
      (a, b) =>
        candidateRank(a.trackId, base)! - candidateRank(b.trackId, base)!,
    )[0];
}

/** Resolves an internal auto-caption track back to its source media. */
export function autoCaptionMediaIdForTrack(
  doc: TimelineDoc,
  trackId: string,
): string | undefined {
  const track = doc.tracks.find((candidate) => candidate.trackId === trackId);
  if (track?.kind !== "caption") return undefined;
  return doc.media.find(
    (media) =>
      candidateRank(trackId, autoCaptionTrackBaseId(media.mediaId)) !== null,
  )?.mediaId;
}

export interface CaptionProjection {
  captionTrack: Track;
  item: Item;
  mediaId?: string;
  sourceItemIds: string[];
}

function overlaps(a: Item, b: Item): boolean {
  return a.from < itemEnd(b) && b.from < itemEnd(a);
}

function fallbackVideoTrackId(doc: TimelineDoc): string | undefined {
  const videoTrackIds = new Set(
    doc.tracks
      .filter((track) => track.kind === "video")
      .map((track) => track.trackId),
  );
  return (
    doc.items.find(
      (item) => item.kind === "video" && videoTrackIds.has(item.trackId),
    )?.trackId ?? doc.tracks.find((track) => track.kind === "video")?.trackId
  );
}

/**
 * Resolves authoritative caption items back to their source video track.
 * Auto tracks follow every matching media instance; legacy/manual tracks use
 * the first video track. The timeline renders the authoritative C track itself.
 */
export function captionProjectionsForVideoTrack(
  doc: TimelineDoc,
  videoTrackId: string,
): CaptionProjection[] {
  const sourceItems = doc.items.filter(
    (item) => item.trackId === videoTrackId && item.kind === "video",
  );
  const fallbackTrackId = fallbackVideoTrackId(doc);
  const projections: CaptionProjection[] = [];

  for (const captionTrack of doc.tracks) {
    if (captionTrack.kind !== "caption") continue;
    const mediaId = autoCaptionMediaIdForTrack(doc, captionTrack.trackId);
    if (!mediaId && isAutoCaptionTrackId(captionTrack.trackId)) continue;
    if (!mediaId && videoTrackId !== fallbackTrackId) continue;
    const matchingSources = mediaId
      ? sourceItems.filter((item) => item.mediaId === mediaId)
      : sourceItems;
    if (mediaId && matchingSources.length === 0) continue;

    for (const item of doc.items) {
      if (item.trackId !== captionTrack.trackId || item.kind !== "text") continue;
      const sourceItemIds = matchingSources
        .filter((source) => overlaps(source, item))
        .map((source) => source.itemId);
      if (sourceItemIds.length === 0) continue;
      projections.push({ captionTrack, item, mediaId, sourceItemIds });
    }
  }

  return projections.toSorted(
    (a, b) => a.item.from - b.item.from || a.item.itemId.localeCompare(b.item.itemId),
  );
}

export function captionTracksForVideoTrack(
  doc: TimelineDoc,
  videoTrackId: string,
): Track[] {
  const byId = new Map<string, Track>();
  for (const projection of captionProjectionsForVideoTrack(doc, videoTrackId)) {
    byId.set(projection.captionTrack.trackId, projection.captionTrack);
  }
  return [...byId.values()];
}

export function mainTimelineTracks(doc: TimelineDoc): Track[] {
  const captionTracks = doc.tracks.filter((track) => track.kind === "caption");
  const videoTracks = doc.tracks.filter((track) => track.kind === "video").toReversed();
  const audioTracks = doc.tracks.filter((track) => track.kind === "audio");
  return [...captionTracks, ...videoTracks, ...audioTracks];
}
