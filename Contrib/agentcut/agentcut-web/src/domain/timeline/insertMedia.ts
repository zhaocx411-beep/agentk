// 把一条素材插入到时间线的共享逻辑，被 TimelinePanel 的拖放 onDrop 与
// MediaLibrary 的"添加到时间线"菜单项共用，保证两处算出的 addItem op 一致。
import type {
  ItemKind,
  Media,
  Operation,
  Track,
  TimelineDoc,
  TrackKind,
} from "./types";
import { secondsToFrame } from "./time";
import { itemEnd, itemKindAllowedOn } from "./types";

const DEFAULT_IMAGE_SECONDS = 3;
const FALLBACK_MEDIA_SECONDS = 3;

/** 新 item 的时间线占用帧数：图片固定给 3 秒；video/audio 用素材探测出的
 *  时长，缺失时同样兜底 3 秒（比如探测失败但用户仍想先占位）。 */
export function durationInFramesForNewItem(media: Media, fps: number): number {
  return media.kind === "image"
    ? secondsToFrame(DEFAULT_IMAGE_SECONDS, fps)
    : media.durationInFrames || secondsToFrame(FALLBACK_MEDIA_SECONDS, fps);
}

/** 找第一条能容纳该素材类别、且未锁定的轨道（"添加到时间线"菜单项用，
 *  拖放插入是用户主动选轨道，不走这个函数）。 */
export function findDefaultTrackForMedia(doc: TimelineDoc, kind: Media["kind"]): Track | undefined {
  return doc.tracks.find((track) => !track.locked && itemKindAllowedOn(kind as ItemKind, track.kind));
}

export function trackKindForMedia(kind: Media["kind"]): TrackKind {
  return kind === "audio" ? "audio" : "video";
}

function generatedTrackId(): string {
  return `trk_${crypto.randomUUID().replaceAll("-", "").slice(0, 12)}`;
}

function newTrackForMedia(media: Media, trackId = generatedTrackId()): Track {
  const kind = trackKindForMedia(media.kind);
  return {
    trackId,
    kind,
    name: `${kind === "audio" ? "音频" : "视频"}轨`,
    createdBy: "auto",
  };
}

function overlaps(from: number, durationInFrames: number, itemFrom: number, itemDuration: number): boolean {
  return from < itemFrom + itemDuration && itemFrom < from + durationInFrames;
}

export function trackAcceptsRange(
  doc: TimelineDoc,
  track: Track,
  itemKind: ItemKind,
  from: number,
  durationInFrames: number,
  excludeItemId?: string,
): boolean {
  return (
    !track.locked &&
    itemKindAllowedOn(itemKind, track.kind) &&
    !doc.items.some(
      (item) =>
        item.trackId === track.trackId &&
        item.itemId !== excludeItemId &&
        overlaps(from, durationInFrames, item.from, item.durationInFrames),
    )
  );
}

function trackInsertIndex(doc: TimelineDoc, kind: TrackKind): number {
  if (kind === "video") {
    let lastVideo = -1;
    for (let index = 0; index < doc.tracks.length; index += 1) {
      if (doc.tracks[index]?.kind === "video") lastVideo = index;
    }
    return lastVideo + 1;
  }
  let lastAudio = -1;
  for (let index = 0; index < doc.tracks.length; index += 1) {
    if (doc.tracks[index]?.kind === "audio") lastAudio = index;
  }
  return lastAudio >= 0 ? lastAudio + 1 : doc.tracks.length;
}

export interface InsertMediaPlan {
  operations: Operation[];
  track: Track;
  createdTrack: boolean;
}

export function buildInsertMediaOps(
  doc: TimelineDoc,
  media: Media,
  from: number,
  preferredTrackId?: string,
  forceNewTrack = false,
  trackId?: string,
): InsertMediaPlan {
  const itemKind = media.kind as ItemKind;
  const durationInFrames = durationInFramesForNewItem(media, doc.metadata.fps);
  const preferred = doc.tracks.find((track) => track.trackId === preferredTrackId);
  let track =
    !forceNewTrack &&
    preferred &&
    trackAcceptsRange(doc, preferred, itemKind, from, durationInFrames)
      ? preferred
      : undefined;
  track ??= !forceNewTrack
    ? doc.tracks.find((candidate) =>
        trackAcceptsRange(doc, candidate, itemKind, from, durationInFrames),
      )
    : undefined;
  const createdTrack = !track;
  track ??= newTrackForMedia(media, trackId);
  const addItem = buildAddMediaItemOp(media, track, from, doc.metadata.fps);
  return {
    track,
    createdTrack,
    operations: createdTrack
      ? [
          {
            op: "addTrack",
            track,
            index: trackInsertIndex(doc, track.kind),
          },
          addItem,
        ]
      : [addItem],
  };
}

/** 轨道上最后一个 item 的结束帧（空轨道为 0），用于把新 item 接到轨道末尾。 */
export function trackEndFrame(doc: TimelineDoc, trackId: string): number {
  return doc.items
    .filter((item) => item.trackId === trackId)
    .reduce((max, item) => Math.max(max, itemEnd(item)), 0);
}

/** 生成新 item 的 ID（前端本地生成，服务端不做二次分配）。 */
export function newItemId(): string {
  return `it_${crypto.randomUUID().replaceAll("-", "").slice(0, 12)}`;
}

/** 构造把素材放到指定轨道 from 位置的 addItem op。 */
export function buildAddMediaItemOp(
  media: Media,
  track: Track,
  from: number,
  fps: number,
): Operation {
  return {
    op: "addItem",
    item: {
      itemId: newItemId(),
      trackId: track.trackId,
      kind: media.kind as ItemKind,
      mediaId: media.mediaId,
      from,
      durationInFrames: durationInFramesForNewItem(media, fps),
    },
  };
}

/**
 * 把素材追加到默认兼容轨道；不存在未锁定兼容轨道时，在同一原子批次中
 * 先创建轨道再插入素材，避免要求用户手动搭建空时间线。
 */
export function buildAppendMediaOps(doc: TimelineDoc, media: Media): Operation[] {
  const existing = findDefaultTrackForMedia(doc, media.kind);
  const track = existing ?? newTrackForMedia(media);
  const from = existing ? trackEndFrame(doc, track.trackId) : 0;
  const addItem = buildAddMediaItemOp(media, track, from, doc.metadata.fps);
  return existing
    ? [addItem]
    : [
        { op: "addTrack", track, index: trackInsertIndex(doc, track.kind) },
        addItem,
      ];
}
