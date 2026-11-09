/**
 * ToolGallery - 页面级静态配置常量
 */
import type { ToolAccent } from '@/pages/AgentCenter/AgentDetail/lib/toolMeta';
import type { ToolGalleryView } from './types';
import styles from './ToolGallery.module.less';

/** 视图白名单：推荐分区预览 / 全量列表 */
export const TOOL_GALLERY_VIEWS: readonly ToolGalleryView[] = ['recommend', 'all'];

/** 首屏骨架卡数量：铺满两行四列，避免加载结束时高度塌陷 */
export const TOOL_SKELETON_COUNT = 8;

/** 工具执行状态轮询间隔 */
export const CAPABILITY_RUN_POLL_MS = 2500;

/** 轮询超过此时长仍未进入终态：自动取消并提示超时 */
export const CAPABILITY_RUN_TIMEOUT_MS = 2 * 60 * 60 * 1000;

/** 超时取消后展示给用户的说明 */
export const CAPABILITY_RUN_TIMEOUT_MESSAGE = '执行超时，已自动取消';

/** 工具图标色相 → 卡片图标的 CSS Module class */
export const TOOL_ACCENT_CLASS: Record<ToolAccent, string> = {
  indigo: styles.accentIndigo,
  sky: styles.accentSky,
  emerald: styles.accentEmerald,
  amber: styles.accentAmber,
  rose: styles.accentRose,
  violet: styles.accentViolet,
};
