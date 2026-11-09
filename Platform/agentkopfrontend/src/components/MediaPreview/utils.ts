/** 预览类型：office 指 Word/Excel/PPT 等需外部打开的文件 */
export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'office' | 'file';

const TEXT_EXT = /\.(txt|md|csv|rtf)$/i;
const PDF_EXT = /\.pdf$/i;
const OFFICE_EXT = /\.(docx?|xlsx?|pptx?|doc|xls|ppt)$/i;
const IMAGE_EXT = /\.(png|jpe?g|webp|gif|bmp|svg|heic|heif)$/i;
const VIDEO_EXT = /\.(mp4|webm|mov|m4v|avi|mkv|mpeg)$/i;
const AUDIO_EXT = /\.(mp3|wav|m4a|aac|ogg|flac)$/i;

/** `.internal` 域名无法从办公网浏览器访问，不应渲染为媒体预览。 */
export function isBrowserInaccessibleMediaUrl(url: string): boolean {
  const trimmed = url.trim();
  if (!trimmed) return false;
  try {
    return new URL(trimmed).hostname.toLowerCase().endsWith('.internal');
  } catch {
    return false;
  }
}

function extFromPath(value: string): string {
  const path = value.split('?')[0]?.split('#')[0]?.toLowerCase() ?? '';
  return path.split('/').pop() ?? path;
}

export function resolvePreviewKind(
  url: string,
  mediaType?: string,
  name?: string,
): PreviewKind {
  const mt = (mediaType || '').trim().toLowerCase();
  if (mt === 'image' || mt.startsWith('image')) return 'image';
  if (mt === 'video' || mt.startsWith('video')) return 'video';
  if (mt === 'audio' || mt.startsWith('audio')) return 'audio';
  if (mt === 'doc') {
    const label = extFromPath(name || url);
    if (PDF_EXT.test(label)) return 'pdf';
    if (TEXT_EXT.test(label)) return 'text';
    if (OFFICE_EXT.test(label)) return 'office';
    return 'file';
  }

  const label = extFromPath(name || url);
  if (IMAGE_EXT.test(label)) return 'image';
  if (VIDEO_EXT.test(label)) return 'video';
  if (AUDIO_EXT.test(label)) return 'audio';
  if (PDF_EXT.test(label)) return 'pdf';
  if (TEXT_EXT.test(label)) return 'text';
  if (OFFICE_EXT.test(label)) return 'office';
  return 'file';
}

export function previewKindLabel(kind: PreviewKind): string {
  if (kind === 'image') return '图片';
  if (kind === 'video') return '视频';
  if (kind === 'audio') return '音频';
  if (kind === 'pdf') return 'PDF';
  if (kind === 'text') return '文本';
  if (kind === 'office') return '文档';
  return '文件';
}

const KIND_FALLBACK_LABELS = new Set([
  '图片',
  '视频',
  '音频',
  'PDF',
  '文本',
  '文档',
  '文件',
  '素材',
  '附件',
]);

const GENERIC_MEDIA_BASENAMES = new Set([
  'preview',
  'asset',
  'file',
  'download',
  'output',
  'result',
  'media',
  'temp',
  'untitled',
]);

/** CDN / 生成链路常见的占位文件名，不应展示给用户 */
export function isGenericMediaName(name: string): boolean {
  const trimmed = name.trim();
  if (!trimmed) return true;
  const base = trimmed.replace(/\.[a-z0-9]{1,8}$/i, '').toLowerCase();
  if (GENERIC_MEDIA_BASENAMES.has(base)) return true;
  // 上传后 CDN 路径常见的 opaque id（如 702ce3690e134a…）
  if (/^[a-f0-9-]{20,}$/i.test(base)) return true;
  if (/^[a-f0-9]{12,}$/i.test(base)) return true;
  return false;
}

export function isDocumentPreviewKind(kind: PreviewKind): boolean {
  return kind === 'office' || kind === 'pdf' || kind === 'text' || kind === 'file';
}

/** 上传区 compact：音频 / 文档用方块 + 外侧标题 */
export function usesCompactFileStack(kind: PreviewKind): boolean {
  return kind === 'audio' || isDocumentPreviewKind(kind);
}

/** 类型兜底文案，不能当作文件名展示 */
export function isKindFallbackLabel(name: string): boolean {
  return KIND_FALLBACK_LABELS.has(name.trim());
}

/** 优先从 name / url 提取可读文件名 */
export function resolveMediaFileLabel(url: string, name?: string, mediaType?: string): string {
  const kind = resolvePreviewKind(url, mediaType, name);
  const trimmed = name?.trim();
  if (trimmed && !isGenericMediaName(trimmed) && !isKindFallbackLabel(trimmed)) {
    return trimmed;
  }
  const fromUrl = extFromPath(url);
  if (fromUrl && !isGenericMediaName(fromUrl) && !isKindFallbackLabel(fromUrl)) {
    return fromUrl;
  }
  if (trimmed && !isGenericMediaName(trimmed)) return trimmed;
  if (fromUrl && !isGenericMediaName(fromUrl)) return fromUrl;
  return kind ? previewKindLabel(kind) : '素材';
}

export function fileDisplayName(url: string, name?: string, kind?: PreviewKind): string {
  const mt =
    kind === 'image'
      ? 'image'
      : kind === 'video'
        ? 'video'
        : kind === 'audio'
          ? 'audio'
          : kind
            ? 'doc'
            : undefined;
  return resolveMediaFileLabel(url, name, mt);
}

/** 缩略图角标：仅在有真实扩展名时返回，如 XLSX；不用「文档」等类型名占位 */
export function fileExtensionLabel(
  url: string,
  name?: string,
  _kind?: PreviewKind,
): string | undefined {
  for (const source of [name, url]) {
    const label = extFromPath(source || '');
    const match = label.match(/\.([a-z0-9]{1,8})$/i);
    if (match?.[1]) return match[1].toUpperCase();
  }
  return undefined;
}

/** compact 外侧标题：有真实文件名才展示 */
export function shouldShowCompactCaption(displayName: string): boolean {
  const trimmed = displayName.trim();
  if (!trimmed) return false;
  if (isGenericMediaName(trimmed)) return false;
  if (isKindFallbackLabel(trimmed)) return false;
  return true;
}

export function truncateFileName(name: string, maxLen = 14): string {
  const trimmed = name.trim();
  if (trimmed.length <= maxLen) return trimmed;
  const extMatch = trimmed.match(/(\.[a-z0-9]{1,8})$/i);
  const ext = extMatch?.[1] ?? '';
  const base = ext ? trimmed.slice(0, -ext.length) : trimmed;
  const keep = Math.max(maxLen - ext.length - 1, 4);
  return `${base.slice(0, keep)}…${ext}`;
}
