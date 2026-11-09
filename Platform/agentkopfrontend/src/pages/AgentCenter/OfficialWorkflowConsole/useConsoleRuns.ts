/**
 * 控制台：拉取 session 下 pipeline-runs，并对 running 项轮询详情
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { message } from '@/utils/feedback';
import { isRequestCanceled } from '@/utils/request';
import { getWorkflowRunDetail, getWorkflowRuns } from '@/services/workflow';
import type { WorkflowRunInfo } from '@/pages/AgentPlayground/types';
import { collectMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import { isOfficialPendingSessionId } from '../utils';
import type { ArtifactItem } from '../OfficialExperience/studio/lib/types';

const POLL_INTERVAL_MS = 2500;

export interface ConsoleRunRow extends WorkflowRunInfo {
  artifacts: ArtifactItem[];
}

function artifactsFromOutput(output: unknown, name: string): ArtifactItem[] {
  const { videos, images } = collectMediaUrls(output);
  return [
    ...videos.map((url) => ({ url, kind: 'video' as const, from: name })),
    ...images.map((url) => ({ url, kind: 'image' as const, from: name })),
  ];
}

function toRow(run: WorkflowRunInfo, output?: unknown): ConsoleRunRow {
  const resolved = output ?? run.output;
  return {
    ...run,
    output: resolved,
    artifacts: resolved != null ? artifactsFromOutput(resolved, run.name) : [],
  };
}

export function useConsoleRuns(sessionId: string) {
  const [runs, setRuns] = useState<ConsoleRunRow[]>([]);
  const [loading, setLoading] = useState(false);
  const timersRef = useRef<Record<string, ReturnType<typeof setInterval>>>({});
  const requestRef = useRef(0);
  const sessionRef = useRef(sessionId);
  sessionRef.current = sessionId;

  const stopPolling = useCallback((runId: string) => {
    const timer = timersRef.current[runId];
    if (!timer) return;
    clearInterval(timer);
    delete timersRef.current[runId];
  }, []);

  const stopAll = useCallback(() => {
    for (const id of Object.keys(timersRef.current)) stopPolling(id);
  }, [stopPolling]);

  const startPolling = useCallback(
    (runId: string) => {
      stopPolling(runId);
      timersRef.current[runId] = setInterval(async () => {
        const sid = sessionRef.current;
        try {
          const detail = await getWorkflowRunDetail(sid, runId);
          if (!detail) return;
          if (detail.status === 'running') return;
          stopPolling(runId);
          setRuns((prev) =>
            prev.map((row) =>
              row.id === runId
                ? toRow(
                    {
                      ...row,
                      status: detail.status,
                      output: detail.output,
                      finishedAt: detail.finishedAt,
                      name: detail.name || row.name,
                    },
                    detail.output,
                  )
                : row,
            ),
          );
        } catch {
          // 瞬时失败下次再试
        }
      }, POLL_INTERVAL_MS);
    },
    [stopPolling],
  );

  const reload = useCallback(async () => {
    const sid = sessionId.trim();
    // 占位 session 尚未创建，不打 pipeline-runs
    if (!sid || isOfficialPendingSessionId(sid)) {
      setRuns([]);
      return;
    }
    const requestId = requestRef.current + 1;
    requestRef.current = requestId;
    stopAll();
    setLoading(true);
    try {
      const res = await getWorkflowRuns(sid);
      if (requestRef.current !== requestId) return;
      const list = res?.pipelineRuns ?? [];
      const rows: ConsoleRunRow[] = [];
      for (const run of list) {
        if (run.status === 'running') {
          rows.push(toRow(run));
          startPolling(run.id);
          continue;
        }
        if (run.status === 'completed' && run.output != null) {
          rows.push(toRow(run));
          continue;
        }
        if (run.status === 'completed') {
          try {
            const detail = await getWorkflowRunDetail(sid, run.id);
            if (requestRef.current !== requestId) return;
            if (detail?.status === 'running') {
              rows.push(toRow({ ...run, status: 'running' }));
              startPolling(run.id);
            } else {
              rows.push(
                toRow(
                  {
                    ...run,
                    status: detail?.status ?? run.status,
                    finishedAt: detail?.finishedAt ?? run.finishedAt,
                    name: detail?.name || run.name,
                  },
                  detail?.output,
                ),
              );
            }
          } catch {
            rows.push(toRow(run));
          }
          continue;
        }
        rows.push(toRow(run));
      }
      if (requestRef.current === requestId) setRuns(rows);
    } catch (error) {
      if (requestRef.current === requestId) {
        setRuns([]);
        if (!isRequestCanceled(error)) {
          message.error('加载生成记录失败');
        }
      }
    } finally {
      if (requestRef.current === requestId) setLoading(false);
    }
  }, [sessionId, startPolling, stopAll]);

  useEffect(() => {
    void reload();
    return () => stopAll();
  }, [reload, stopAll]);

  return { runs, loading, reload };
}
