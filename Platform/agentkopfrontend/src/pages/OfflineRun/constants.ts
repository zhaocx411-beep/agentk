/**
 * OfflineRun - 状态色映射等静态配置
 */

/** 离线任务状态 → Tag 颜色 */
export const JOB_STATUS_COLOR: Record<string, string> = {
  RUNNING: 'blue',
  SUCCEEDED: 'green',
  FAILED: 'red',
  PENDING: 'default',
};

/** 单元格执行状态 → Tag 颜色 */
export const CELL_STATUS_COLOR: Record<string, string> = {
  DONE: 'green',
  ERROR: 'red',
  RUNNING: 'blue',
  QUEUED: 'default',
  PENDING: 'default',
};
