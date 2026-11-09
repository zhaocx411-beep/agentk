import type { MaterialFile } from '@/pages/MaterialLibrary/types';
import { materialCategoryLabel } from '@/pages/MaterialLibrary/constants';
import type { PortfolioRound } from '@/pages/WorksGallery/types';
import { isGenericMediaName } from '@/components/MediaPreview/utils';
import type { MyUploadRecord } from '@/services/media';
import { pickHistoryUploadThumbUrl, pickHistoryUploadUrl } from '@/services/media';
import { rememberMediaUrlSize } from '@/services/mediaSizeGuard';
import type { MediaAsset, MediaKind, MediaSortOrder } from './types';

const KIND_SET = new Set<MediaKind>(['image', 'video', 'audio']);

export function normalizeKinds(accept?: MediaKind | MediaKind[]): MediaKind[] {
  let list: MediaKind[];
  if (accept == null) {
    list = ['image'];
  } else if (Array.isArray(accept)) {
    list = accept;
  } else {
    list = [accept];
  }
  const kinds = list.filter((item) => KIND_SET.has(item));
  return kinds.length > 0 ? kinds : ['image'];
}

export function parseMediaKind(value: unknown): MediaKind | undefined {
  if (typeof value !== 'string' && typeof value !== 'number') return undefined;
  const key = String(value).trim().toLowerCase();
  if (!key) return undefined;
  if (key.startsWith('image') || key === 'img' || key === 'pic' || key === '图片') return 'image';
  if (key.startsWith('audio') || key === 'sound' || key === 'voice' || key === '音频') {
    return 'audio';
  }
  if (key.startsWith('video') || key === 'movie' || key === '视频') return 'video';
  return undefined;
}

export function kindLabel(kind: MediaKind): string {
  if (kind === 'audio') return '音频';
  if (kind === 'video') return '视频';
  return '图片';
}

export function pickerTitle(kinds: MediaKind[]): string {
  if (kinds.length === 1) return `选择${kindLabel(kinds[0])}`;
  return '选择素材';
}

export function acceptAttr(kinds: MediaKind[]): string {
  const parts: string[] = [];
  if (kinds.includes('image'))
    parts.push('image/*', '.png', '.jpg', '.jpeg', '.webp', '.gif', '.bmp');
  if (kinds.includes('video')) parts.push('video/*');
  if (kinds.includes('audio')) parts.push('audio/*');
  return parts.join(',');
}

export function fileMatchesKinds(file: File, kinds: MediaKind[]): boolean {
  const kind = parseMediaKind(file.type) ?? parseMediaKind(file.name.split('.').pop());
  if (kind) return kinds.includes(kind);
  if (kinds.includes('image') && /\.(png|jpe?g|webp|gif|bmp)$/i.test(file.name)) return true;
  if (kinds.includes('video') && /\.(mp4|webm|mov|m4v)$/i.test(file.name)) return true;
  if (kinds.includes('audio') && /\.(mp3|wav|m4a|aac|ogg)$/i.test(file.name)) return true;
  return false;
}

function portfolioAssetLabel(url: string, assetId: string | number): string {
  const fromUrl = url.split('/').pop()?.split('?')[0]?.split('#')[0]?.trim();
  return fromUrl || `作品-${String(assetId)}`;
}

export function sortAssets(items: MediaAsset[], order: MediaSortOrder): MediaAsset[] {
  const copy = [...items];
  copy.sort((a, b) => {
    const left = a.createdAt || '';
    const right = b.createdAt || '';
    return order === 'time-asc' ? left.localeCompare(right) : right.localeCompare(left);
  });
  return copy;
}

export function toUploadAssets(records: MyUploadRecord[], kinds: MediaKind[]): MediaAsset[] {
  return records.flatMap((record) => {
    if (record.status && record.status !== 'success') return [];
    const mediaType = parseMediaKind(record.mediaType);
    if (!mediaType || !kinds.includes(mediaType)) return [];
    const url = pickHistoryUploadUrl(record);
    if (!url) return [];
    const thumbUrl = pickHistoryUploadThumbUrl(record);
    const name = record.blobKey?.split('/').pop();
    return [
      {
        id: `upload-${record.id}`,
        url,
        thumbUrl: thumbUrl !== url ? thumbUrl : undefined,
        name,
        mediaType,
        source: 'history' as const,
        createdAt: record.createdAt,
      },
    ];
  });
}

export function toPortfolioAssets(rounds: PortfolioRound[], kinds: MediaKind[]): MediaAsset[] {
  const items: MediaAsset[] = [];
  const seen = new Set<string>();
  for (const round of rounds) {
    const assets = [...round.resultAssets, round.cover, ...round.intermediateAssets];
    for (const asset of assets) {
      if (!asset?.assetUrl || seen.has(asset.assetUrl)) continue;
      if (!kinds.includes(asset.assetType)) continue;
      seen.add(asset.assetUrl);
      items.push({
        id: `portfolio-${asset.assetId}`,
        url: asset.assetUrl,
        name: portfolioAssetLabel(asset.assetUrl, asset.assetId),
        mediaType: asset.assetType,
        source: 'portfolio',
        createdAt: asset.createdAt || round.createdAt,
      });
    }
  }
  return items;
}

function materialAssetName(file: MaterialFile): string {
  const title = file.title?.trim();
  if (title) return title;
  const fromUrl = file.url.split('/').pop()?.split('?')[0]?.split('#')[0]?.trim();
  if (fromUrl && !isGenericMediaName(fromUrl)) return fromUrl;
  if (file.category) return materialCategoryLabel(file.category);
  return `素材-${file.taskId}`;
}

export function toMaterialAssets(files: MaterialFile[], kinds: MediaKind[]): MediaAsset[] {
  return files
    .filter((file) => kinds.includes(file.kind))
    .map((file) => {
      rememberMediaUrlSize(file.url, file.bytes);
      return {
        id: `material-${file.id}`,
        url: file.url,
        name: materialAssetName(file),
        mediaType: file.kind,
        source: 'material' as const,
        createdAt: file.createdAt,
        size: file.bytes,
      };
    });
}
