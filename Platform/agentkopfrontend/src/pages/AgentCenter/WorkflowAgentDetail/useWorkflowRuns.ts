/**
 * Pipeline 运行历史拉取、轮询与启动（对齐 Pipeline 运行页）
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { getWorkflowRunDetail, getWorkflowRuns, runWorkflow } from '@/services/workflow';
import type { WorkflowDefinition, WorkflowRun } from '@/pages/AgentPlayground/types';
import { detailToPatch, mapPipelineRunStatus } from '@/utils/workflowRun';
import { buildSubmitInputs } from './pipelineRuntime';

export function useWorkflowRuns(
  messageError: (text: string) => void,
  messageSuccess: (text: string) => void,
) {
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRun[]>([]);
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const pollingTimersRef = useRef<Record<string, ReturnType<typeof setInterval>>>({});

  const stopAllPolling = useCallback(() => {
    for (const timer of Object.values(pollingTimersRef.current)) clearInterval(timer);
    pollingTimersRef.current = {};
  }, []);

  const startPolling = useCallback((sid: string, runId: string) => {
    if (pollingTimersRef.current[runId]) clearInterval(pollingTimersRef.current[runId]);
    pollingTimersRef.current[runId] = setInterval(async () => {
      try {
        const detail = await getWorkflowRunDetail(sid, runId);
        if (!detail) return;
        const patch = detailToPatch(detail);
        setWorkflowRuns((prev) => prev.map((r) => (r.runId === runId ? { ...r, ...patch } : r)));
        if (detail.status === 'completed' || detail.status === 'failed') {
          clearInterval(pollingTimersRef.current[runId]);
          delete pollingTimersRef.current[runId];
          setIsRunning(false);
        }
      } catch {
        // 瞬时失败下次再试
      }
    }, 2000);
  }, []);

  useEffect(() => () => stopAllPolling(), [stopAllPolling]);

  const resetRuns = useCallback(() => {
    stopAllPolling();
    setWorkflowRuns([]);
    setActiveRunId(null);
    setIsRunning(false);
  }, [stopAllPolling]);

  const fetchRuns = useCallback(
    async (sid: string) => {
      const res = await getWorkflowRuns(sid);
      if (!res?.pipelineRuns) return;
      const runs: WorkflowRun[] = res.pipelineRuns.map((r) => ({
        runId: r.id,
        workflowId: r.name,
        workflowName: r.name,
        params: (r.inputs as Record<string, string | number>) ?? {},
        nodes: [],
        status: mapPipelineRunStatus(r.status),
        output: r.output ?? undefined,
        createdAt: r.createdAt,
      }));
      setWorkflowRuns(runs);
      for (const r of res.pipelineRuns) {
        try {
          const detail = await getWorkflowRunDetail(sid, r.id);
          if (!detail) continue;
          const patch = detailToPatch(detail);
          setWorkflowRuns((prev) =>
            prev.map((run) => (run.runId === r.id ? { ...run, ...patch } : run)),
          );
          if (r.status === 'running') startPolling(sid, r.id);
        } catch {
          // 单条失败不影响
        }
      }
      if (runs.length > 0) setActiveRunId((prev) => prev ?? runs[runs.length - 1].runId);
    },
    [startPolling],
  );

  const startRun = useCallback(
    async (
      sessionId: string,
      selectedWorkflow: WorkflowDefinition,
      paramMode: 'form' | 'json',
      rawArgsJson: string,
      workflowParams: Record<string, string | number>,
    ) => {
      if (workflowRuns.some((r) => r.status === 'running')) {
        messageError('已有 Pipeline 正在运行，请等待完成');
        return;
      }
      const parsed = buildSubmitInputs(selectedWorkflow, paramMode, rawArgsJson, workflowParams);
      if (!parsed.ok) {
        messageError(parsed.error);
        return;
      }
      setIsRunning(true);
      try {
        const data = await runWorkflow(sessionId, {
          workflow: selectedWorkflow.name,
          pipeline: selectedWorkflow.name,
          inputs: parsed.inputs,
        });
        if (!data) {
          messageError('启动 Pipeline 失败：无返回数据');
          setIsRunning(false);
          return;
        }
        const run: WorkflowRun = {
          runId: data.id,
          workflowId: selectedWorkflow.id,
          workflowName: data.name,
          params: parsed.inputs,
          nodes: [],
          status: data.status === 'running' ? 'running' : 'completed',
          createdAt: new Date().toISOString(),
        };
        setWorkflowRuns((prev) => [...prev, run]);
        setActiveRunId(data.id);
        messageSuccess(`Pipeline ${data.name} 已启动`);
        startPolling(sessionId, data.id);
      } catch {
        messageError('启动 Pipeline 失败');
        setIsRunning(false);
      }
    },
    [workflowRuns, startPolling, messageError, messageSuccess],
  );

  return {
    workflowRuns,
    activeRun: workflowRuns.find((r) => r.runId === activeRunId) ?? null,
    isRunning,
    hasRunningWorkflow: workflowRuns.some((r) => r.status === 'running'),
    resetRuns,
    fetchRuns,
    startRun,
    stopAllPolling,
  };
}
