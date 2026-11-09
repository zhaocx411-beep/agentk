/**
 * keffects 任务轮询与进度展示共用逻辑
 */
import { useCallback, useRef } from 'react';
import type { PipelineExecListRecord } from '@/services/pipelineExecs';
import {
  isKeffectsTaskActive,
  resolveFailedTask,
  type KeffectsTask,
} from '@/services/keffectsTasks';
import { KEFFECTS_POLL_INTERVAL_MS } from './constants';

export function execToTaskId(exec: PipelineExecListRecord): string {
  return exec.agentkExecId || exec.publicId;
}

/** 失败 / 已取消不展示进度条 */
export function shouldShowTaskProgress(status?: string): boolean {
  if (!status) return false;
  const upper = status.toUpperCase();
  return upper !== 'FAILED' && upper !== 'CANCELLED';
}

/** 成功固定 100%；进行中取 task.progress；失败由 shouldShowTaskProgress 控制不渲染 */
export function resolveTaskProgressPercent(
  task: Pick<KeffectsTask, 'status' | 'progress'> | null,
): number {
  if (!task?.status) return 0;
  const upper = task.status.toUpperCase();
  if (upper === 'SUCCEEDED') return 100;
  return Math.max(0, Math.min(100, task.progress ?? 0));
}

interface UseKeffectsTaskPollingOptions<T extends KeffectsTask> {
  onRefreshHistory?: () => void;
  fetchTask: (taskId: string) => Promise<T>;
  isActive?: (status?: string) => boolean;
}

export function useKeffectsTaskPolling<T extends KeffectsTask>({
  onRefreshHistory,
  fetchTask,
  isActive = isKeffectsTaskActive,
}: UseKeffectsTaskPollingOptions<T>) {
  const pollGenerationRef = useRef(0);
  const onRefreshHistoryRef = useRef(onRefreshHistory);
  onRefreshHistoryRef.current = onRefreshHistory;

  const poll = useCallback(
    async (taskId: string, generation: number, apply: (task: T) => void) => {
      try {
        const next = await fetchTask(taskId);
        if (pollGenerationRef.current !== generation) return;
        apply(next);
        if (isActive(next.status)) {
          window.setTimeout(() => void poll(taskId, generation, apply), KEFFECTS_POLL_INTERVAL_MS);
        } else {
          onRefreshHistoryRef.current?.();
        }
      } catch (error) {
        if (pollGenerationRef.current !== generation) return;
        apply(resolveFailedTask(null, taskId, error) as T);
        onRefreshHistoryRef.current?.();
      }
    },
    [fetchTask, isActive],
  );

  const startPolling = useCallback(
    (taskId: string, apply: (task: T) => void) => {
      const generation = pollGenerationRef.current + 1;
      pollGenerationRef.current = generation;
      void poll(taskId, generation, apply);
    },
    [poll],
  );

  const stopPolling = useCallback(() => {
    pollGenerationRef.current += 1;
  }, []);

  return { pollGenerationRef, startPolling, stopPolling };
}
