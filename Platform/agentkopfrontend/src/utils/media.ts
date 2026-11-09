/**
 * 媒体链接判断工具：按扩展名识别视频 / 图片，兼容带 query 的 URL
 */

const VIDEO_EXT_RE = /\.(mp4|webm|mov|avi)(\?|$)/i;
const IMAGE_EXT_RE = /\.(png|jpg|jpeg|gif|webp|bmp|svg)(\?|$)/i;

/** 是否为视频 URL */
export function isVideoUrl(value: string): boolean {
  return VIDEO_EXT_RE.test(value);
}

/** 是否为图片 URL */
export function isImageUrl(value: string): boolean {
  return IMAGE_EXT_RE.test(value);
}
