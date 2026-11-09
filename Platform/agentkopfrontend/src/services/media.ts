/**
 * 平台多媒体上传
 * POST /agent/operation/backend/platform/media/upload
 *
 * 上传后优先选用 blobUrl，
 * 再转换为 messages V3 的 attachments 结构发送。
 */
import type {
  MessageAttachment,
  SendMessageAttachment,
  SendMessageAttachmentType,
} from '@/pages/AgentPlayground/types';
import { isGenericMediaName } from '@/components/MediaPreview/utils';
import { buildMediaDownloadProxyUrl, shouldUseMediaDownloadProxy } from './mediaDownloadProxy';
import { post } from '@/utils/request';
import {
  resolvePlatformMediaType,
  UNSUPPORTED_MEDIA_MESSAGE,
  type PlatformMediaType as AllowedPlatformMediaType,
} from './mediaTypes';

export {
  PLATFORM_AUDIO_ACCEPT,
  PLATFORM_DOC_ACCEPT,
  PLATFORM_IMAGE_ACCEPT,
  PLATFORM_MEDIA_ACCEPT,
  PLATFORM_MEDIA_EXTS,
  PLATFORM_VIDEO_ACCEPT,
  UNSUPPORTED_MEDIA_MESSAGE,
  resolvePlatformMediaType,
} from './mediaTypes';

