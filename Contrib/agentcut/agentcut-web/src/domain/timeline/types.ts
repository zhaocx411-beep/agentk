// 与后端 timeline 包（timeline/doc.go、operation.go、errors.go、limits.go）
// 逐字段对齐的手工翻译。阶段 A 先手写；后续切换为 JSON Schema 生成（设计文档第 7 节），
// 届时本文件禁止手写修改。
//
// 对齐约定：
// - Go 的 int64 帧坐标 → TS number（帧数远小于 2^53，安全）；
// - Go 的 `omitempty` 可选字段 → TS 可选属性（?），零值省略；
// - Go 零值语义保留：Speed/Volume 为 0 或缺省时按 1 处理（见 effectiveSpeed）。

// ---- doc.go ----

/** 当前唯一接受的文档版本（timeline.SchemaVersion）。 */
export const SCHEMA_VERSION = 1;

export type MediaKind = "video" | "audio" | "image";
export type TrackKind = "video" | "audio" | "caption";
export type ItemKind = "video" | "audio" | "image" | "text";

export interface Metadata {
  name?: string;
  width: number;
  height: number;
  fps: number;
  backgroundColor?: string;
}

export interface MediaProbe {
  durationMs?: number;
  width?: number;
  height?: number;
  nativeFps?: number;
  hasAudio?: boolean;
  codec?: string;
}

export interface TranscriptWord {
  wordId?: string;
  startMs: number;
  endMs: number;
  text: string;
}

export interface TranscriptSegment {
  segmentId?: string;
  startMs: number;
  endMs: number;
  text: string;
  words?: TranscriptWord[];
}

export interface Transcript {
  revision?: number;
  language?: string;
  segments: TranscriptSegment[];
}

export type ProcessingState = "pending" | "running" | "succeeded" | "skipped" | "failed";

export interface MediaProcessing {
  status: "processing" | "ready" | "partial" | "failed";
  readyForTimeline: boolean;
  probe: ProcessingState;
  thumbnail: ProcessingState;
  waveform: ProcessingState;
  transcript: ProcessingState;
  understanding: ProcessingState;
  error?: string;
}

export interface Media {
  mediaId: string;
  /** 后端分配的素材代次；删除后复用同一 ID 时递增，用于隔离异步回填。 */
  generation?: number;
  uri: string;
  blobKey?: string;
  kind: MediaKind;
  /** 用户可编辑的展示名（updateMedia 写入）；缺省时按 blobKey 文件名兜底。 */
  displayName?: string;
  /** 素材总长换算到文档 fps 后的帧数。图片为 0（可无限拉伸）。 */
  durationInFrames: number;
  probe?: MediaProbe;
  transcript?: Transcript;
  /** 上传时由 cut-apiserver 本地 ffmpeg 生成的缩略图 URL（视频为多帧横向
   *  sprite，图片为单帧缩放）；缺失表示生成失败/不适用，纯展示层数据。 */
  thumbnailUri?: string;
  /** 按固定桶数（WAVEFORM_BUCKET_COUNT）均分整段素材时长后，每桶归一化到
   *  0~1 的振幅峰值；纯展示层数据。 */
  waveformPeaks?: number[];
  processing?: MediaProcessing;
  understandingText?: string;
}

/** 与后端 platform/media.WaveformBucketCount 对齐。 */
export const WAVEFORM_BUCKET_COUNT = 200;
/** 与后端 platform/media.ThumbnailFrameCount 对齐。 */
export const THUMBNAIL_FRAME_COUNT = 10;

export interface Track {
  trackId: string;
  kind: TrackKind;
  name?: string;
  /** 自动轨道可在变空且未锁定时清理；用户轨道会保留。 */
  createdBy?: "auto" | "user";
  /** 为真时 user/agent 均不能改本轨内容。 */
  locked?: boolean;
  muted?: boolean;
  /** 仅视频轨：隐藏画面但保留该轨内嵌音频。 */
  hidden?: boolean;
  /** 轨道级增益（0~2，0/缺省表示未设置按 1 处理）。 */
  volume?: number;
  ducking?: DuckingConfig;
}

export interface DuckingConfig {
  sidechainTrackIds: string[];
  gain: number;
  attackFrames: number;
  releaseFrames: number;
}

export interface NormalizedRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ItemTransform {
  x: number;
  y: number;
  width: number;
  height: number;
  /** 0/缺省表示历史默认值 1。 */
  opacity?: number;
  fit: "contain" | "cover";
  crop?: NormalizedRect;
  rotationDegrees?: number;
}

export type CaptionFontFamily = "default" | "sans" | "serif" | "mono";
export type CaptionAlign = "left" | "center" | "right";

/** 字段级 preset override；缺省字段继续继承 preset/后端默认值。 */
export interface CaptionStyle {
  fontFamily?: CaptionFontFamily;
  fontSize?: number;
  textColor?: string;
  backgroundColor?: string;
  outlineColor?: string;
  outlineWidth?: number;
  bold?: boolean;
  italic?: boolean;
  align?: CaptionAlign;
  x?: number;
  y?: number;
  maxWidth?: number;
  lineHeight?: number;
}

