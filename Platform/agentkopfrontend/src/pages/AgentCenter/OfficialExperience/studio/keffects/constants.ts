import type { ProgressProps } from 'antd';
import dayjs from 'dayjs';

export const KEFFECTS_POLL_INTERVAL_MS = 2000;
export const GOODS_STUDIO_MAX_REF_IMAGES = 5;

export const KEFFECTS_RESOLUTIONS = ['720p', '1080p'] as const;
export type KeffectsResolution = (typeof KEFFECTS_RESOLUTIONS)[number];
export const KEFFECTS_RESOLUTION_OPTIONS: { label: string; value: KeffectsResolution }[] =
  KEFFECTS_RESOLUTIONS.map((value) => ({ label: value, value }));

export const CLOTHING_DUPE_ASPECT_RATIOS = [
  '9:16',
  '2:3',
  '3:4',
  '1:1',
  '4:3',
  '3:2',
  '16:9',
  '21:9',
] as const;

export type ClothingDupeAspectRatio = (typeof CLOTHING_DUPE_ASPECT_RATIOS)[number];

export const CLOTHING_DUPE_ASPECT_RATIO_OPTIONS: { label: string; value: ClothingDupeAspectRatio }[] =
  CLOTHING_DUPE_ASPECT_RATIOS.map((value) => ({ label: value, value }));

export const GOODS_STUDIO_ASPECT_RATIOS = ['9:16', '16:9'] as const;
export type GoodsStudioAspectRatio = (typeof GOODS_STUDIO_ASPECT_RATIOS)[number];
export const GOODS_STUDIO_ASPECT_RATIO_OPTIONS: { label: string; value: GoodsStudioAspectRatio }[] = [
  { label: '9:16 竖版', value: '9:16' },
  { label: '16:9 横版', value: '16:9' },
];

export const GOODS_STUDIO_DURATIONS = [15, 30, 60] as const;
export type GoodsStudioDuration = (typeof GOODS_STUDIO_DURATIONS)[number];
export const GOODS_STUDIO_DURATION_OPTIONS: { label: string; value: GoodsStudioDuration }[] = [
  { label: '15 秒（4 镜）', value: 15 },
  { label: '30 秒（7 镜）', value: 30 },
  { label: '60 秒（12 镜）', value: 60 },
];

export const DEFAULT_CLOTHING_DUPE_RESOLUTION: KeffectsResolution = '1080p';
export const DEFAULT_CLOTHING_DUPE_ASPECT_RATIO: ClothingDupeAspectRatio = '9:16';
export const DEFAULT_GOODS_STUDIO_RESOLUTION: KeffectsResolution = '720p';
export const DEFAULT_GOODS_STUDIO_ASPECT_RATIO: GoodsStudioAspectRatio = '9:16';
export const DEFAULT_GOODS_STUDIO_DURATION: GoodsStudioDuration = 15;

export const KEFFECTS_STATUS_META: Record<string, { text: string; color: string }> = {
  SUBMITTED: { text: '已提交', color: 'processing' },
  PROCESSING: { text: '生成中', color: 'processing' },
  QUEUED: { text: '排队中', color: 'processing' },
  RUNNING: { text: '执行中', color: 'processing' },
  SUCCEEDED: { text: '已完成', color: 'success' },
  FAILED: { text: '失败', color: 'error' },
  CANCELLING: { text: '取消中', color: 'warning' },
  CANCELLED: { text: '已取消', color: 'default' },
};

const TASK_PROGRESS_STATUS: Record<string, NonNullable<ProgressProps['status']>> = {
  FAILED: 'exception',
  SUCCEEDED: 'success',
  CANCELLED: 'normal',
  CANCELLING: 'normal',
};

export function resolveTaskStatusMeta(status?: string) {
  if (!status) return null;
  return KEFFECTS_STATUS_META[status] ?? { text: status, color: 'default' };
}

export function toTaskProgressStatus(status?: string): ProgressProps['status'] {
  if (!status) return 'active';
  return TASK_PROGRESS_STATUS[status] ?? 'active';
}

export function formatTaskTime(value?: number): string {
  return value ? dayjs(value).format('MM-DD HH:mm:ss') : '—';
}
