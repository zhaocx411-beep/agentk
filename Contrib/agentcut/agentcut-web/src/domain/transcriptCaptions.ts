import type {
  CaptionInput,
  Item,
  Media,
  Operation,
  TimelineDoc,
  Track,
  TranscriptSegment,
} from "./timeline/types";
import { autoCaptionTrackId } from "./captionTracks";
import { millisecondsToFrame, safeFps } from "./timeline/time";
import { effectiveSpeed, sourceFramesConsumed } from "./timeline/types";

const AUTO_CAPTION_TRACK_NAME = "自动字幕";

export function transcriptSegmentsToCaptions(
  segments: TranscriptSegment[],
  fps: number,
): CaptionInput[] {
  const safeFPS = safeFps(fps);
  return segments.flatMap((segment) => {
    const text = segment.text.trim();
    if (!text || segment.startMs < 0 || segment.endMs < segment.startMs) return [];
    const from = millisecondsToFrame(segment.startMs, safeFPS);
    const end = Math.max(from + 1, millisecondsToFrame(segment.endMs, safeFPS));
    return [{ from, durationInFrames: end - from, text }];
  });
}

export function buildTranscriptCaptionOps(doc: TimelineDoc, media: Media): Operation[] {
  const segments = media.transcript?.segments ?? [];
  const sourceItems = doc.items.filter(
    (item) => item.mediaId === media.mediaId && (item.kind === "video" || item.kind === "audio"),
  );
  const captions = normalizeCaptionOverlaps(
    sourceItems.flatMap((item) =>
      transcriptSegmentsForItem(segments, item, doc.metadata.fps),
    ),
  );
  if (captions.length === 0) return [];

  const trackID = autoCaptionTrackId(doc, media.mediaId);
  const track = doc.tracks.find(
    (candidate) =>
      candidate.trackId === trackID && candidate.kind === "caption",
  );
  if (track) {
    if (track.locked) return [];
    const existingCaptions = doc.items
      .filter((item) => item.trackId === track.trackId && item.kind === "text")
      .sort((a, b) => a.from - b.from);
    const styledCaptions = captions.map((caption) => {
      const existing =
        existingCaptions.find(
          (item) =>
            item.from < caption.from + caption.durationInFrames &&
            caption.from < item.from + item.durationInFrames,
        ) ?? existingCaptions[0];
      return {
        ...caption,
        stylePresetId: existing?.stylePresetId,
        captionStyle: existing?.captionStyle
          ? structuredClone(existing.captionStyle)
          : undefined,
      };
    });
    return [{ op: "setCaptions", trackId: track.trackId, captions: styledCaptions }];
  }
  const newTrack: Track = {
    trackId: trackID,
    kind: "caption",
    name: `${AUTO_CAPTION_TRACK_NAME}：${mediaLabel(media)}`,
  };
  return [
    { op: "addTrack", track: newTrack },
    { op: "setCaptions", trackId: newTrack.trackId, captions },
  ];
}

export function transcriptSegmentsForItem(
  segments: TranscriptSegment[],
  item: Item,
  fps: number,
): CaptionInput[] {
  const speed = effectiveSpeed(item);
  const visibleSourceStart = item.trimStart ?? 0;
  const visibleSourceEnd = visibleSourceStart + sourceFramesConsumed(item);
  return segments.flatMap((segment) => {
    const text = segment.text.trim();
    if (!text || segment.startMs < 0 || segment.endMs <= segment.startMs) return [];
    const sourceStart = millisecondsToFrame(segment.startMs, fps);
    const sourceEnd = millisecondsToFrame(segment.endMs, fps);
    const clippedStart = Math.max(sourceStart, visibleSourceStart);
    const clippedEnd = Math.min(sourceEnd, visibleSourceEnd);
    if (clippedEnd <= clippedStart) return [];
    const from =
      item.from + Math.round((clippedStart - visibleSourceStart) / speed);
    const end = Math.min(
      item.from + item.durationInFrames,
      item.from + Math.round((clippedEnd - visibleSourceStart) / speed),
    );
    if (end <= from) return [];
    return [{ from, durationInFrames: end - from, text }];
  });
}

function normalizeCaptionOverlaps(captions: CaptionInput[]): CaptionInput[] {
  const sorted = captions.toSorted((a, b) => a.from - b.from);
  const normalized: CaptionInput[] = [];
  for (const caption of sorted) {
    const previous = normalized.at(-1);
    const from = previous
      ? Math.max(caption.from, previous.from + previous.durationInFrames)
      : caption.from;
    const end = caption.from + caption.durationInFrames;
    if (end <= from) continue;
    normalized.push({ ...caption, from, durationInFrames: end - from });
  }
  return normalized;
}

function mediaLabel(media: Media): string {
  if (media.displayName?.trim()) return media.displayName.trim().slice(0, 80);
  const blobName = media.blobKey?.split("/").pop();
  if (blobName) return blobName.slice(0, 80);
  return { video: "视频", audio: "音频", image: "图片" }[media.kind];
}
