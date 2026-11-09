import { useCallback, useEffect, useState } from 'react';
import {
  cancelKeffectsTask,
  createKeffectsRequestId,
  createKeffectsTask,
  getKeffectsTask,
  GOODS_SCRAPER_TASK_TYPE,
  isKeffectsTaskActive,
  resolveCancelledTask,
  type KeffectsTask,
} from '@/services/keffectsTasks';
import { parseGoodsScraperTemplate } from './applyKeffectsTemplate';
import { execToTaskId, useKeffectsTaskPolling } from './keffectsTaskRuntime';
import type { PipelineExecListRecord } from '@/services/pipelineExecs';

export interface GoodsScraperOutput {
  refImage?: string[];
  goodsTitle?: string;
  goodsDescription?: string;
}

export type GoodsScraperTask = KeffectsTask<GoodsScraperOutput>;

function isHttpUrl(value: string): boolean {
  return /^https?:\/\//i.test(value.trim());
}

export type GoodsScraperModel = ReturnType<typeof useGoodsScraperTask>;

interface UseGoodsScraperTaskOptions {
  onRefreshHistory?: () => void;
  onFocusTask?: (taskId: string) => void | Promise<void>;
}

export function useGoodsScraperTask(options: UseGoodsScraperTaskOptions = {}) {
  const [url, setUrl] = useState('');
  const [task, setTask] = useState<GoodsScraperTask | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  const { startPolling, stopPolling } = useKeffectsTaskPolling({
    onRefreshHistory: options.onRefreshHistory,
    fetchTask: getKeffectsTask<GoodsScraperOutput>,
  });

  const trimmed = url.trim();
  const canGenerate = isHttpUrl(trimmed);
  const active = isKeffectsTaskActive(task?.status);
  const isBusy = submitting || active || cancelling;

  useEffect(
    () => () => {
      stopPolling();
    },
    [stopPolling],
  );

  const generate = useCallback(async () => {
    if (!canGenerate || isBusy) return;
    stopPolling();
    setSubmitting(true);
    try {
      const requestId = createKeffectsRequestId();
      const created = await createKeffectsTask<GoodsScraperOutput>({
        taskType: GOODS_SCRAPER_TASK_TYPE,
        requestId,
        input: {
          url: trimmed,
          contents: [{ type: 'url', url: trimmed }],
        },
      });
      const normalized = {
        ...created,
        taskType: created.taskType || GOODS_SCRAPER_TASK_TYPE,
        requestId: created.requestId || requestId,
      };
      setTask(normalized);
      void options.onFocusTask?.(normalized.taskId);
      if (isKeffectsTaskActive(normalized.status)) {
        startPolling(normalized.taskId, setTask);
      }
    } catch {
      // request 层已统一提示
    } finally {
      setSubmitting(false);
    }
  }, [canGenerate, isBusy, options, startPolling, stopPolling, trimmed]);

  const cancel = useCallback(async () => {
    if (!task?.taskId || cancelling) return;
    if (!active && task.status !== 'CANCELLING') return;
    setCancelling(true);
    try {
      const cancelled = await cancelKeffectsTask<GoodsScraperOutput>(task.taskId);
      stopPolling();
      setTask((prev) => (prev ? resolveCancelledTask(prev, cancelled) : cancelled));
      options.onRefreshHistory?.();
    } catch {
      if (task.status !== 'CANCELLING') return;
      setTask((prev) => (prev ? { ...prev, status: 'PROCESSING' } : prev));
      startPolling(task.taskId, setTask);
    } finally {
      setCancelling(false);
    }
  }, [active, cancelling, options, startPolling, stopPolling, task]);

  const output = task?.output ?? null;
  const images = (output?.refImage ?? []).filter(Boolean);
  const outputCount =
    images.length + (output?.goodsTitle ? 1 : 0) + (output?.goodsDescription ? 1 : 0);

  const applyTemplateExtra = useCallback((extra: Record<string, unknown>) => {
    setUrl(parseGoodsScraperTemplate(extra).url);
  }, []);

  const restoreFromExec = useCallback(
    (exec: PipelineExecListRecord) => {
      stopPolling();
      setUrl(parseGoodsScraperTemplate(exec.argsJson ?? {}).url);
      const taskId = execToTaskId(exec);
      setTask({
        taskId,
        status: exec.status,
        output: (exec.outputJson ?? null) as GoodsScraperOutput | null,
      });
      startPolling(taskId, setTask);
    },
    [startPolling, stopPolling],
  );

  return {
    url,
    setUrl,
    task,
    output,
    images,
    outputCount,
    isBusy,
    canGenerate,
    cancelling,
    generate,
    cancel,
    applyTemplateExtra,
    restoreFromExec,
  };
}