export interface Item {
  itemId: string;
  trackId: string;
  kind: ItemKind;
  /** 对 video/audio/image 必填，对 text 必须为空。 */
  mediaId?: string;
  /** 片段在时间线上的起始帧（文档 fps 帧空间）。 */
  from: number;
  /** 片段在时间线上占据的帧数。 */
  durationInFrames: number;
  /** 源素材被裁掉的开头帧数（同样按文档 fps 表达）。 */
  trimStart?: number;
  /** 播放速率（0/缺省表示未设置按 1 处理）。源消耗量 = durationInFrames * speed。 */
  speed?: number;
  /** 片段级增益（0~2，0/缺省表示未设置按 1 处理）。 */
  volume?: number;
  muted?: boolean;
  fadeInFrames?: number;
  fadeOutFrames?: number;
  transform?: ItemTransform;
  /** 仅 text item 使用。 */
  text?: string;
  stylePresetId?: string;
  captionStyle?: CaptionStyle;
}

export type TransitionKind = "crossfade" | "dipToBlack";

export interface Transition {
  transitionId: string;
  fromItemId: string;
  toItemId: string;
  kind: TransitionKind;
  frames: number;
}

export interface TimelineDoc {
  schemaVersion: number;
  metadata: Metadata;
  media: Media[];
  tracks: Track[];
  items: Item[];
  transitions?: Transition[];
}

// ---- limits.go ----

export const MAX_MEDIA = 500;
export const MAX_TRACKS = 64;
export const MAX_ITEMS = 5000;
export const MAX_TRANSITIONS = 5000;
export const MAX_OPS_PER_BATCH = 200;
/** 序列化后文档体积上限（4 MiB）。 */
export const MAX_DOC_BYTES = 4 << 20;

export const ALLOWED_FPS: ReadonlySet<number> = new Set([24, 25, 30, 50, 60]);
export const DEFAULT_FPS = 30;

// ---- operation.go ----

/** updateItem 的白名单 patch 字段。undefined 表示不修改。 */
export interface ItemPatch {
  speed?: number;
  volume?: number;
  muted?: boolean;
  fadeInFrames?: number;
  fadeOutFrames?: number;
  transform?: ItemTransform;
  text?: string;
  stylePresetId?: string;
  captionStyle?: CaptionStyle;
}

/** updateTrack 的白名单 patch 字段。 */
export interface TrackPatch {
  name?: string;
  locked?: boolean;
  muted?: boolean;
  hidden?: boolean;
  volume?: number;
  ducking?: DuckingConfig;
}

export interface TransitionPatch {
  kind?: TransitionKind;
  frames?: number;
}

/** setCaptions 的一条字幕；item ID 由服务端确定性生成。 */
export interface CaptionInput {
  from: number;
  durationInFrames: number;
  text: string;
  stylePresetId?: string;
  captionStyle?: CaptionStyle;
}

/** updateMedia 的白名单 patch 字段。 */
export interface MediaPatch {
  displayName?: string;
}

/** setMetadata 的白名单字段。 */
export interface MetadataPatch {
  name?: string;
  width?: number;
  height?: number;
  fps?: number;
  backgroundColor?: string;
}

// Operation 判别联合。后端 Go 侧是扁平结构（operation.go 的 Operation struct，
// 所有字段 omitempty），JSON 形状与这里的判别联合完全兼容：序列化后每个成员
// 只带自己需要的字段，`op` 是判别式。
export type Operation =
  | { op: "addMedia"; media: Media }
  | { op: "removeMedia"; mediaId: string }
  | { op: "updateMedia"; mediaId: string; mediaPatch: MediaPatch }
  | { op: "setTranscript"; mediaId: string; transcript: Transcript }
  | { op: "addItem"; item: Item }
  | { op: "removeItems"; itemIds: string[] }
  | { op: "split"; itemId: string; frame: number; newItemId?: string }
  | { op: "trimStart"; itemId: string; frame: number }
  | { op: "trimEnd"; itemId: string; frame: number }
  | { op: "moveItem"; itemId: string; from: number; trackId?: string }
  | { op: "updateItem"; itemId: string; patch: ItemPatch }
  | {
      op: "rippleDelete";
      fromFrame: number;
      toFrame: number;
      /** 作用的轨道；空 = 所有未锁定轨道。 */
      trackIds?: string[];
    }
  | { op: "addTrack"; track: Track; index?: number }
  | { op: "removeTrack"; trackId: string }
  | { op: "updateTrack"; trackId: string; trackPatch: TrackPatch }
  | { op: "setCaptions"; trackId: string; captions: CaptionInput[] }
  | { op: "setMetadata"; metadata: MetadataPatch }
  | { op: "addTransition"; transition: Transition }
  | { op: "updateTransition"; transitionId: string; transitionPatch: TransitionPatch }
  | { op: "removeTransition"; transitionId: string };

