/** 官方体验页运行态：SSE 流式 / 后台 job / 错误 / 空闲 */
export type AgentRunStatus = 'running' | 'processing' | 'failed' | 'idle';

export const AGENT_RUN_STATUS_LABEL: Record<AgentRunStatus, string> = {
  running: '运行中',
  processing: '后台处理中',
  failed: '运行失败',
  idle: '空闲',
};

export function resolveAgentRunStatus(
  isRunning: boolean,
  hasPendingJobs: boolean,
  hasError: boolean,
): AgentRunStatus {
  if (isRunning) return 'running';
  if (hasPendingJobs) return 'processing';
  if (hasError) return 'failed';
  return 'idle';
}
