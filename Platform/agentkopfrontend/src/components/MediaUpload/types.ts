import type { MaterialCategoryFilter } from '@/pages/MaterialLibrary/types';

/** 上传组件可选的媒体类型 */
export type MediaKind = 'image' | 'video' | 'audio';

/** 素材来源：本地 / 历史上传 / 素材库 / 作品集 */
export type MediaSource = 'local' | 'history' | 'material' | 'portfolio';

export type MediaPickerTab = 'portfolio' | 'uploads' | 'material';

export type MediaSortOrder = 'time-desc' | 'time-asc';

export type MediaUploadVariant = 'default' | 'compact';

/** 统一后的可选素材 */
export interface MediaAsset {
  id: string;
  url: string;
  /** 列表缩略图；缺省用 url（历史上传 cdn 预览 + bs 导入时与 url 不同） */
  thumbUrl?: string;
  name?: string;
  mediaType: MediaKind;
  source: MediaSource;
  createdAt?: string;
  /** 本地上传时的字节数；素材库勾选通常没有 */
  size?: number;
}

export interface MediaUploadProps {
  value: MediaAsset[];
  onChange: (value: MediaAsset[]) => void;
  /** 允许的媒体类型，默认仅图片 */
  accept?: MediaKind | MediaKind[];
  /** 最多可选数量，默认 8 */
  maxCount?: number;
  /** 投放区主文案，如「添加首尾帧图」 */
  title?: string;
  /** default 用于页面表单，compact 用于弹窗等高密度区域 */
  variant?: MediaUploadVariant;
  /** 素材库默认分类（单类型选择时避免「全部分类」首屏无结果） */
  materialCategoryDefault?: MaterialCategoryFilter;
  disabled?: boolean;
  onUploadingChange?: (uploading: boolean) => void;
}
