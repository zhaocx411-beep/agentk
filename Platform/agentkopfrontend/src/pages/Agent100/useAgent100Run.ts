/**
 * Agent100：启动执行、轮询状态、取消、新开一次
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { message } from '@/utils/feedback';
import {
  cancelAgent100Session,
  getAgent100Execution,
  startAgent100Execution,
} from './api';
import { isTerminalStatus } from './executionView';
import { AGENT100_TEXT } from './mock';
import type { Agent100Execution, Agent100HistoryItem } from './types';

const POLL_MS = 2500;

interface UseAgent100RunOptions {
  agentId: string;
  pipeline: string;
  defaultTitle?: string;
  onTerminal?: (result: Agent100Execution) => void;
  onStarted?: (result: Agent100Execution) => void;
}

export function useAgent100Run({ agentId, pipeline, defaultTitle, onTerminal, onStarted }: UseAgent100RunOptions) {
  const [runResult, setRunResult] = useState<Agent100Execution | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [loadingExecutionId, setLoadingExecutionId] = useState<string | null>(null);
  const [cancelling, setCancelling] = useState(false);
  const [hasSession, setHasSession] = useState(false);
  const pollTimerRef = useRef<number | null>(null);
  const sessionIdRef = useRef('');
  const pollGenRef = useRef(0);
  const ignoreRunningRef = useRef(false);

  const bumpPollGen = useCallback(() => {
    pollGenRef.current += 1;
    return pollGenRef.current;
  }, []);

  const bindSession = useCallback((sessionId?: string) => {
    const next = sessionId?.trim() ?? '';
    sessionIdRef.current = next;
    setHasSession(Boolean(next));
  }, []);

  const clearPoll = useCallback(() => {
    if (pollTimerRef.current != null) {
      window.clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  const pollOnce = useCallback(
    async (sessionId: string, executionId: string, gen: number) => {
      try {
        const next = await getAgent100Execution(agentId, sessionId, executionId);
        if (gen !== pollGenRef.current) return null;
        if (ignoreRunningRef.current && next.status === 'running') return next;
        setRunResult(next);
        if (isTerminalStatus(next.status)) {
          ignoreRunningRef.current = false;
          clearPoll();
          setSubmitting(false);
          onTerminal?.(next);
        }
        return next;
      } catch {
        if (gen !== pollGenRef.current) return null;
        clearPoll();
        setSubmitting(false);
        message.error('查询执行状态失败');
        return null;
      }
    },
    [agentId, clearPoll, onTerminal],
  );

  const startPoll = useCallback(
    (sessionId: string, executionId: string, gen: number) => {
      clearPoll();
      pollTimerRef.current = window.setInterval(() => {
        void pollOnce(sessionId, executionId, gen);
      }, POLL_MS);
    },
    [clearPoll, pollOnce],
  );

  useEffect(() => () => clearPoll(), [clearPoll]);

  useEffect(() => {
    bindSession('');
  }, [agentId, bindSession]);

  const startRun = useCallback(
    async (args: Record<string, unknown>, title?: string) => {
      const gen = bumpPollGen();
      ignoreRunningRef.current = false;
      clearPoll();
      setSubmitting(true);
      try {
        const result = await startAgent100Execution(agentId, {
          pipeline,
          title: title?.trim() || defaultTitle || undefined,
          args,
          ...(sessionIdRef.current ? { sessionId: sessionIdRef.current } : {}),
        });
        if (gen !== pollGenRef.current) return null;
        if (result.sessionId) bindSession(result.sessionId);
        setRunResult(result);
        if (!result.sessionId || !result.executionId) {
          setSubmitting(false);
          message.error('启动失败，请稍后重试');
          return null;
        }
        onStarted?.(result);
        if (isTerminalStatus(result.status)) {
          setSubmitting(false);
          onTerminal?.(result);
          return result;
        }
        startPoll(result.sessionId, result.executionId, gen);
        void pollOnce(result.sessionId, result.executionId, gen);
        return result;
      } catch {
        if (gen !== pollGenRef.current) return null;
        setSubmitting(false);
        message.error('启动失败，请稍后重试');
        return null;
      }
    },
    [agentId, pipeline, defaultTitle, bumpPollGen, bindSession, clearPoll, startPoll, pollOnce, onTerminal, onStarted],
  );

  const cancelRun = useCallback(async () => {
    if (!runResult?.sessionId) return false;
    const sessionId = runResult.sessionId;
    const executionId = runResult.executionId;
    const gen = bumpPollGen();
    clearPoll();
    setCancelling(true);
    try {
      await cancelAgent100Session(sessionId);
      if (gen !== pollGenRef.current) return false;
      ignoreRunningRef.current = true;
      bindSession('');
      setSubmitting(false);
      setRunResult((prev) => (prev ? { ...prev, status: 'cancelled' } : prev));
      onTerminal?.(runResult);
      const next = await pollOnce(sessionId, executionId, gen);
      if (next && !isTerminalStatus(next.status)) {
        startPoll(sessionId, executionId, gen);
      }
      message.success('已请求取消');
      return true;
    } catch {
      if (gen !== pollGenRef.current) return false;
      ignoreRunningRef.current = false;
      message.error('取消失败');
      return false;
    } finally {
      if (gen === pollGenRef.current) setCancelling(false);
    }
  }, [runResult, bumpPollGen, clearPoll, bindSession, pollOnce, startPoll, onTerminal]);

  const loadRun = useCallback(
    async (record: Agent100HistoryItem) => {
      const executionId = record.executionId?.trim();
      const sessionId = record.sessionId?.trim();
      if (!executionId || !sessionId) {
        message.warning('该记录缺少编号，无法加载');
        return null;
      }
      const gen = bumpPollGen();
      ignoreRunningRef.current = false;
      clearPoll();
      setLoadingExecutionId(executionId);
      bindSession(sessionId);
      const result = await pollOnce(sessionId, executionId, gen);
      setLoadingExecutionId(null);
      if (result && !isTerminalStatus(result.status)) {
        startPoll(sessionId, executionId, gen);
      }
      return result;
    },
    [bumpPollGen, clearPoll, bindSession, pollOnce, startPoll],
  );

  const resetRun = useCallback(() => {
    bumpPollGen();
    ignoreRunningRef.current = false;
    clearPoll();
    setRunResult(null);
    setSubmitting(false);
    setCancelling(false);
  }, [bumpPollGen, clearPoll]);

  const startNewSession = useCallback(() => {
    resetRun();
    bindSession('');
    message.success(AGENT100_TEXT.newSessionDone);
  }, [resetRun, bindSession]);

  const isRunning = submitting || runResult?.status === 'running';

  return {
    runResult,
    isRunning,
    loadingExecutionId,
    cancelling,
    hasSession,
    startRun,
    cancelRun,
    loadRun,
    resetRun,
    startNewSession,
  };
}
