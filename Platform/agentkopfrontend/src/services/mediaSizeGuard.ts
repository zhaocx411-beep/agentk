/**
 * 发给 Agent / 启动 pipeline / 启动工具前的素材体积拦截。
 * 本地上传与素材库勾选最终都走这里，只读 Content-Length / Range，不整文件下载。
 */
import { collectMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import type { MessageAttachment } from '@/pages/AgentPlayground/types';
import { filenameFromMediaUrl, MEDIA_UPLOAD_MAX_BYTES, MEDIA_UPLOAD_MAX_MB } from './media';
import {
  buildMediaDownloadProxyUrl,
  shouldUseMediaDownloadProxy,
} from './mediaDownloadProxy';

const MEDIA_FIELD_KEY =
  /(url|urls|image|images|video|videos|audio|audios|media|file|files|cover|bgm|source_video|product_image|model_image|ref_image)$/i;

const MEDIA_EXT_RE = /\.(mp4|mov|webm|m4v|avi|mkv|mp3|wav|ogg|m4a|aac|flac|png|jpe?g|webp|gif|bmp|pdf|docx?|xlsx?|pptx?|txt|md|csv|rtf)(\?|#|$)/i;

const sizeCache = new Map<string, number | null>();

/** 素材库 extraFields.bytes、本地上传 file.size 等已知体积写入缓存，发送时不再探测 */
export function rememberMediaUrlSize(url: string, bytes?: number): void {
  const trimmed = url.trim();
  if (!trimmed || bytes == null || !Number.isFinite(bytes) || bytes <= 0) return;
  sizeCache.set(trimmed, Math.round(bytes));
}

export interface OutboundMediaRef {
  url: string;
  name?: string;
  size?: number;
}

/** 已有 bytes / file.size 时同步判断，不探测 */
export function isKnownMediaOversize(bytes?: number): boolean {
  return typeof bytes === 'number' && Number.isFinite(bytes) && bytes > MEDIA_UPLOAD_MAX_BYTES;
}

export function mediaOversizeMessage(name?: string): string {
  const label = name?.trim() || '素材';
  return `「${label}」超过 ${MEDIA_UPLOAD_MAX_MB}MB 上限，请压缩或更换素材`;
}

export class MediaTooLargeError extends Error {
  readonly oversized: OutboundMediaRef[];

  constructor(oversized: OutboundMediaRef[]) {
    const names = oversized.map((item) => item.name || filenameFromMediaUrl(item.url) || '素材');
    super(
      names.length === 1
        ? `「${names[0]}」超过 ${MEDIA_UPLOAD_MAX_MB}MB 上限，请压缩或更换素材`
        : `以下素材超过 ${MEDIA_UPLOAD_MAX_MB}MB 上限：${names.join('、')}`,
    );
    this.name = 'MediaTooLargeError';
    this.oversized = oversized;
  }
}

function isHttpUrl(value: string): boolean {
  return /^https?:\/\//i.test(value.trim());
}

function parseByteSize(res: Response): number | undefined {
  const range = res.headers.get('content-range');
  const rangeTotal = range?.match(/\/(\d+)\s*$/)?.[1];
  if (rangeTotal) {
    const parsed = Number(rangeTotal);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  const length = res.headers.get('content-length');
  if (length) {
    const parsed = Number(length);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  return undefined;
}

async function fetchSize(url: string, init?: RequestInit): Promise<number | undefined> {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), 8000);
  try {
    const res = await fetch(url, { ...init, signal: controller.signal });
    const size = parseByteSize(res);
    controller.abort();
    return size;
  } catch {
    controller.abort();
    return undefined;
  } finally {
    window.clearTimeout(timer);
  }
}

async function probeRemoteByteSize(url: string): Promise<number | undefined> {
  const cached = sizeCache.get(url);
  if (cached !== undefined) return cached ?? undefined;

  const directHead = await fetchSize(url, { method: 'HEAD', mode: 'cors', credentials: 'omit' });
  if (directHead != null) {
    sizeCache.set(url, directHead);
    return directHead;
  }

  const directRange = await fetchSize(url, {
    method: 'GET',
    mode: 'cors',
    credentials: 'omit',
    headers: { Range: 'bytes=0-0' },
  });
  if (directRange != null) {
    sizeCache.set(url, directRange);
    return directRange;
  }

  if (shouldUseMediaDownloadProxy(url)) {
    const proxy = buildMediaDownloadProxyUrl(url, 'probe.bin');
    const proxyHead = await fetchSize(proxy, { method: 'HEAD', credentials: 'include' });
    if (proxyHead != null) {
      sizeCache.set(url, proxyHead);
      return proxyHead;
    }
    const proxyRange = await fetchSize(proxy, {
      method: 'GET',
      credentials: 'include',
      headers: { Range: 'bytes=0-0' },
    });
    if (proxyRange != null) {
      sizeCache.set(url, proxyRange);
      return proxyRange;
    }
  }

  sizeCache.set(url, null);
  return undefined;
}

function collectPayloadMediaUrls(value: unknown, keyHint = ''): string[] {
  const urls: string[] = [];
  const walk = (val: unknown, key: string) => {
    if (typeof val === 'string') {
      const trimmed = val.trim();
      if (!isHttpUrl(trimmed)) return;
      if (MEDIA_FIELD_KEY.test(key) || MEDIA_EXT_RE.test(trimmed)) {
        urls.push(trimmed);
      }
      return;
    }
    if (Array.isArray(val)) {
      for (const item of val) walk(item, key);
      return;
    }
    if (val && typeof val === 'object') {
      for (const [nextKey, nextVal] of Object.entries(val)) {
        walk(nextVal, nextKey);
      }
    }
  };
  walk(value, keyHint);
  const collected = collectMediaUrls(value);
  urls.push(...collected.videos, ...collected.audios, ...collected.images);
  return [...new Set(urls)];
}

function labelOf(item: OutboundMediaRef): string {
  return item.name?.trim() || filenameFromMediaUrl(item.url) || '素材';
}

export async function assertOutboundMediaWithinLimit(input: {
  attachments?: Array<Pick<MessageAttachment, 'url' | 'name' | 'size' | 'fileUrl' | 'cdnUrl' | 'blobUrl' | 'sendUrl'>>;
  payload?: unknown;
  extraUrls?: string[];
}): Promise<void> {
  const byUrl = new Map<string, OutboundMediaRef>();

  const push = (url: string, name?: string, size?: number) => {
    const trimmed = url.trim();
    if (!trimmed || !isHttpUrl(trimmed)) return;
    const prev = byUrl.get(trimmed);
    byUrl.set(trimmed, {
      url: trimmed,
      name: name || prev?.name,
      size: typeof size === 'number' && size > 0 ? size : prev?.size,
    });
  };

  for (const item of input.attachments ?? []) {
    push(item.sendUrl || item.url, item.name, item.size);
    if (item.fileUrl) push(item.fileUrl, item.name, item.size);
    if (item.cdnUrl) push(item.cdnUrl, item.name, item.size);
    if (item.blobUrl) push(item.blobUrl, item.name, item.size);
  }
  for (const url of input.extraUrls ?? []) push(url);
  for (const url of collectPayloadMediaUrls(input.payload)) push(url);

  if (byUrl.size === 0) return;

  const oversized: OutboundMediaRef[] = [];
  await Promise.all(
    [...byUrl.values()].map(async (item) => {
      const known = typeof item.size === 'number' && item.size > 0 ? item.size : await probeRemoteByteSize(item.url);
      if (known != null && known > MEDIA_UPLOAD_MAX_BYTES) {
        oversized.push({ ...item, name: labelOf(item), size: known });
      }
    }),
  );

  if (oversized.length > 0) {
    throw new MediaTooLargeError(oversized);
  }
}
