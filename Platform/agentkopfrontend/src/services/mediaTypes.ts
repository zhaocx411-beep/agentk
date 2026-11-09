/**
 * 平台 media/upload 允许的素材类型（与后端四类识别对齐）
 * image / video / audio / doc，其它扩展名一律拒绝
 */
export type PlatformMediaType = 'image' | 'video' | 'audio' | 'doc';

export const PLATFORM_MEDIA_EXTS: Record<PlatformMediaType, readonly string[]> = {
  image: ['png', 'jpg', 'jpeg', 'webp', 'gif'],
  video: ['mp4', 'mov', 'webm', 'avi', 'mpeg'],
  audio: ['mp3', 'm4a', 'wav', 'aac', 'ogg', 'flac'],
  doc: ['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'txt', 'md', 'csv', 'rtf'],
};

const MIME_TO_TYPE: Record<string, PlatformMediaType> = {
  'image/png': 'image',
  'image/jpeg': 'image',
  'image/webp': 'image',
  'image/gif': 'image',
  'video/mp4': 'video',
  'video/quicktime': 'video',
  'video/webm': 'video',
  'video/x-msvideo': 'video',
  'video/avi': 'video',
  'video/mpeg': 'video',
  'audio/mpeg': 'audio',
  'audio/mp3': 'audio',
  'audio/mp4': 'audio',
  'audio/x-m4a': 'audio',
  'audio/wav': 'audio',
  'audio/x-wav': 'audio',
  'audio/aac': 'audio',
  'audio/ogg': 'audio',
  'audio/flac': 'audio',
  'audio/x-flac': 'audio',
  'application/pdf': 'doc',
  'application/msword': 'doc',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'doc',
  'application/vnd.ms-excel': 'doc',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'doc',
  'application/vnd.ms-powerpoint': 'doc',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'doc',
  'text/plain': 'doc',
  'text/markdown': 'doc',
  'text/csv': 'doc',
  'application/rtf': 'doc',
  'text/rtf': 'doc',
};

const EXT_TO_TYPE: Record<string, PlatformMediaType> = Object.fromEntries(
  (Object.entries(PLATFORM_MEDIA_EXTS) as Array<[PlatformMediaType, readonly string[]]>).flatMap(
    ([type, exts]) => exts.map((ext) => [ext, type]),
  ),
);

function toAccept(exts: readonly string[]): string {
  return exts.map((ext) => `.${ext}`).join(',');
}

export const PLATFORM_IMAGE_ACCEPT = toAccept(PLATFORM_MEDIA_EXTS.image);
export const PLATFORM_VIDEO_ACCEPT = toAccept(PLATFORM_MEDIA_EXTS.video);
export const PLATFORM_AUDIO_ACCEPT = toAccept(PLATFORM_MEDIA_EXTS.audio);
export const PLATFORM_DOC_ACCEPT = toAccept(PLATFORM_MEDIA_EXTS.doc);
export const PLATFORM_MEDIA_ACCEPT = [
  PLATFORM_IMAGE_ACCEPT,
  PLATFORM_VIDEO_ACCEPT,
  PLATFORM_AUDIO_ACCEPT,
  PLATFORM_DOC_ACCEPT,
].join(',');

export const UNSUPPORTED_MEDIA_MESSAGE =
  '仅支持图片（png/jpg/webp/gif）、视频（mp4/mov/webm/avi/mpeg）、音频（mp3/m4a/wav/aac/ogg/flac）和文档（pdf/doc/xls/ppt/txt/md/csv/rtf）';

export function fileExtension(fileName: string): string {
  const trimmed = fileName.trim();
  const index = trimmed.lastIndexOf('.');
  if (index < 0 || index === trimmed.length - 1) return '';
  return trimmed.slice(index + 1).toLowerCase();
}

/** 按扩展名优先、MIME 兜底，解析平台 mediaType；不支持则返回 null */
export function resolvePlatformMediaType(file: File): PlatformMediaType | null {
  const ext = fileExtension(file.name);
  if (ext) return EXT_TO_TYPE[ext] ?? null;
  const mime = file.type.trim().toLowerCase();
  return MIME_TO_TYPE[mime] ?? null;
}
