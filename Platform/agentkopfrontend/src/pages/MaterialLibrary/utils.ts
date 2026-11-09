import type {
  DatasetTask,
  MaterialCategory,
  MaterialDramaFilter,
  MaterialFile,
  MaterialKind,
} from './types';
import { MATERIAL_DRAMA_ALL, MATERIAL_DRAMA_UNLABELED } from './types';
import { MATERIAL_CATEGORIES, materialCategoryKind } from './constants';

function asString(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value === 'string') {
    try {
      return asRecord(JSON.parse(value) as unknown);
    } catch {
      return null;
    }
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function asUrl(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  const record = asRecord(value);
  if (!record) return '';
  return asString(
    record.blobUrl ||
      record.url ||
      record.fileUrl ||
      record.cdnUrl ||
      record.src ||
      record.uri,
  );
}

function asUrlList(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(asUrl).filter(Boolean);
  const single = asUrl(value);
  return single ? [single] : [];
}

export function parseMediaType(value: unknown): MaterialKind | undefined {
  if (typeof value !== 'string' && typeof value !== 'number') return undefined;
  const raw = String(value).trim();
  if (!raw) return undefined;
  const key = raw.toLowerCase();
  if (key.startsWith('image') || key === 'img' || key === 'pic' || raw === '图片') return 'image';
  if (key.startsWith('audio') || key === 'sound' || key === 'voice' || raw === '音频') {
    return 'audio';
  }
  if (key.startsWith('video') || key === 'movie' || raw === '视频') return 'video';
  return undefined;
}

export function parseMaterialCategory(value: unknown): MaterialCategory | undefined {
  if (typeof value !== 'string' && typeof value !== 'number') return undefined;
  const raw = String(value).trim();
  if (!raw) return undefined;
  return (MATERIAL_CATEGORIES as readonly string[]).includes(raw)
    ? (raw as MaterialCategory)
    : undefined;
}

function urlsOfKind(extra: Record<string, unknown>, kind: MaterialKind): string[] {
  if (kind === 'image') return asUrlList(extra.images);
  if (kind === 'audio') return asUrlList(extra.audioUrls).concat(asUrlList(extra.audio_urls));
  return asUrlList(extra.videoUrls).concat(asUrlList(extra.video_urls));
}

/** 接口常不带 extraFields.mediaType，从 category 或 URL 字段推断 */
function inferMaterialKind(
  extra: Record<string, unknown>,
  category?: MaterialCategory,
): MaterialKind | undefined {
  const fromMediaType = parseMediaType(extra.mediaType);
  if (fromMediaType) return fromMediaType;
  if (category) return materialCategoryKind(category);

  if (asUrlList(extra.videoUrls).length || asUrlList(extra.video_urls).length) return 'video';
  if (asUrlList(extra.audioUrls).length || asUrlList(extra.audio_urls).length) return 'audio';
  if (asUrlList(extra.images).length) return 'image';
  return undefined;
}

/** 合并 extraFields 与 pipelineArgs.asset，避免媒体字段被嵌套 */
function resolveExtra(task: DatasetTask): Record<string, unknown> {
  const extra = asRecord(task.extraFields) ?? {};
  const pipelineArgs = asRecord(extra.pipelineArgs);
  const nested =
    asRecord(pipelineArgs?.asset) ?? asRecord(extra['pipelineArgs.asset']) ?? asRecord(extra.asset);
  return nested ? { ...extra, ...nested } : extra;
}

/** 解析 task → 可展示素材；kind 优先 mediaType，否则 category / URL 字段 */
export function flattenMaterialFiles(tasks: DatasetTask[]): MaterialFile[] {
  return tasks.flatMap((task) => {
    const extra = resolveExtra(task);
    const category =
      parseMaterialCategory(extra.category) ?? parseMaterialCategory(task.category);
    const kind = inferMaterialKind(extra, category);
    if (!kind) return [];
    const title = asString(extra.title);
    const drama = asString(extra.drama) || undefined;
    const episode = parseEpisode(extra.episode);
    const urls = urlsOfKind(extra, kind);
    const bytes = urls.length === 1 ? parseBytes(extra.bytes) : undefined;
    return urls.map((url, index) => ({
      id: `${task.id}-${kind}-${index}`,
      taskId: task.id,
      url,
      kind,
      category,
      title: title || undefined,
      drama,
      episode,
      bytes,
      createdAt: task.createdAt,
      updatedAt: task.updatedAt,
    }));
  });
}

function parseEpisode(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
}

/** extraFields.bytes：只认正整数，缺省不算 */
function parseBytes(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return Math.round(value);
  }
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    if (Number.isFinite(parsed) && parsed > 0) return Math.round(parsed);
  }
  return undefined;
}

export function listMaterialDramas(files: readonly MaterialFile[]): string[] {
  const names = new Set<string>();
  for (const file of files) {
    if (file.drama) names.add(file.drama);
  }
  return [...names].sort((left, right) => left.localeCompare(right, 'zh-CN'));
}

export function hasUnlabeledDrama(files: readonly MaterialFile[]): boolean {
  return files.some((file) => !file.drama);
}

export function filterMaterialFilesByDrama(
  files: readonly MaterialFile[],
  drama: MaterialDramaFilter,
): MaterialFile[] {
  if (drama === MATERIAL_DRAMA_ALL) return [...files];
  if (drama === MATERIAL_DRAMA_UNLABELED) return files.filter((file) => !file.drama);
  return files.filter((file) => file.drama === drama);
}

/** 同一剧名下按集数排列，方便连播挑选 */
export function sortDramaEpisodes(files: readonly MaterialFile[]): MaterialFile[] {
  return [...files].sort((left, right) => {
    const episodeLeft = left.episode ?? Number.POSITIVE_INFINITY;
    const episodeRight = right.episode ?? Number.POSITIVE_INFINITY;
    if (episodeLeft !== episodeRight) return episodeLeft - episodeRight;
    return (left.title ?? '').localeCompare(right.title ?? '', 'zh-CN');
  });
}

export function materialDramaSelectOptions(
  dramas: readonly string[],
  unlabeled: boolean,
): { label: string; value: MaterialDramaFilter }[] {
  return [
    { label: '全部剧名', value: MATERIAL_DRAMA_ALL },
    ...dramas.map((name) => ({ label: name, value: name })),
    ...(unlabeled ? [{ label: '未标注剧名', value: MATERIAL_DRAMA_UNLABELED }] : []),
  ];
}

export function formatDateTime(value: string): string {
  return value.replace('T', ' ').slice(0, 19);
}

export function materialKindLabel(kind: MaterialKind): string {
  if (kind === 'audio') return '音频';
  if (kind === 'video') return '视频';
  return '图片';
}

export { materialCategoryLabel } from './constants';
