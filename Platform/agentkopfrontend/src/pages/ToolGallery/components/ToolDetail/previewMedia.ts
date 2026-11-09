import { isBrowserInaccessibleMediaUrl } from '@/components/MediaPreview/utils';
import type { CapabilityExecution } from '@/types/capabilityRuns';

interface PreviewMediaItem {
  url: string;
  mediaType: 'image' | 'video' | 'audio';
}

function normalizeMediaType(type: string | undefined): PreviewMediaItem['mediaType'] {
  const value = (type ?? '').toLowerCase();
  if (value.includes('video')) return 'video';
  if (value.includes('audio')) return 'audio';
  return 'image';
}

export function collectPreviewMedia(execution: CapabilityExecution): PreviewMediaItem[] {
  const seen = new Set<string>();
  const items: PreviewMediaItem[] = [];

  const add = (url: unknown, type?: string) => {
    if (typeof url !== 'string') return;
    const normalizedUrl = url.trim();
    if (
      !normalizedUrl ||
      seen.has(normalizedUrl) ||
      isBrowserInaccessibleMediaUrl(normalizedUrl)
    ) {
      return;
    }
    seen.add(normalizedUrl);
    items.push({ url: normalizedUrl, mediaType: normalizeMediaType(type) });
  };

  for (const item of execution.mediaArtifacts ?? []) add(item.url, item.type);
  for (const url of execution.imageUrls ?? []) add(url, 'image');
  for (const url of execution.videoUrls ?? []) add(url, 'video');

  const artifacts = execution.output?.artifacts;
  if (Array.isArray(artifacts)) {
    for (const artifact of artifacts) {
      if (!artifact || typeof artifact !== 'object') continue;
      const row = artifact as Record<string, unknown>;
      add(row.uri, typeof row.type === 'string' ? row.type : undefined);
    }
  }

  return items.sort((a, b) => a.url.localeCompare(b.url));
}
