/**
 * 工具详情页：启动执行、轮询状态、取消
 * 新调用通过 invoke 轮询，历史执行通过 runs 回溯；
 * 查询时的 600/NOT_FOUND 视为瞬时抖动，跳过并继续轮询；
 * 真正需要展示的失败是 code 200 且 status=failed。
 * 超过 CAPABILITY_RUN_TIMEOUT_MS 仍未终态则自动取消并提示超时。
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { message } from '@/utils/feedback';
import type { CapabilityRunHistoryRecord, CapabilityRunResult, CapabilityRunStatus } from '@/types/capabilityRuns';
import {
  cancelCapabilityRun,
  getCapabilityInvocation,
  getCapabilityRun,
  startCapabilityRun,
} from '../api';
import { CAPABILITY_RUN_POLL_MS, CAPABILITY_RUN_TIMEOUT_MESSAGE } from '../constants';
import {
  buildTimedOutRunResult,
  isCapabilityRunNotReady,
  resolveCapabilityRunDeadline,
} from '../lib/runErrors';
import {
  EXECUTION_BIND_GRACE_MS,
  remainingExecutionGrace,
} from '@/utils/executionGrace';

const TERMINAL: CapabilityRunStatus[] = ['completed', 'failed', 'cancelled'];

function isTerminal(status: CapabilityRunStatus): boolean {
  return TERMINAL.includes(status);
}

const GRACE_POLL_MS = 250;
type RunLookup = 'invocation' | 'history';

function keepCapabilityRunAlive(
  executionId: string,
  graceUntil: number,
) {
  const poll = () => {
    const remaining = remainingExecutionGrace(graceUntil);
    if (remaining <= 0) return;
    void getCapabilityInvocation(executionId).catch(() => undefined);
    window.setTimeout(poll, Math.min(GRACE_POLL_MS, remaining));
  };
  poll();
}

function resultFromHistoryRecord(record: CapabilityRunHistoryRecord): CapabilityRunResult {
  return {
    sessionId: record.sessionId,
    executionId: record.executionId,
    status: record.status,
    execution: {
      id: record.executionId,
      sessionId: record.sessionId,
      name: record.capabilityId,
      status: record.status,
      args: record.args,
      output: record.output ?? undefined,
      error: record.error,
      createdAt: record.createdAt,
      finishedAt: record.finishedAt,
    },
  };
}

interface UseToolCapabilityRunOptions {
  capabilityId: string;
  /** 执行进入终态（completed / failed / cancelled）时回调 */
  onTerminal?: (result: CapabilityRunResult) => void;
}

