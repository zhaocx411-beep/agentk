/**
 * 精选轮播视觉素材（Unsplash 可商用图，已本地压缩）
 * 来源：images.unsplash.com，仅做氛围背景 / 右侧图标，不外链热加载
 */
import bg0 from '@/assets/featured/bg0.jpg';
import bg1 from '@/assets/featured/bg1.jpg';
import bg2 from '@/assets/featured/bg2.jpg';
import bg3 from '@/assets/featured/bg3.jpg';
import iconPlanning from '@/assets/featured/icon-planning.jpg';
import iconWorkflow from '@/assets/featured/icon-workflow.jpg';

/** 与 officialCoverThemeIndex 0–3 对齐 */
export const FEATURED_SLIDE_BACKGROUNDS = [bg0, bg1, bg2, bg3] as const;

export const FEATURED_VARIANT_ICONS = {
  planning: iconPlanning,
  workflow: iconWorkflow,
} as const;
