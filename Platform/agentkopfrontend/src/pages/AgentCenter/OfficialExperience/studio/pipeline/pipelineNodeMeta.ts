import styles from '../CreationStudio.module.less';
import type { PipelineNode } from '../lib/types';

export const PIPELINE_NODE_STATUS_TEXT: Record<PipelineNode['status'], string> = {
  done: '已完成',
  running: '执行中',
  failed: '失败',
  pending: '等待中',
};

export const PIPELINE_NODE_STATUS_CLASS: Record<PipelineNode['status'], string> = {
  done: styles.statusDone,
  running: styles.statusRunning,
  failed: styles.statusFailed,
  pending: styles.statusPending,
};

export const PIPELINE_NODE_TAG_COLOR: Record<PipelineNode['status'], string> = {
  done: 'success',
  running: 'processing',
  failed: 'error',
  pending: 'default',
};
