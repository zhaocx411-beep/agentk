import { Tag } from 'antd';
import styles from './Agent100.module.less';

const STATUS_META: Record<string, { color: string; text: string }> = {
  running: { color: 'processing', text: '运行中' },
  completed: { color: 'success', text: '已完成' },
  failed: { color: 'error', text: '失败' },
  cancelled: { color: 'default', text: '已取消' },
};

interface Agent100RunStatusTagProps {
  status: string;
  bordered?: boolean;
}

function Agent100RunStatusTag({ status, bordered = false }: Agent100RunStatusTagProps) {
  const meta = STATUS_META[status] ?? { color: 'default', text: status };
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

export { Agent100RunStatusTag };
