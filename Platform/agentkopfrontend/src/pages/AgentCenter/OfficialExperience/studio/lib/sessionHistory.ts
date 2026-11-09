/**
 * 把官方 Agent 的会话列表映射成「我的生成」记录。
 * 历史列表主要用于切换会话；状态只给当前打开的那条填准。
 * - 自主规划：当前会话用 SSE 实时状态
 * - 工作流：当前会话用已拉取的 pipeline-runs
 */
import type { Session } from '@/pages/AgentPlayground/types';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import type { GenerationRecord } from './types';

type KnownStatus = Exclude<GenerationRecord['status'], null>;

export type SessionHistoryMode = 'agent-loop' | 'pipeline';

function statusFromSession(session: Session): KnownStatus {
  const run = session.activeRunStatus;
  // 等后台任务 / 等用户回答时都未收尾
  if (run === 'running' || run === 'waiting_job' || run === 'waiting_user') return 'running';
  if (run === 'failed' || session.status === 'failed') return 'failed';
  return 'done';
}

/**
 * 当前会话以后台 job / ask_user / SSE error 为准。
 * 没有消息 / job / 账本时不默认「成功」，避免切换会话时先绿后红。
 */
function statusForCurrentLoop(sse: SSEState): GenerationRecord['status'] {
  if (sse.pendingAskUser) return 'waiting';
  if (sse.isRunning || sse.jobs.some((job) => job.status === 'submitted')) return 'running';
  if (sse.error || sse.jobs.some((job) => job.status === 'failed')) return 'failed';
  const hasSignal = sse.messages.length > 0 || sse.jobs.length > 0 || sse.ledger.length > 0;
  if (!hasSignal) return null;
  return 'done';
}

export interface SessionsToRecordsOptions {
  mode: SessionHistoryMode;
  /** 当前会话账本 / pipeline-runs 是否已就绪；未就绪时当前条不展示状态 */
  historyReady?: boolean;
  /**
   * 工作流：当前会话根据 pipeline-runs 推导出的状态。
   * 传入时优先于 sessions/query 的会话级字段。
   */
  currentPipelineStatus?: GenerationRecord['status'];
}

/** 会话标题往往就是提交时的摘要；没有标题时用 id 后缀占位 */
export function sessionsToRecords(
  sessions: Session[],
  currentSessionId: string,
  sseState: SSEState,
  options: SessionsToRecordsOptions = { mode: 'agent-loop' },
): GenerationRecord[] {
  const historyReady = options.historyReady ?? true;
  const isPipeline = options.mode === 'pipeline';

  return sessions
    .filter((session) => session.id.startsWith('ses_'))
    .map((session) => {
      const prompt = session.title?.trim() || `对话 ${session.id.slice(-8)}`;
      const current = session.id === currentSessionId;
      let status: GenerationRecord['status'];
      if (current) {
        if (!historyReady) {
          status = null;
        } else if (isPipeline) {
          status =
            options.currentPipelineStatus !== undefined
              ? options.currentPipelineStatus
              : statusFromSession(session);
        } else {
          status = statusForCurrentLoop(sseState);
        }
      } else {
        status = statusFromSession(session);
      }

      return {
        id: session.id,
        templateName: prompt,
        scene: '对话',
        prompt,
        intentPreview: current
          ? isPipeline
            ? '当前对话。点「生成视频」后才会启动 pipeline；历史会话需从左侧切换才会加载运行记录。'
            : '当前对话。Agent-Loop 每次生成使用独立会话，详情以实时输出为准。'
          : isPipeline
            ? '历史对话。点击切换后加载该会话的 pipeline-runs 与成品。'
            : '历史对话。点击可切换到该会话查看当时的输出。',
        payloadPreview: prompt,
        status,
        createdAt: session.createdAt ? new Date(session.createdAt).getTime() : Date.now(),
      } satisfies GenerationRecord;
    })
    .sort((left, right) => right.createdAt - left.createdAt);
}

/** 由当前会话的 pipeline-runs 摘要推导列表状态 */
export function statusFromPipelineRuns(
  runs: { status: string; createdAt?: string }[],
): GenerationRecord['status'] {
  if (runs.some((run) => run.status === 'running')) return 'running';
  if (runs.length === 0) return null;
  const latest = [...runs].sort((left, right) => {
    const leftAt = left.createdAt ? new Date(left.createdAt).getTime() : 0;
    const rightAt = right.createdAt ? new Date(right.createdAt).getTime() : 0;
    return rightAt - leftAt;
  })[0];
  if (latest.status === 'failed') return 'failed';
  if (latest.status === 'completed') return 'done';
  if (latest.status === 'running') return 'running';
  return null;
}
