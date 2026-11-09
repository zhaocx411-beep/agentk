/**
 * SessionReview - 静态配置
 */

/** 查询条件与日期选择器统一使用的时间格式 */
export const TIME_FORMAT = 'YYYY-MM-DD HH:mm:ss';

/** 会话状态 → Tag 颜色 */
export const SESSION_STATUS_COLOR: Record<string, string> = {
  active: 'green',
  running: 'blue',
  failed: 'red',
  closed: 'default',
};

/** 活跃 Run 状态 → Tag 颜色 */
export const RUN_STATUS_COLOR: Record<string, string> = {
  running: 'processing',
  idle: 'default',
};
