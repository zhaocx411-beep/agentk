/**
 * 把执行入参 / 输出摊成可读字段，避免结果区直接倒 JSON
 */
import { compareResultRows, sortedObjectKeys, sortedUrls } from './order';

export type ResultRowKind = 'text' | 'boolean' | 'media' | 'mediaList' | 'json';
export type ResultMediaKind = 'image' | 'video' | 'audio';

export interface ResultDisplayRow {
  key: string;
  label: string;
  value: unknown;
  kind: ResultRowKind;
  copyable?: boolean;
  mediaType?: ResultMediaKind;
}

export interface FlattenResultOptions {
  skipKeys?: ReadonlySet<string>;
  mediaKeys?: ReadonlySet<string>;
  labelOf?: (key: string) => string;
}

export const RESULT_OUTPUT_PREVIEW_KEYS = new Set([
  'artifacts',
  'videoUrls',
  'imageUrls',
  'videoUri',
  'uri',
  'mediaArtifacts',
]);

export const RESULT_FIELD_LABELS: Record<string, string> = {
  videoUri: '原视频',
  sourceVideoUri: '原视频',
  videoUrls: '视频',
  imageUri: '图片',
  imageUrls: '图片',
  preserveOriginalAudio: '保留原声',
  size: '尺寸',
  subtitleFormat: '字幕格式',
  width: '宽度',
  height: '高度',
  clipCount: '片段数',
  durationSeconds: '时长',
  sourceStartSeconds: '起始秒',
  sourceDurationSeconds: '片长',
  id: '编号',
  voiceId: '音色',
};

const VIDEO_KEYS = new Set(['videoUri', 'sourceVideoUri', 'videoUrl', 'videoUrls']);
const IMAGE_KEYS = new Set(['imageUri', 'imageUrl', 'imageUrls']);
const AUDIO_KEYS = new Set(['audioUri', 'audioUrl', 'audioUrls']);
const MEDIA_EXT_VIDEO = /\.(mp4|mov|webm|m4v)(\?|$)/i;
const MEDIA_EXT_AUDIO = /\.(mp3|wav|m4a|aac)(\?|$)/i;
const MEDIA_EXT_IMAGE = /\.(png|jpe?g|gif|webp|avif)(\?|$)/i;

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

export function prettyJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}

function isPrimitive(value: unknown): value is string | number | boolean {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean';
}

function isShallowMap(value: Record<string, unknown>): boolean {
  return Object.values(value).every((item) => item == null || isPrimitive(item));
}

function looksLikeUrl(value: string): boolean {
  return /^(https?:)?\/\//.test(value) || value.startsWith('blob:') || value.startsWith('data:');
}

function leafKey(key: string): string {
  return key.split('.').pop() ?? key;
}

function mediaTypeOfValue(
  key: string,
  value: string,
  mediaKeys?: ReadonlySet<string>,
): ResultMediaKind | undefined {
  const leaf = leafKey(key);
  if (VIDEO_KEYS.has(leaf) || mediaKeys?.has(leaf)) return 'video';
  if (IMAGE_KEYS.has(leaf)) return 'image';
  if (AUDIO_KEYS.has(leaf)) return 'audio';
  if (MEDIA_EXT_VIDEO.test(value)) return 'video';
  if (MEDIA_EXT_AUDIO.test(value)) return 'audio';
  if (MEDIA_EXT_IMAGE.test(value)) return 'image';
  return undefined;
}

function resolveLabel(key: string, labelOf?: (name: string) => string): string {
  const custom = labelOf?.(key);
  if (custom && custom !== key) return custom;
  return RESULT_FIELD_LABELS[key] ?? key;
}

function composeLabel(parent: string | undefined, key: string, labelOf?: (name: string) => string): string {
  const self = resolveLabel(key, labelOf);
  if (!parent || self !== key) return self;
  return `${resolveLabel(parent, labelOf)} · ${self}`;
}

function isUrlList(value: unknown): value is string[] {
  return Array.isArray(value) && value.length > 0 && value.every((item) => typeof item === 'string' && looksLikeUrl(item));
}

function rowFromPrimitive(
  key: string,
  label: string,
  value: string | number | boolean,
  mediaKeys?: ReadonlySet<string>,
): ResultDisplayRow {
  if (typeof value === 'boolean') {
    return { key, label, value, kind: 'boolean' };
  }
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if ((trimmed.startsWith('{') || trimmed.startsWith('[')) && trimmed.length > 80) {
      try {
        return { key, label, value: JSON.parse(trimmed) as unknown, kind: 'json' };
      } catch {
        return { key, label, value, kind: 'json' };
      }
    }
    const mediaType = mediaTypeOfValue(key, value, mediaKeys);
    if (mediaType) {
      return { key, label, value, kind: 'media', mediaType };
    }
    return { key, label, value, kind: 'text', copyable: looksLikeUrl(value) };
  }
  return { key, label, value, kind: 'text' };
}

export function flattenResultRows(
  data: Record<string, unknown>,
  options: FlattenResultOptions = {},
): ResultDisplayRow[] {
  const { skipKeys, mediaKeys, labelOf } = options;
  const rows: ResultDisplayRow[] = [];

  const push = (key: string, label: string, value: unknown) => {
    if (value == null || value === '') return;

    if (isPlainObject(value) && isShallowMap(value)) {
      for (const childKey of sortedObjectKeys(value)) {
        const childValue = value[childKey];
        if (childValue == null || childValue === '') continue;
        const childPath = `${key}.${childKey}`;
        const childLabel = composeLabel(key, childKey, labelOf);
        if (isPrimitive(childValue)) {
          rows.push(rowFromPrimitive(childPath, childLabel, childValue, mediaKeys));
        }
      }
      return;
    }

    if (isPrimitive(value)) {
      rows.push(rowFromPrimitive(key, label, value, mediaKeys));
      return;
    }

    if (isUrlList(value)) {
      const urls = sortedUrls(value);
      const mediaType = mediaTypeOfValue(key, urls[0], mediaKeys) ?? 'video';
      rows.push({ key, label, value: urls, kind: 'mediaList', mediaType });
      return;
    }

    if (Array.isArray(value) && value.every((item) => isPrimitive(item))) {
      const text = value.map((item) => String(item)).join('、');
      if (text) rows.push({ key, label, value: text, kind: 'text' });
      return;
    }

    rows.push({ key, label, value, kind: 'json' });
  };

  for (const key of sortedObjectKeys(data)) {
    if (skipKeys?.has(key)) continue;
    push(key, composeLabel(undefined, key, labelOf), data[key]);
  }

  return rows.sort(compareResultRows);
}
