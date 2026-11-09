/**
 * Capability 执行状态展示
 */
import { Tag } from 'antd';
import type { CapabilityRunStatus } from '@/types/capabilityRuns';
import styles from '../ToolGallery.module.less';

const RUN_STATUS_META: Record<string, { color: string; text: string }> = {
  running: { color: 'processing', text: '运行中' },
  completed: { color: 'success', text: '已完成' },
  failed: { color: 'error', text: '失败' },
  cancelled: { color: 'default', text: '已取消' },
};

interface RunStatusTagProps {
  status: CapabilityRunStatus;
  bordered?: boolean;
}

export function RunStatusTag({ status, bordered = false }: RunStatusTagProps) {
  const meta = RUN_STATUS_META[status] ?? { color: 'default', text: status };
  return (
    <Tag
      bordered={bordered}
      color={meta.color}
      className={status === 'running' ? styles.runStatusTagRunning : undefined}
    >
      {meta.text}
    </Tag>
  );
}