export function useToolCapabilityRun({ capabilityId, onTerminal }: UseToolCapabilityRunOptions) {
  const [runResult, setRunResult] = useState<CapabilityRunResult | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [loadingExecutionId, setLoadingExecutionId] = useState<string | null>(null);
  const [cancelling, setCancelling] = useState(false);
  const pollTimerRef = useRef<number | null>(null);
  const pollInFlightRef = useRef(false);
  const pollDeadlineRef = useRef(0);
  const finishingRef = useRef(false);
  const runResultRef = useRef<CapabilityRunResult | null>(null);
  const bindGraceUntilRef = useRef(0);
  const mountedRef = useRef(true);

  const clearPoll = useCallback(() => {
    if (pollTimerRef.current != null) {
      window.clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    pollInFlightRef.current = false;
  }, []);

  const markFinished = useCallback(() => {
    finishingRef.current = true;
    pollDeadlineRef.current = 0;
    bindGraceUntilRef.current = 0;
    clearPoll();
    setSubmitting(false);
  }, [clearPoll]);

  const applyTimeout = useCallback(
    async (executionId: string, sessionId: string) => {
      if (finishingRef.current) return;
      markFinished();
      try {
        await cancelCapabilityRun(sessionId, true);
      } catch {
        // session 可能已不存在，本地仍按超时收口
      }
      const timedOut = buildTimedOutRunResult(
        executionId,
        sessionId,
        capabilityId,
        runResultRef.current,
      );
      runResultRef.current = timedOut;
      setRunResult(timedOut);
      message.warning(CAPABILITY_RUN_TIMEOUT_MESSAGE);
      onTerminal?.(timedOut);
    },
    [capabilityId, markFinished, onTerminal],
  );

  const isPastDeadline = useCallback(() => {
    return pollDeadlineRef.current > 0 && Date.now() >= pollDeadlineRef.current;
  }, []);

  const pollOnce = useCallback(
    async (
      executionId: string,
      sessionId: string,
      lookup: RunLookup = 'invocation',
    ) => {
      if (finishingRef.current || pollInFlightRef.current) return null;
      if (isPastDeadline()) {
        void applyTimeout(executionId, sessionId);
        return null;
      }
      pollInFlightRef.current = true;
      try {
        const statusResult =
          lookup === 'invocation'
            ? await getCapabilityInvocation(executionId)
            : await getCapabilityRun(executionId, sessionId);
        const next =
          lookup === 'invocation' && statusResult.status === 'completed'
            ? await getCapabilityRun(statusResult.executionId, statusResult.sessionId)
            : statusResult;
        if (finishingRef.current) return null;
        runResultRef.current = next;
        setRunResult(next);
        if (isTerminal(next.status)) {
          markFinished();
          onTerminal?.(next);
          return next;
        }
        if (isPastDeadline()) {
          void applyTimeout(executionId, sessionId);
        }
        return next;
      } catch (error) {
        if (finishingRef.current) return null;
        if (isCapabilityRunNotReady(error)) {
          if (isPastDeadline()) {
            void applyTimeout(executionId, sessionId);
          }
          return null;
        }
        markFinished();
        message.error('查询执行状态失败');
        return null;
      } finally {
        pollInFlightRef.current = false;
      }
    },
    [applyTimeout, isPastDeadline, markFinished, onTerminal],
  );

  const startPoll = useCallback(
    (
      executionId: string,
      sessionId: string,
      immediate = false,
      lookup: RunLookup = 'invocation',
    ) => {
      clearPoll();
      finishingRef.current = false;
      if (immediate) {
        void pollOnce(executionId, sessionId, lookup);
      }
      pollTimerRef.current = window.setInterval(() => {
        if (finishingRef.current) return;
        if (isPastDeadline()) {
          void applyTimeout(executionId, sessionId);
          return;
        }
        void pollOnce(executionId, sessionId, lookup);
      }, CAPABILITY_RUN_POLL_MS);
    },
    [applyTimeout, clearPoll, isPastDeadline, pollOnce],
  );

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      const current = runResultRef.current;
      if (
        current &&
        !isTerminal(current.status) &&
        remainingExecutionGrace(bindGraceUntilRef.current) > 0
      ) {
        keepCapabilityRunAlive(
          current.executionId,
          bindGraceUntilRef.current,
        );
      }
      clearPoll();
    };
  }, [clearPoll]);

  const runTool = useCallback(
    async (args: Record<string, unknown>, title?: string) => {
      finishingRef.current = false;
      setSubmitting(true);
      try {
        const result = await startCapabilityRun({
          capabilityId,
          title: title?.trim() || undefined,
          args,
        });
        runResultRef.current = result;
        bindGraceUntilRef.current = Date.now() + EXECUTION_BIND_GRACE_MS;
        if (!mountedRef.current) {
          if (!isTerminal(result.status)) {
            keepCapabilityRunAlive(
              result.executionId,
              bindGraceUntilRef.current,
            );
          }
          return result;
        }
        setRunResult(result);
        if (isTerminal(result.status)) {
          markFinished();
          onTerminal?.(result);
          return result;
        }
        pollDeadlineRef.current = resolveCapabilityRunDeadline(result.execution?.createdAt);
        startPoll(result.executionId, result.sessionId, true);
        return result;
      } catch {
        setSubmitting(false);
        message.error('启动执行失败，请稍后重试');
        return null;
      }
    },
    [capabilityId, markFinished, startPoll, onTerminal],
  );

  const cancelRun = useCallback(async () => {
    if (!runResult?.sessionId) return false;
    setCancelling(true);
    try {
      await cancelCapabilityRun(runResult.sessionId);
      markFinished();
      try {
        const next = await getCapabilityRun(runResult.executionId, runResult.sessionId);
        runResultRef.current = next;
        setRunResult(next);
        if (isTerminal(next.status)) {
          onTerminal?.(next);
        }
      } catch {
        const prev = runResultRef.current;
        if (prev && !isTerminal(prev.status)) {
          const cancelled: CapabilityRunResult = {
            ...prev,
            status: 'cancelled',
            execution: {
              ...prev.execution,
              status: 'cancelled',
              finishedAt: new Date().toISOString(),
            },
          };
          runResultRef.current = cancelled;
          setRunResult(cancelled);
          onTerminal?.(cancelled);
        }
      }
      message.success('已请求取消执行');
      return true;
    } catch {
      message.error('取消执行失败');
      return false;
    } finally {
      setCancelling(false);
    }
  }, [runResult, markFinished, onTerminal]);

  const loadRun = useCallback(
    async (record: CapabilityRunHistoryRecord) => {
      const executionId = record.executionId?.trim();
      const sessionId = record.sessionId?.trim();
      if (!executionId) {
        message.warning('该记录缺少 executionId，无法加载');
        return null;
      }

      finishingRef.current = false;
      pollDeadlineRef.current = 0;
      bindGraceUntilRef.current = 0;
      clearPoll();
      setLoadingExecutionId(executionId);
      const summary = resultFromHistoryRecord(record);
      runResultRef.current = summary;
      setRunResult(summary);

      if (!sessionId) {
        setLoadingExecutionId(null);
        message.warning('该记录缺少 sessionId，仅展示摘要信息');
        return summary;
      }

      if (!isTerminal(record.status)) {
        pollDeadlineRef.current = resolveCapabilityRunDeadline(record.createdAt);
        if (isPastDeadline()) {
          setLoadingExecutionId(null);
          await applyTimeout(executionId, sessionId);
          return runResultRef.current;
        }
      }

      const lookup: RunLookup = executionId.startsWith('inv_')
        ? 'invocation'
        : 'history';
      const result = await pollOnce(executionId, sessionId, lookup);
      setLoadingExecutionId(null);
      const latest = result ?? runResultRef.current ?? summary;
      if (latest && !isTerminal(latest.status) && !finishingRef.current) {
        if (!pollDeadlineRef.current) {
          pollDeadlineRef.current = resolveCapabilityRunDeadline(record.createdAt);
        }
        startPoll(executionId, sessionId, false, lookup);
      }
      return latest;
    },
    [applyTimeout, clearPoll, isPastDeadline, pollOnce, startPoll],
  );

  const resetRun = useCallback(() => {
    finishingRef.current = false;
    pollDeadlineRef.current = 0;
    bindGraceUntilRef.current = 0;
    runResultRef.current = null;
    clearPoll();
    setRunResult(null);
    setSubmitting(false);
  }, [clearPoll]);

  const isRunning = submitting || runResult?.status === 'running';

  return {
    runResult,
    isRunning,
    submitting,
    loadingExecutionId,
    cancelling,
    runTool,
    cancelRun,
    loadRun,
    resetRun,
  };
}
