/**
 * Pipeline 节点状态的展示元数据（图标 / 颜色 / 文案）
 * 编辑器调试面板与对话区运行卡片共用，避免两处各写一份导致状态文案不一致
 */
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  LoadingOutlined,
} from '@ant-design/icons';
import type { ReactNode } from 'react';
import type { WorkflowNodeStatus, WorkflowRun } from '@/pages/AgentPlayground/types';

export interface NodeStatusMeta {
  icon: ReactNode;
  /** Ant Design Tag 的 color 取值 */
  color: string;
  label: string;
}

export const NODE_STATUS_META: Record<WorkflowNodeStatus, NodeStatusMeta> = {
  pending: { icon: <ClockCircleOutlined />, color: 'default', label: '等待' },
  running: { icon: <LoadingOutlined />, color: 'processing', label: '执行中' },
  completed: { icon: <CheckCircleOutlined />, color: 'success', label: '完成' },
  failed: { icon: <ExclamationCircleOutlined />, color: 'error', label: '失败' },
  skipped: { icon: <ClockCircleOutlined />, color: 'default', label: '跳过' },
};

export const PIPELINE_RUN_STATUS_META: Record<WorkflowRun['status'], { color: string; text: string }> =
  {
    running: { color: 'processing', text: '运行中' },
    completed: { color: 'success', text: '完成' },
    failed: { color: 'error', text: '失败' },
  };
