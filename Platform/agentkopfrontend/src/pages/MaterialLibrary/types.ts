import type { DatasetTask } from '@/pages/OfflineRun/types';

export type { DatasetTask };

/** 素材库固定查询：TEMPLATE + pipelineName=asset */
export const MATERIAL_USAGE = 'TEMPLATE' as const;
export const MATERIAL_PIPELINE = 'asset';

export const MATERIAL_FETCH_PAGE_SIZE = 100;

export type MaterialKind = 'image' | 'audio' | 'video';

/** 素材库 category（extraFields.category，pipelineName=asset） */
export type MaterialCategory =
  | 'product_image'
  | 'model_image'
  | 'style_ref_image'
  | 'audio'
  | 'ecommerce_ad'
  | 'runway'
  | 'short_drama'
  | 'broll';

export type MaterialCategoryFilter = 'all' | MaterialCategory;

/** 短剧二级筛选：全部 / 未标注 extraFields.drama / 具体剧名 */
export const MATERIAL_DRAMA_ALL = 'all';
export const MATERIAL_DRAMA_UNLABELED = '__unlabeled__';
export type MaterialDramaFilter = typeof MATERIAL_DRAMA_ALL | typeof MATERIAL_DRAMA_UNLABELED | string;

/** 从 extraFields 拆出的单条图片 / 音频 / 视频 */
export interface MaterialFile {
  id: string;
  taskId: number;
  url: string;
  kind: MaterialKind;
  category?: MaterialCategory;
  /** extraFields.title，如 BGM 名称 */
  title?: string;
  /** extraFields.drama，短剧剧名 */
  drama?: string;
  /** extraFields.episode，集数 */
  episode?: number;
  /** extraFields.bytes，有则用于体积拦截；短剧已下发，其它类目可能没有 */
  bytes?: number;
  createdAt: string;
  updatedAt: string;
}
