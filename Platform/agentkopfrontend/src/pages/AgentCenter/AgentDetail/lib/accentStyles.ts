/**
 * 能力卡片图标色相 → CSS Module class 映射（模型 / 工具共用）
 */
import type { ToolAccent } from './toolMeta';
import styles from '../AgentDetail.module.less';

export const ACCENT_CLASS: Record<ToolAccent, string> = {
  indigo: styles.toolIconIndigo,
  sky: styles.toolIconSky,
  emerald: styles.toolIconEmerald,
  amber: styles.toolIconAmber,
  rose: styles.toolIconRose,
  violet: styles.toolIconViolet,
};
