import { useCallback, useEffect, useState } from 'react';
import type { MediaAsset } from '@/components';
import {
  cancelKeffectsTask,
  createKeffectsRequestId,
  createKeffectsTask,
  getKeffectsTask,
  GOODS_STUDIO_TASK_TYPE,
  isKeffectsTaskActive,
  resolveCancelledTask,
  type KeffectsTask,
} from '@/services/keffectsTasks';
import { parseGoodsStudioTemplate } from './applyKeffectsTemplate';
import {
  DEFAULT_GOODS_STUDIO_ASPECT_RATIO,
  DEFAULT_GOODS_STUDIO_DURATION,
  DEFAULT_GOODS_STUDIO_RESOLUTION,
  GOODS_STUDIO_MAX_REF_IMAGES,
  type GoodsStudioAspectRatio,
  type GoodsStudioDuration,
  type KeffectsResolution,
} from './constants';
import { execToTaskId, useKeffectsTaskPolling } from './keffectsTaskRuntime';
import type { PipelineExecListRecord } from '@/services/pipelineExecs';

export type GoodsStudioResolution = KeffectsResolution;

export interface GoodsStudioOutput {
  videoUrl?: string;
  bucket?: string;
  key?: string;
}

export type GoodsStudioTask = KeffectsTask<GoodsStudioOutput>;

export interface GoodsStudioContent {
  type: 'ref_image' | 'goods_title' | 'goods_description';
  url?: string;
  text?: string;
}

export function useGoodsStudioTask(options: {
  onRefreshHistory?: () => void;
  onFocusTask?: (taskId: string) => void | Promise<void>;
} = {}) {
  const [refImages, setRefImages] = useState<MediaAsset[]>([]);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [resolution, setResolution] = useState<GoodsStudioResolution>(DEFAULT_GOODS_STUDIO_RESOLUTION);
  const [aspectRatio, setAspectRatio] = useState<GoodsStudioAspectRatio>(
    DEFAULT_GOODS_STUDIO_ASPECT_RATIO,
  );
  const [duration, setDuration] = useState<GoodsStudioDuration>(DEFAULT_GOODS_STUDIO_DURATION);
  const [uploading, setUploading] = useState(false);
  const [task, setTask] = useState<GoodsStudioTask | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  const { startPolling, stopPolling } = useKeffectsTaskPolling({
    onRefreshHistory: options.onRefreshHistory,
    fetchTask: getKeffectsTask<GoodsStudioOutput>,
  });

  const trimmedTitle = title.trim();
  const canGenerate = refImages.length >= 1 && trimmedTitle.length > 0 && !uploading;
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
    const contents: GoodsStudioContent[] = [
      ...refImages.map((item) => ({ type: 'ref_image' as const, url: item.url })),
      { type: 'goods_title', text: trimmedTitle },
      ...(description.trim()
        ? [{ type: 'goods_description' as const, text: description.trim() }]
        : []),
    ];
    setSubmitting(true);
    try {
      const requestId = createKeffectsRequestId();
      const created = await createKeffectsTask<GoodsStudioOutput>({
        taskType: GOODS_STUDIO_TASK_TYPE,
        requestId,
        input: {
          contents,
          settings: { resolution, aspectRatio, duration },
        },
      });
      const normalized = {
        ...created,
        taskType: created.taskType || GOODS_STUDIO_TASK_TYPE,
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
  }, [
    aspectRatio,
    canGenerate,
    description,
    duration,
    isBusy,
    options,
    refImages,
    resolution,
    startPolling,
    stopPolling,
    trimmedTitle,
  ]);

  const cancel = useCallback(async () => {
    if (!task?.taskId || cancelling) return;
    if (!active && task.status !== 'CANCELLING') return;
    setCancelling(true);
    try {
      const cancelled = await cancelKeffectsTask<GoodsStudioOutput>(task.taskId);
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

  const videoUrl = task?.output?.videoUrl?.trim() || '';
  const outputCount = videoUrl || task?.output?.key ? 1 : 0;

  const applyTemplateExtra = useCallback((extra: Record<string, unknown>) => {
    const next = parseGoodsStudioTemplate(extra);
    setRefImages(next.refImages);
    setTitle(next.title);
    setDescription(next.description);
    setResolution(next.resolution ?? DEFAULT_GOODS_STUDIO_RESOLUTION);
    setAspectRatio(next.aspectRatio ?? DEFAULT_GOODS_STUDIO_ASPECT_RATIO);
    setDuration(next.duration ?? DEFAULT_GOODS_STUDIO_DURATION);
  }, []);

  const restoreFromExec = useCallback(
    (exec: PipelineExecListRecord) => {
      stopPolling();
      const next = parseGoodsStudioTemplate(exec.argsJson ?? {});
      setRefImages(next.refImages);
      setTitle(next.title);
      setDescription(next.description);
      setResolution(next.resolution ?? DEFAULT_GOODS_STUDIO_RESOLUTION);
      setAspectRatio(next.aspectRatio ?? DEFAULT_GOODS_STUDIO_ASPECT_RATIO);
      setDuration(next.duration ?? DEFAULT_GOODS_STUDIO_DURATION);
      const taskId = execToTaskId(exec);
      setTask({
        taskId,
        status: exec.status,
        output: (exec.outputJson ?? null) as GoodsStudioOutput | null,
      });
      startPolling(taskId, setTask);
    },
    [startPolling, stopPolling],
  );

  return {
    refImages,
    setRefImages,
    title,
    setTitle,
    description,
    setDescription,
    resolution,
    setResolution,
    aspectRatio,
    setAspectRatio,
    duration,
    setDuration,
    maxRefImages: GOODS_STUDIO_MAX_REF_IMAGES,
    uploading,
    setUploading,
    task,
    videoUrl,
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

export type GoodsStudioModel = ReturnType<typeof useGoodsStudioTask>;
