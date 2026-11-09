/**
 * 会话活跃 Run 状态标识
 * activeRunStatus 语义（见 agentk 调试回看接口文档）：
 * - idle：建会话默认 / 发消息失败 / 回合结束且无未完成 job
 * - running：开始发消息 / 发消息成功
 * - waiting_job：落了 submitted 的异步 job / 终态时仍有未完成 job
 * - completed / failed：由游标更新传入
 */
import { Tag } from 'antd';

interface RunStatusTagProps {
  status?: string;
}

/** 仅展示进行态与失败态；idle/completed 不打扰列表 */
const STATUS_MAP: Record<string, { color: string; text: string }> = {
  running: { color: 'processing', text: '运行中' },
  waiting_job: { color: 'warning', text: '等待任务' },
  failed: { color: 'error', text: '失败' },
};

function RunStatusTag({ status }: RunStatusTagProps) {
  const meta = status ? STATUS_MAP[status] : undefined;
  if (!meta) return null;
  return <Tag color={meta.color}>{meta.text}</Tag>;
}

export default RunStatusTag;
