/**
 * 结果字段展示顺序：短配置在前、素材在后，避免轮询时后端乱序把页面顶来顶去
 */
import type { ResultRowKind, ResultDisplayRow } from './flatten';

const RESULT_FIELD_ORDER = [
  'preserveOriginalAudio',
  'asrEnabled',
  'asrLanguage',
  'size',
  'aspect',
  'subtitleFormat',
  'subtitles',
  'voiceId',
  'provider',
  'tts',
  'theme',
  'instructions',
  'narrationVolume',
  'originalAudioVolume',
  'width',
  'height',
  'clipCount',
  'durationSeconds',
  'sourceStartSeconds',
  'sourceDurationSeconds',
  'id',
  'models',
  'analysisOptions',
  'videoUri',
  'sourceVideoUri',
  'videoUrl',
  'videoUrls',
  'imageUri',
  'imageUrl',
  'imageUrls',
  'audioUri',
  'script',
  'segments',
];

const KIND_ORDER: Record<ResultRowKind, number> = {
  boolean: 0,
  text: 1,
  media: 2,
  mediaList: 3,
  json: 4,
};

function leafKey(key: string): string {
  return key.split('.').pop() ?? key;
}

function fieldOrderIndex(key: string): number {
  const index = RESULT_FIELD_ORDER.indexOf(leafKey(key));
  return index < 0 ? RESULT_FIELD_ORDER.length : index;
}

export function compareResultKeys(a: string, b: string): number {
  const byOrder = fieldOrderIndex(a) - fieldOrderIndex(b);
  return byOrder !== 0 ? byOrder : a.localeCompare(b);
}

export function compareResultRows(a: ResultDisplayRow, b: ResultDisplayRow): number {
  const byKind = KIND_ORDER[a.kind] - KIND_ORDER[b.kind];
  return byKind !== 0 ? byKind : compareResultKeys(a.key, b.key);
}

export function sortedObjectKeys(data: Record<string, unknown>): string[] {
  return Object.keys(data).sort(compareResultKeys);
}

export function sortedUrls(urls: string[]): string[] {
  return [...urls].sort((a, b) => a.localeCompare(b));
}