const DOCUMENT_FILE_PATTERN = /\.(pdf|docx?|xlsx?|pptx?|txt|md|csv|rtf)$/i;
const IMAGE_URL_MEDIA_PATTERN = /\.(png|jpe?g|webp|gif|bmp|svg|heic|heif)(?:[?#&]|$)/i;
const VIDEO_URL_MEDIA_PATTERN = /\.(mp4|webm|mov|m4v|avi|mkv)(?:[?#&]|$)/i;
const AUDIO_URL_MEDIA_PATTERN = /\.(mp3|wav|aac|m4a|ogg|flac)(?:[?#&]|$)/i;
const DOCUMENT_URL_MEDIA_PATTERN = /\.(pdf|docx?|xlsx?|pptx?|txt|md|csv|rtf)(?:[?#&]|$)/i;
export const DOCUMENT_UPLOAD_ACCEPT = '.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt,.md,.csv,.rtf';
export const PLATFORM_MEDIA_UPLOAD_ACCEPT = `image/*,video/*,audio/*,${DOCUMENT_UPLOAD_ACCEPT}`;

/** 平台本地上传单文件上限（剪辑链路约 512MB，前端先拦 500MB） */
export const MEDIA_UPLOAD_MAX_MB = 500;
export const MEDIA_UPLOAD_MAX_BYTES = MEDIA_UPLOAD_MAX_MB * 1024 * 1024;
export const SEND_MESSAGE_MAX_ATTACHMENTS = 10;
export const SEND_MESSAGE_MAX_VIDEOS = 1;

function mediaUploadOversizeMessage(fileName: string): string {
  return `${fileName} 超过 ${MEDIA_UPLOAD_MAX_MB}MB 上限，请压缩后再上传`;
}

/** 平台允许类型 + 兜底 file（校验时用于拒绝未知格式） */
export type PlatformMediaType = AllowedPlatformMediaType | 'file';

export interface MediaUploadResult {
  /** 优先选用 blobUrl，其余 URL 仅用于兼容兜底 */
  url: string;
  fileId?: string;
  fileUrl?: string;
  cdnUrl?: string;
  /** 内网 blob 地址（本地 / staging 供 Agent 读取） */
  blobUrl?: string;
  mediaType?: string;
  contentType?: string;
  size?: number;
  name?: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function asOptionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined;
}

/** 从扩展名 / MIME 推断 mediaType；白名单外兜底为 file */
export function inferMediaType(file: File): PlatformMediaType {
  const allowed = resolvePlatformMediaType(file);
  if (allowed) return allowed;
  if (file.type.startsWith('image/')) return 'image';
  if (file.type.startsWith('video/')) return 'video';
  if (file.type.startsWith('audio/')) return 'audio';
  if (DOCUMENT_FILE_PATTERN.test(file.name)) return 'doc';
  return 'file';
}

/** 从远程 URL 推断 mediaType（用于 TEMPLATE referImages 等） */
export function inferMediaTypeFromUrl(url: string): PlatformMediaType {
  if (IMAGE_URL_MEDIA_PATTERN.test(url)) return 'image';
  if (VIDEO_URL_MEDIA_PATTERN.test(url)) return 'video';
  if (AUDIO_URL_MEDIA_PATTERN.test(url)) return 'audio';
  if (DOCUMENT_URL_MEDIA_PATTERN.test(url)) return 'doc';
  return 'file';
}

const MIME_TYPE_BY_EXTENSION: Record<string, string> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  webp: 'image/webp',
  gif: 'image/gif',
  bmp: 'image/bmp',
  svg: 'image/svg+xml',
  heic: 'image/heic',
  heif: 'image/heif',
  mp4: 'video/mp4',
  m4v: 'video/mp4',
  webm: 'video/webm',
  mov: 'video/quicktime',
  avi: 'video/x-msvideo',
  mkv: 'video/x-matroska',
  mp3: 'audio/mpeg',
  wav: 'audio/wav',
  aac: 'audio/aac',
  m4a: 'audio/mp4',
  ogg: 'audio/ogg',
  flac: 'audio/flac',
};

function resolveSendMessageAttachmentType(
  item: MessageAttachment,
): SendMessageAttachmentType | undefined {
  const declared = item.mediaType?.trim().toLowerCase();
  if (declared === 'image' || declared === 'video' || declared === 'audio') return declared;
  const mimePrefix = item.contentType?.trim().toLowerCase().split('/')[0];
  if (mimePrefix === 'image' || mimePrefix === 'video' || mimePrefix === 'audio') {
    return mimePrefix;
  }
  const inferred = inferMediaTypeFromUrl(item.sendUrl?.trim() || item.url);
  return inferred === 'image' || inferred === 'video' || inferred === 'audio'
    ? inferred
    : undefined;
}

function resolveAttachmentMimeType(
  item: MessageAttachment,
  type: SendMessageAttachmentType,
  uri: string,
): string | undefined {
  const declared = item.contentType?.split(';')[0]?.trim().toLowerCase();
  if (declared?.startsWith(`${type}/`)) return declared;
  const pathname = uri.split('?')[0]?.split('#')[0] ?? '';
  const extension = pathname.match(/\.([a-z0-9]+)$/i)?.[1]?.toLowerCase();
  const inferred = extension ? MIME_TYPE_BY_EXTENSION[extension] : undefined;
  if (inferred?.startsWith(`${type}/`)) return inferred;
  // V3 要求视频、音频必须携带 mimeType；上传接口缺失时使用通用默认值兜底。
  if (type === 'video') return 'video/mp4';
  if (type === 'audio') return 'audio/mpeg';
  return undefined;
}

/** UI 附件 → POST /messages V3 attachments。文档等不支持类型留给兼容链路处理。 */
export function toSendMessageAttachment(
  item: MessageAttachment,
): SendMessageAttachment | undefined {
  const uri = item.sendUrl?.trim() || resolveMessageAttachmentUrl(item);
  if (!uri) return undefined;
  const type = resolveSendMessageAttachmentType(item);
  if (!type) return undefined;
  const mimeType = resolveAttachmentMimeType(item, type, uri);
  return {
    type,
    uri,
    ...(mimeType ? { mimeType } : {}),
  };
}

export function partitionSendMessageAttachments(items: MessageAttachment[]): {
  attachments: SendMessageAttachment[];
  unsupported: MessageAttachment[];
} {
  const attachments: SendMessageAttachment[] = [];
  const unsupported: MessageAttachment[] = [];
  for (const item of items) {
    const attachment = toSendMessageAttachment(item);
    if (attachment) attachments.push(attachment);
    else unsupported.push(item);
  }
  return { attachments, unsupported };
}

export function validateSendMessageAttachments(
  attachments: SendMessageAttachment[],
): string | undefined {
  if (attachments.length > SEND_MESSAGE_MAX_ATTACHMENTS) {
    return `每次最多发送 ${SEND_MESSAGE_MAX_ATTACHMENTS} 个附件`;
  }
  const videoCount = attachments.filter((item) => item.type === 'video').length;
  if (videoCount > SEND_MESSAGE_MAX_VIDEOS) {
    return `每轮最多发送 ${SEND_MESSAGE_MAX_VIDEOS} 个视频`;
  }
  return undefined;
}

/** 平台受管对象存储：hostname 以 bs 开头（浏览器侧常可访问，Agent 工具不可） */
export function isInternalMediaUrl(url: string): boolean {
  const trimmed = url.trim();
  if (!trimmed) return false;
  try {
    return new URL(trimmed).hostname.toLowerCase().startsWith('bs');
  } catch {
    return false;
  }
}

/**
 * Agent 工具（read_file 等）无法拉取的 URL。
 * staging CDN（如 kcdn.staging.kuaishou.com）在浏览器可访问，但 worker 解析到 10.x 内网。
 */
export function isAgentInaccessibleMediaUrl(url: string): boolean {
  const trimmed = url.trim();
  if (!trimmed) return true;
  try {
    const host = new URL(trimmed).hostname.toLowerCase();
    if (isInternalMediaUrl(trimmed)) return true;
    if (host.includes('.staging.') || host.endsWith('.staging.kuaishou.com')) return true;
    if (
      host === 'localhost' ||
      /^127\./.test(host) ||
      /^10\./.test(host) ||
      /^192\.168\./.test(host) ||
      /^172\.(1[6-9]|2\d|3[01])\./.test(host)
    ) {
      return true;
    }
    return false;
  } catch {
    return true;
  }
}

export interface UploadUrlFields {
  blobUrl?: string;
  cdnUrl?: string;
  fileUrl?: string;
  url?: string;
}

/**
 * 上传结果统一优先选用 blobUrl，其他地址仅作缺字段时的兜底。
 */
export function pickUploadMediaUrl(fields: UploadUrlFields): string {
  const blob = fields.blobUrl?.trim();
  const cdn = fields.cdnUrl?.trim();
  const file = fields.fileUrl?.trim();
  const url = fields.url?.trim();
  return blob || file || url || cdn || '';
}

/** 上传结果写入附件 / 工具入参时的地址，规则同 pickUploadMediaUrl */
export function resolveAgentToolMediaUrl(result: MediaUploadResult): string {
  return pickUploadMediaUrl(result);
}

/** 消息发送附件 URL，规则同 pickUploadMediaUrl */
export function resolveMessageAttachmentUrl(item: MessageAttachment): string {
  return pickUploadMediaUrl(item);
}

function isDocMediaType(mediaType?: string): boolean {
  return (mediaType || '').trim().toLowerCase() === 'doc';
}

/** 与 pickUploadMediaUrl 相同，保留给文档解析发送路径 */
export function pickDocParseLoopMediaUrl(fields: UploadUrlFields): string {
  return pickUploadMediaUrl(fields);
}

/** 仅改发送用 sendUrl，气泡预览仍走 url / cdnUrl */
export function withDocParseLoopSendUrl(item: MessageAttachment): MessageAttachment {
  const sendUrl = pickUploadMediaUrl(item);
  return sendUrl ? { ...item, sendUrl } : item;
}

/** 远程 URL → 对话附件（模版 / 素材库 / 历史创作：不走上传接口） */
export function messageAttachmentFromUrl(
  url: string,
  name?: string,
  mediaType?: PlatformMediaType,
): MessageAttachment {
  const trimmed = url.trim();
  const resolvedMediaType = mediaType ?? inferMediaTypeFromUrl(trimmed);
  const fallbackName = trimmed.split('/').pop()?.split('?')[0]?.split('#')[0] || '素材';
  const displayName = name?.trim() || fallbackName;
  if (isDocMediaType(resolvedMediaType)) {
    return {
      url: trimmed,
      mediaType: resolvedMediaType,
      name: displayName,
      cdnUrl: trimmed,
      fileUrl: trimmed,
    };
  }
  return {
    url: trimmed,
    mediaType: resolvedMediaType,
    name: displayName,
    fileUrl: trimmed,
  };
}

/** 平台媒体上传的统一前端校验；具体场景可额外限制 allowedTypes */
export function validateMediaUploadFile(
  file: File,
  allowedTypes: readonly PlatformMediaType[] = ['image', 'video', 'audio', 'doc'],
): string | undefined {
  const mediaType = inferMediaType(file);
  if (mediaType === 'file' || !allowedTypes.includes(mediaType)) {
    return `${file.name} 的文件格式不受支持`;
  }
  if (file.size > MEDIA_UPLOAD_MAX_BYTES) {
    return mediaUploadOversizeMessage(file.name);
  }
  return undefined;
}

/**
 * 解析上传响应。
 * 实际字段：fileId / mediaType / blobUrl / fileUrl / cdnUrl / contentType / size / originalFilename
 * url 统一优先选用 blobUrl。
 */
export function normalizeMediaUploadResult(
  data: unknown,
  fallbackName?: string,
): MediaUploadResult {
  if (typeof data === 'string' && data.trim()) {
    return { url: data.trim(), name: fallbackName };
  }
  if (!isRecord(data)) {
    throw new Error('上传响应无效');
  }

  const nested = isRecord(data.data) ? data.data : null;
  const source = nested ?? data;

  const cdnUrl = asOptionalString(source.cdnUrl);
  const fileUrl = asOptionalString(source.fileUrl);
  const blobUrl = asOptionalString(source.blobUrl);
  const legacyUrl =
    asOptionalString(source.url) ||
    asOptionalString(source.mediaUrl) ||
    asOptionalString(source.path) ||
    asOptionalString(source.ossUrl);
  const url = pickUploadMediaUrl({ blobUrl, cdnUrl, fileUrl, url: legacyUrl });
  if (!url) {
    throw new Error('上传成功但未返回文件地址');
  }

  const fromApi =
    asOptionalString(source.originalFilename) ||
    asOptionalString(source.name) ||
    asOptionalString(source.fileName);
  const fallback = fallbackName?.trim();
  const name =
    (fromApi && !isGenericMediaName(fromApi) ? fromApi : undefined) ||
    (fallback && !isGenericMediaName(fallback) ? fallback : undefined) ||
    fromApi ||
    fallback;

  return {
    url,
    fileId: asOptionalString(source.fileId),
    fileUrl,
    cdnUrl,
    blobUrl,
    mediaType: asOptionalString(source.mediaType) || asOptionalString(source.type) || undefined,
    contentType: asOptionalString(source.contentType),
    size: typeof source.size === 'number' ? source.size : undefined,
    name,
  };
}

/** 上传单个多媒体文件（仅 image / video / audio / doc） */
export async function uploadMedia(file: File): Promise<MediaUploadResult> {
  const actualType = resolvePlatformMediaType(file);
  if (!actualType) {
    throw new Error(UNSUPPORTED_MEDIA_MESSAGE);
  }
  if (file.size > MEDIA_UPLOAD_MAX_BYTES) {
    throw new Error(mediaUploadOversizeMessage(file.name));
  }
  const formData = new FormData();
  formData.append('file', file);
  formData.append('mediaType', actualType);

  const { default: request } = await import('@/utils/request');
  const res = await request({
    method: 'POST',
    url: '/agent/operation/backend/platform/media/upload',
    data: formData,
    timeout: 120000,
    headers: {
      'Content-Type': 'multipart/form-data',
      'X-No-Loading': '1',
    },
  });
  const body = res.data as { code?: number; message?: string; data?: unknown };
  if (body.code !== undefined && body.code !== 200 && body.code !== 0) {
    throw new Error(body.message || '上传失败');
  }
  return normalizeMediaUploadResult(body.data ?? body, file.name);
}

function triggerBlobDownload(blob: Blob, filename: string): void {
  const objectUrl = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = objectUrl;
  link.download = filename;
  link.rel = 'noopener';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000);
}

async function fetchAsDownloadBlob(url: string, init?: RequestInit): Promise<Blob | null> {
  try {
    const res = await fetch(url, init);
    if (!res.ok) return null;
    const contentType = res.headers.get('content-type') || '';
    if (contentType.includes('text/html')) return null;
    const blob = await res.blob();
    if (blob.size === 0) return null;
    if (blob.type.includes('application/json')) return null;
    if (blob.type.includes('text/html')) return null;
    return blob;
  } catch {
    return null;
  }
}

function filenameFromRemoteUrl(url: string, fallback = '素材'): string {
  const path = url.split('?')[0]?.split('#')[0] ?? '';
  const name = path.split('/').pop()?.trim();
  if (name && /\./.test(name)) return name;
  return fallback;
}

/** 代理下载用安全文件名：优先 URL 路径，避免把整段中文 prompt 塞进 query */
function filenameForRemoteFetch(sourceUrl: string, displayName?: string): string {
  const fromUrl = filenameFromRemoteUrl(sourceUrl, '');
  if (fromUrl && fromUrl !== '素材') return fromUrl;
  const cleaned = (displayName || '').replace(/[\r\n"<>|/\\:*?]/g, '_').trim();
  if (cleaned && /^[\w.-]+$/.test(cleaned)) return cleaned.slice(0, 120);
  return 'remote-asset.bin';
}

async function fetchRemoteMediaBlob(url: string, displayName?: string): Promise<Blob> {
  const trimmed = url.trim();
  if (!trimmed) throw new Error('文件地址无效');

  const filename = filenameForRemoteFetch(trimmed, displayName);
  const fetchDirect = () => fetchAsDownloadBlob(trimmed, { mode: 'cors', credentials: 'omit' });

  let blob: Blob | null = null;
  if (shouldUseMediaDownloadProxy(trimmed)) {
    const proxy = buildMediaDownloadProxyUrl(trimmed, filename);
    blob = (await fetchAsDownloadBlob(proxy, { credentials: 'include' })) || (await fetchDirect());
  } else {
    blob = await fetchDirect();
  }
  if (!blob) {
    const proxy = buildMediaDownloadProxyUrl(trimmed, filename);
    blob = await fetchAsDownloadBlob(proxy, { credentials: 'include' });
  }
  if (!blob) throw new Error('无法读取远程文件，请稍后重试');
  return blob;
}

export interface UploadMediaFromUrlResult {
  result: MediaUploadResult;
  /** 图片本地 blob 预览（调用方需在移除时 revoke） */
  previewUrl?: string;
}

export interface EnsurePlatformMediaUrlResult {
  attachment: MessageAttachment;
  /** 外部 URL 转存时图片预览 blob（调用方需在移除时 revoke） */
  previewUrl?: string;
}

/** 平台受管存储（bs 内网等）：已是 upload 产物，无需再 download → upload */
export function isPlatformManagedMediaUrl(url: string): boolean {
  return isInternalMediaUrl(url);
}

/**
 * 远程 URL 是否可直接挂附件，跳过浏览器 download → upload。
 * - bs 内网：已是平台存储（如 doc_02 Word）
 * - 公网 Agent 可读（arxiv / github raw 等）：Agent 可 read_file，浏览器 CORS 转存常失败
 * 仍需转存：staging CDN 等 Agent 不可达、需代理拉取后再 upload 的地址
 */
export function canUseRemoteMediaUrlDirectly(url: string): boolean {
  const trimmed = url.trim();
  if (!trimmed) return false;
  if (isInternalMediaUrl(trimmed)) return true;
  return !isAgentInaccessibleMediaUrl(trimmed);
}

/** 历史上传记录选用 URL，规则同 pickUploadMediaUrl */
export function pickHistoryUploadUrl(record: Pick<MyUploadRecord, 'cdnUrl' | 'blobUrl'>): string {
  return pickUploadMediaUrl({ blobUrl: record.blobUrl, cdnUrl: record.cdnUrl });
}

/** 历史上传缩略图与业务地址保持一致，统一优先 blobUrl */
export function pickHistoryUploadThumbUrl(
  record: Pick<MyUploadRecord, 'cdnUrl' | 'blobUrl'>,
): string {
  return pickHistoryUploadUrl(record);
}

export interface PickedMediaSource {
  url: string;
  name?: string;
  mediaType?: PlatformMediaType;
  source?: 'local' | 'history' | 'material' | 'portfolio';
}

/**
 * 素材弹层选中 → 会话附件：仅复用已有 URL，不转存上传。
 * 本地上传在 ChatAttachmentBar / MediaUpload 中单独调用 uploadMedia。
 */
export async function resolvePickedMediaAttachment(
  asset: PickedMediaSource,
): Promise<EnsurePlatformMediaUrlResult> {
  const trimmed = asset.url.trim();
  if (!trimmed) throw new Error('文件地址无效');
  return { attachment: messageAttachmentFromUrl(trimmed, asset.name, asset.mediaType) };
}

/**
 * 远程 URL 转存上传（下载 → platform upload）。
 * 对话附件流程不再调用；保留供其他需要转存的场景。
 */
export async function ensurePlatformMediaUrl(
  fileUrl: string,
  options?: { name?: string; mediaType?: PlatformMediaType },
): Promise<EnsurePlatformMediaUrlResult> {
  const trimmed = fileUrl.trim();
  if (!trimmed) throw new Error('文件地址无效');
  if (canUseRemoteMediaUrlDirectly(trimmed)) {
    return { attachment: messageAttachmentFromUrl(trimmed, options?.name) };
  }
  const { result, previewUrl } = await uploadMediaFromUrl(trimmed, options);
  return { attachment: toMessageAttachment(result), previewUrl };
}

/**
 * 远程 URL → 下载 blob → 平台 upload → 受管 fileUrl。
 */
export async function uploadMediaFromUrl(
  fileUrl: string,
  options?: { name?: string; mediaType?: PlatformMediaType },
): Promise<UploadMediaFromUrlResult> {
  const filename = filenameForRemoteFetch(fileUrl, options?.name);
  const blob = await fetchRemoteMediaBlob(fileUrl, options?.name);
  const file = new File([blob], filename, {
    type: blob.type || 'application/octet-stream',
  });
  const validationError = validateMediaUploadFile(file);
  if (validationError) throw new Error(validationError);

  const actualType = inferMediaType(file);
  const previewUrl = actualType === 'image' ? URL.createObjectURL(blob) : undefined;
  const result = await uploadMedia(file);
  return { result, previewUrl };
}

/**
 * 下载远程媒体。CDN 直链跨域时 <a download> 会变成跳转，
 * 所以先走同源代理拿到 blob，再触发保存（对齐数据集 export 的下载体验）。
 */
export async function downloadMediaByUrl(fileUrl: string, filename: string): Promise<void> {
  const blob = await fetchRemoteMediaBlob(fileUrl, filename);
  triggerBlobDownload(blob, filenameForRemoteFetch(fileUrl, filename));
}

/** 转为会话侧附件结构（url 优先 Agent 可读地址） */
export function toMessageAttachment(result: MediaUploadResult, file?: File): MessageAttachment {
  const apiName = result.name?.trim();
  const localName = file?.name?.trim();
  const name =
    (localName && !isGenericMediaName(localName) ? localName : undefined) ||
    (apiName && !isGenericMediaName(apiName) ? apiName : undefined) ||
    localName ||
    apiName;
  return {
    url: resolveAgentToolMediaUrl(result),
    mediaType: result.mediaType ?? (file ? inferMediaType(file) : undefined),
    name,
    fileId: result.fileId,
    fileUrl: result.fileUrl,
    cdnUrl: result.cdnUrl,
    blobUrl: result.blobUrl,
    contentType: result.contentType,
    size: typeof result.size === 'number' ? result.size : file?.size,
  };
}

/** @deprecated 旧版拼装标记；新消息不再写入，仅解析历史 */
export const USER_UPLOAD_MARKER = '附件：';

/** 历史会话里旧版引导文案 / 标记，解析时剥离，不展示 */
const LEGACY_USER_UPLOAD_MARKERS = [
  USER_UPLOAD_MARKER,
  '用户上传的本地素材（均为受管 artifact URL）：',
  '用户上传的本地素材：',
] as const;

const LEGACY_ATTACHMENT_PLACEHOLDERS = new Set(['[附件]', '请处理以下本地素材。']);

/** `- name（type）：url` 或 `- name (type): url` */
const ATTACHMENT_LINE_RE = /^\s*[-*]\s*(.+?)\s*[（(]([^)）]+)[)）]\s*[：:]\s*(\S+)\s*$/;

const MAX_ATTACHMENT_COMPOSE_NAME_LEN = 120;

/** 从 URL 提取文件名，供附件行展示名 */
export function filenameFromMediaUrl(url: string): string | undefined {
  const trimmed = url.trim();
  if (!trimmed) return undefined;
  return trimmed.split('/').pop()?.split('?')[0]?.split('#')[0]?.trim() || undefined;
}

/**
 * 写入 POST text 的附件名：单行、有长度上限。
 * 历史创作曾把整段 promptText 塞进 name，会在输入框不可见的情况下拼进请求体。
 */
export function attachmentComposeName(item: MessageAttachment): string {
  const fromUrl = filenameFromMediaUrl(resolveMessageAttachmentUrl(item));
  const raw = item.name?.trim();
  if (!raw) return fromUrl || '附件';
  const singleLine = raw.replace(/\s+/g, ' ').trim();
  if (!singleLine) return fromUrl || '附件';
  if (singleLine.length > MAX_ATTACHMENT_COMPOSE_NAME_LEN) {
    return fromUrl || `${singleLine.slice(0, MAX_ATTACHMENT_COMPOSE_NAME_LEN)}…`;
  }
  if (singleLine.includes('。') && singleLine.length > 48) {
    return fromUrl || `${singleLine.slice(0, 48)}…`;
  }
  return singleLine;
}

function formatAttachmentLine(item: MessageAttachment): string {
  const name = attachmentComposeName(item);
  const type = item.mediaType || 'file';
  const url = item.sendUrl?.trim() || resolveMessageAttachmentUrl(item);
  return `- ${name}（${type}）：${url}`;
}

function parseAttachmentLine(line: string): MessageAttachment | null {
  const match = ATTACHMENT_LINE_RE.exec(line);
  if (!match) return null;
  const url = match[3].trim();
  if (!url) return null;
  return { url, name: match[1].trim(), mediaType: match[2].trim() };
}

/**
 * 在 V3 attachments 之外保留给旧 Agent 的兼容文本清单，历史消息解析也复用该格式。
 * 有用户原文：`原文\n\n- name（type）：url`
 * 仅附件：只发附件行，由用户自己在对话里说明意图。
 */
export function composeMessageWithAttachments(
  text: string,
  attachments: MessageAttachment[],
): string {
  if (attachments.length === 0) return text.trim();
  const lines = attachments.map(formatAttachmentLine).join('\n');
  const prompt = text.trim();
  return prompt ? `${prompt}\n\n${lines}` : lines;
}

function findLegacyUploadMarker(raw: string): { index: number; marker: string } | null {
  let best: { index: number; marker: string } | null = null;
  for (const marker of LEGACY_USER_UPLOAD_MARKERS) {
    const index = raw.indexOf(marker);
    if (index < 0) continue;
    if (!best || index < best.index) {
      best = { index, marker };
    }
  }
  return best;
}

function stripLegacyPlaceholder(displayText: string): string {
  const trimmed = displayText.trim();
  return LEGACY_ATTACHMENT_PLACEHOLDERS.has(trimmed) ? '' : trimmed;
}

const IMAGE_URL_EXT_RE = /\.(png|jpe?g|webp|gif|bmp|svg)(\?|#|$)/i;

function isLikelyImageUrl(url: string): boolean {
  return IMAGE_URL_EXT_RE.test(url);
}

/** 从单行文本里识别独立图片 URL（含 `1. https://...` 列表项） */
function parseLineImageUrl(line: string): string | null {
  const trimmed = line.trim();
  if (!trimmed) return null;
  const numbered = /^\d+\.\s+(https?:\/\/\S+)/i.exec(trimmed);
  if (numbered?.[1] && isLikelyImageUrl(numbered[1])) return numbered[1];
  if (/^https?:\/\/\S+$/i.test(trimmed) && isLikelyImageUrl(trimmed)) return trimmed;
  return null;
}

/** 虚拟试穿等结构化 prompt 里的「用户补充要求」段落 */
function extractUserSupplementaryText(raw: string): string {
  const match = /用户补充要求:\s*\n([\s\S]*?)(?:\n【|$)/.exec(raw);
  return match?.[1]?.trim() ?? '';
}

/** 去掉结构化输入/执行要求等框架行，保留可读正文 */
function stripStructuredInputBoilerplate(text: string): string {
  const dropLine = (line: string): boolean => {
    const trimmed = line.trim();
    if (!trimmed) return false;
    if (/^【.+】$/.test(trimmed)) return true;
    if (/^(商品图|模特图)\s+\w+:/.test(trimmed)) return true;
    if (/^用户补充要求:/.test(trimmed)) return true;
    if (/^请执行/.test(trimmed) && trimmed.endsWith('。')) return true;
    return false;
  };

  return text
    .split('\n')
    .filter((line) => !dropLine(line))
    .join('\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

/** 从正文中扫描裸图片 URL，供虚拟试穿等结构化 user.input 回放 */
function extractStructuredImageAttachments(raw: string): {
  displayText: string;
  attachments: MessageAttachment[];
} {
  const attachments: MessageAttachment[] = [];
  const seen = new Set<string>();
  const keptLines: string[] = [];

  for (const line of raw.split('\n')) {
    const url = parseLineImageUrl(line);
    if (url && !seen.has(url)) {
      seen.add(url);
      attachments.push({
        url,
        name: filenameFromMediaUrl(url),
        mediaType: 'image',
      });
      continue;
    }
    keptLines.push(line);
  }

  if (attachments.length === 0) {
    return { displayText: raw, attachments: [] };
  }

  const supplementary = extractUserSupplementaryText(raw);
  const beforeStructured = raw.split('【结构化输入】')[0]?.trim() ?? '';
  const cleaned = stripStructuredInputBoilerplate(keptLines.join('\n'));
  const displayText =
    supplementary ||
    (beforeStructured && !beforeStructured.includes('http') ? beforeStructured : '') ||
    cleaned;

  return {
    displayText: stripLegacyPlaceholder(displayText),
    attachments,
  };
}

/** 虚拟试穿 userInput → 消息附件（乐观气泡与 payload 解析复用） */
export function attachmentsFromUserInput(userInput?: {
  productImageUrls?: string[];
  modelImageUrl?: string;
}): MessageAttachment[] {
  if (!userInput) return [];
  const out: MessageAttachment[] = [];
  const seen = new Set<string>();
  const push = (url: string, fallbackName: string) => {
    const trimmed = url.trim();
    if (!trimmed || seen.has(trimmed)) return;
    seen.add(trimmed);
    out.push({
      url: trimmed,
      name: filenameFromMediaUrl(trimmed) || fallbackName,
      mediaType: 'image',
    });
  };

  for (const [index, url] of (userInput.productImageUrls ?? []).entries()) {
    push(url, `商品图 ${index + 1}`);
  }
  if (userInput.modelImageUrl) {
    push(userInput.modelImageUrl, '模特图');
  }
  return out;
}

/**
 * 把「原文 + 附件清单」拼装文本拆回气泡展示用字段。
 * 气泡：上方缩略图 / 文件卡，下方只显示用户原文（不含 URL 清单）。
 */
export function splitComposedUserMessage(text: string): {
  displayText: string;
  attachments: MessageAttachment[];
} {
  const raw = text.trim();
  if (!raw) return { displayText: '', attachments: [] };

  // 旧格式：引导标记 + 清单
  const legacy = findLegacyUploadMarker(raw);
  if (legacy) {
    const displayText = stripLegacyPlaceholder(raw.slice(0, legacy.index));
    const rest = raw.slice(legacy.index + legacy.marker.length);
    const attachments: MessageAttachment[] = [];
    for (const line of rest.split('\n')) {
      const item = parseAttachmentLine(line);
      if (item) attachments.push(item);
    }
    return { displayText, attachments };
  }

  // 新格式：文末连续附件行，前面是用户原文（可为空）
  const lines = raw.split('\n');
  const attachments: MessageAttachment[] = [];
  let cut = lines.length;
  while (cut > 0) {
    const line = lines[cut - 1];
    if (!line.trim()) {
      cut -= 1;
      continue;
    }
    const item = parseAttachmentLine(line);
    if (!item) break;
    attachments.unshift(item);
    cut -= 1;
  }

  if (attachments.length > 0) {
    return {
      displayText: stripLegacyPlaceholder(lines.slice(0, cut).join('\n')),
      attachments,
    };
  }

  // 结构化输入（如虚拟试穿）：正文内嵌裸图片 URL
  return extractStructuredImageAttachments(raw);
}

export interface MyUploadRecord {
  id: number;
  userId: string;
  mediaType: string;
  status: string;
  errorMessage?: string;
  blobKey?: string;
  blobUrl?: string;
  cdnUrl?: string;
  createdAt: string;
}

export interface MyUploadsQueryParams {
  mediaType?: string;
  status?: string;
  page: number;
  pageSize: number;
}

export interface MyUploadsPage {
  page: number;
  pageSize: number;
  total: number;
  records: MyUploadRecord[];
}

export const myUploadsQueryKey = (params: MyUploadsQueryParams) =>
  ['media', 'my-uploads', params] as const;

/** 当前用户历史上传（仅成功记录用于回填） */
export function queryMyUploads(params: MyUploadsQueryParams) {
  return post<MyUploadsPage>('/agent/operation/backend/platform/media/my-uploads/query', params, {
    headers: { 'X-No-Loading': '1' },
  });
}