export type OpName = Operation["op"];

/** 汇报一批 Ops 实际改动了什么，用于前端高亮与 Agent 汇报。 */
export interface ChangeSummary {
  addedItemIds?: string[];
  removedItemIds?: string[];
  updatedItemIds?: string[];
  addedTrackIds?: string[];
  removedTrackIds?: string[];
  updatedTrackIds?: string[];
  addedMediaIds?: string[];
  removedMediaIds?: string[];
  updatedMediaIds?: string[];
  addedTransitionIds?: string[];
  removedTransitionIds?: string[];
  updatedTransitionIds?: string[];
  metadataChanged?: boolean;
}

// ---- errors.go ----

export type ErrorCode =
  | "BAD_REQUEST"
  | "NOT_FOUND"
  | "CONFLICTING_ID"
  | "OVERLAP"
  | "LOCKED_TRACK"
  | "OUT_OF_RANGE"
  | "KIND_MISMATCH"
  | "LIMIT_EXCEEDED"
  | "STILL_REFERENCED"
  | "UNSUPPORTED"
  | "FPS_LOCKED"
  | "INVALID_TRANSITION"
  | "INSUFFICIENT_HANDLE"
  | "INVALID_DUCKING"
  | "INVALID_CAPTION_STYLE"
  | "INVARIANT_VIOLATION";

/** 描述一批 Ops 中第 index 个操作失败。整批不生效。批级错误 index=-1、op 为空。 */
export class OpError extends Error {
  readonly index: number;
  readonly op: string;
  readonly code: ErrorCode;
  /** 裸错误信息（Go 侧 OpError.Message，不含 index/op/code 前缀）。 */
  readonly detail: string;

  constructor(index: number, op: string, code: ErrorCode, message: string) {
    super(
      index < 0
        ? `${code}: ${message}`
        : `op[${index}] ${op}: ${code}: ${message}`,
    );
    this.name = "OpError";
    this.index = index;
    this.op = op;
    this.code = code;
    this.detail = message;
  }
}

// ---- doc.go 的方法等价物（纯函数版） ----

/** 归一化速率：0/缺省按 1（Item.EffectiveSpeed）。 */
export function effectiveSpeed(it: Item): number {
  return it.speed === undefined || it.speed <= 0 ? 1 : it.speed;
}

/** 归一化增益：0/缺省按 1。 */
export function effectiveVolume(value?: number): number {
  return value === undefined || value === 0 ? 1 : value;
}

export function effectiveOpacity(it: Item): number {
  return !it.transform?.opacity ? 1 : it.transform.opacity;
}

/** 片段在时间线上的结束帧（开区间，Item.End）。 */
export function itemEnd(it: Item): number {
  return it.from + it.durationInFrames;
}

/** 全库统一的帧数四舍五入（roundFrames，Math.round 与 Go math.Round 对 .5 同为远离零……
 * 注意：Math.round(-0.5) = -0，Go math.Round(-0.5) = -1。领域内被 round 的值均为
 * 非负源帧数，负数入参只会出现在越界错误路径上，语义一致。 */
export function roundFrames(f: number): number {
  return f < 0 ? -Math.round(-f) : Math.round(f);
}

/** 片段消耗的源素材帧数（Item.SourceFramesConsumed）。 */
export function sourceFramesConsumed(it: Item): number {
  return roundFrames(it.durationInFrames * effectiveSpeed(it));
}

/** 返回一份空文档（NewDoc）。 */
export function newDoc(
  name: string,
  width: number,
  height: number,
  fps: number,
): TimelineDoc {
  return {
    schemaVersion: SCHEMA_VERSION,
    metadata: { name, width, height, fps },
    media: [],
    tracks: [],
    items: [],
  };
}

/** itemKindAllowedOn：item 类别能否放进 track 类别。 */
export function itemKindAllowedOn(item: ItemKind, track: TrackKind): boolean {
  switch (track) {
    case "video":
      return item === "video" || item === "image";
    case "audio":
      return item === "audio";
    case "caption":
      return item === "text";
  }
}

/** mediaKindForItem：item 类别要求的 media 类别；text 无要求返回 null。 */
export function mediaKindForItem(kind: ItemKind): MediaKind | null {
  switch (kind) {
    case "video":
      return "video";
    case "audio":
      return "audio";
    case "image":
      return "image";
    case "text":
      return null;
  }
}

/** 时间线总长（最右 item 的结束帧，DocDurationInFrames）。 */
export function docDurationInFrames(d: TimelineDoc): number {
  let max = 0;
  for (const it of d.items) {
    const end = itemEnd(it);
    if (end > max) max = end;
  }
  return max;
}

/** 深拷贝文档（Doc.Clone，ApplyOps 的输入保护）。 */
export function cloneDoc(d: TimelineDoc): TimelineDoc {
  // 文档是纯 JSON 数据，structuredClone 足够且最不易漏字段。
  return structuredClone(d);
}
