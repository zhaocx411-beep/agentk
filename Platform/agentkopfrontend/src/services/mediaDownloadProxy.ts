/**
 * 媒体下载代理域名白名单（开发 Vite 中间件 + 前端是否走代理 共用）
 *
 * 用途：快手系 CDN 在浏览器里常无 CORS，需 dev 代理 / 线上 local-media-download 同源拉取。
 * 外链（图虫、unsplash 等）不在此名单 → 前端直连；若站点不允许 CORS 则转存会失败。
 *
 * 扩展白名单：在下面正则里追加域名后缀，改完后需重启 `pnpm dev`。
 */
const MEDIA_PROXY_HOST_PATTERN =
  /(^|\.)(kuaishou\.com|kwai\.com|yximgs\.com|ksapisrv\.com|gifshow\.com)$/i;

export const MEDIA_DOWNLOAD_DEV_PATH = '/__media-proxy__/download';

export function isMediaDownloadProxyHost(hostname: string): boolean {
  return MEDIA_PROXY_HOST_PATTERN.test(hostname.trim());
}

/** 该 URL 是否应走下载代理（仅白名单内域名） */
export function shouldUseMediaDownloadProxy(sourceUrl: string): boolean {
  const trimmed = sourceUrl.trim();
  if (!trimmed) return false;
  try {
    const { hostname, protocol } = new URL(trimmed);
    if (!/^https?:$/i.test(protocol)) return false;
    return isMediaDownloadProxyHost(hostname);
  } catch {
    return false;
  }
}

/** 开发走 Vite 同源代理，线上走 local-media-download */
export function buildMediaDownloadProxyUrl(sourceUrl: string, filename: string): string {
  const params = new URLSearchParams({
    url: sourceUrl,
    filename,
  });
  if (import.meta.env.DEV) {
    return `${MEDIA_DOWNLOAD_DEV_PATH}?${params.toString()}`;
  }
  const apiBase = String(import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '');
  return `${apiBase}/local-media-download?${params.toString()}`;
}
