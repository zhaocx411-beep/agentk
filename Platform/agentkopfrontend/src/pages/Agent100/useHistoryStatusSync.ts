/**
 * 列表里的 running 往往是启动快照，详情 GET 才会向执行引擎对账。
 * 只对「当前没在跟详情轮询」的运行中记录后台对账，避免和 useAgent100Run 打两轮。
 */
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { AGENT100_HISTORY_QUERY_KEY, getAgent100Execution } from './api';
import { AGENT100_HISTORY_POLL_MS } from './constants';
import type { Agent100HistoryData, Agent100HistoryItem } from './types';

function patchHistoryStatus(
  prev: Agent100HistoryData | undefined,
  executionId: string,
  next: { status: Agent100HistoryItem['status']; error?: string; hasOutput?: boolean },
): Agent100HistoryData | undefined {
  if (!prev) return prev;
  let changed = false;
  const items = prev.items.map((row) => {
    if (row.executionId !== executionId) return row;
    if (row.status === next.status && row.error === next.error) return row;
    changed = true;
    return {
      ...row,
      status: next.status,
      error: next.error,
      hasOutput: next.hasOutput || row.hasOutput,
    };
  });
  return changed ? { ...prev, items } : prev;
}

export function useHistoryStatusSync(
  agentId: string,
  items: Agent100HistoryItem[],
  skipExecutionId?: string,
) {
  const queryClient = useQueryClient();
  const itemsRef = useRef(items);
  itemsRef.current = items;

  const skip = skipExecutionId?.trim() ?? '';
  const runningKey = items
    .filter((item) => item.status === 'running' && item.sessionId && item.executionId)
    .map((item) => item.executionId)
    .filter((id) => id !== skip)
    .sort()
    .join(',');

  useEffect(() => {
    if (!agentId || !runningKey) return;
    let disposed = false;

    const targets = () =>
      itemsRef.current.filter(
        (item) =>
          item.status === 'running' &&
          item.sessionId &&
          item.executionId &&
          item.executionId !== skip,
      );

    const sync = async () => {
      await Promise.all(
        targets().map(async (item) => {
          try {
            const detail = await getAgent100Execution(agentId, item.sessionId, item.executionId);
            if (disposed || detail.status === item.status) return;
            queryClient.setQueryData<Agent100HistoryData>(
              [...AGENT100_HISTORY_QUERY_KEY, agentId],
              (prev) =>
                patchHistoryStatus(prev, item.executionId, {
                  status: detail.status,
                  error: detail.error,
                  hasOutput: Boolean(detail.output),
                }),
            );
          } catch {
            // 后台对账失败不打断列表
          }
        }),
      );
    };

    void sync();
    const timer = window.setInterval(() => void sync(), AGENT100_HISTORY_POLL_MS);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [agentId, queryClient, runningKey, skip]);
}
