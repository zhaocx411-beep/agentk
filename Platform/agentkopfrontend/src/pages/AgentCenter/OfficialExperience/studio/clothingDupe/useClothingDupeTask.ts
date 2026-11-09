import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  cancelClothingDupeTask,
  CLOTHING_DUPE_TASK_TYPE,
  createClothingDupeTask,
  getClothingDupeTask,
  isClothingDupeActive,
  type ClothingDupeContent,
  type ClothingDupeTask,
} from '@/services/clothingDupe';
import { createKeffectsRequestId, mergeKeffectsTask, resolveCancelledTask } from '@/services/keffectsTasks';
import { resolvePlatformMediaType, uploadMedia } from '@/services/media';
import { message } from '@/utils/feedback';
import {
  DEFAULT_CLOTHING_DUPE_ASPECT_RATIO,
  DEFAULT_CLOTHING_DUPE_RESOLUTION,
  type ClothingDupeAspectRatio,
  type KeffectsResolution,
} from '../keffects/constants';
import { parseClothingDupeTemplate } from '../keffects/applyKeffectsTemplate';
import { execToTaskId, useKeffectsTaskPolling } from '../keffects/keffectsTaskRuntime';
import type { PipelineExecListRecord } from '@/services/pipelineExecs';
import {
  EMPTY_UPLOADING,
  MEDIA_KIND_BY_FIELD,
  MEDIA_LIMIT_BY_FIELD,
  mediaSetters,
  UPLOAD_TYPE_HINT,
  type ClothingDupeMedia,
  type ClothingDupeMediaField,
} from './mediaFields';

export type { ClothingDupeMedia, ClothingDupeMediaField };

type UploadingCounts = Record<ClothingDupeMediaField, number>;

export type ClothingDupeModel = ReturnType<typeof useClothingDupeTask>;

interface UseClothingDupeTaskOptions {
  onRefreshHistory?: () => void;
  onFocusTask?: (taskId: string) => void | Promise<void>;
}

export function useClothingDupeTask(options: UseClothingDupeTaskOptions = {}) {
  const { onFocusTask, onRefreshHistory } = options;
  const [sourceVideo, setSourceVideo] = useState<ClothingDupeMedia[]>([]);
  const [productImages, setProductImages] = useState<ClothingDupeMedia[]>([]);
  const [modelImages, setModelImages] = useState<ClothingDupeMedia[]>([]);
  const [bgm, setBgm] = useState<ClothingDupeMedia[]>([]);
  const [prompt, setPrompt] = useState('');
  const [voiceId, setVoiceId] = useState('');
  const [resolution, setResolution] = useState<KeffectsResolution>(DEFAULT_CLOTHING_DUPE_RESOLUTION);
  const [aspectRatio, setAspectRatio] = useState<ClothingDupeAspectRatio>(
    DEFAULT_CLOTHING_DUPE_ASPECT_RATIO,
  );
  const [task, setTask] = useState<ClothingDupeTask | null>(null);
  const [uploadingCounts, setUploadingCounts] = useState<UploadingCounts>(EMPTY_UPLOADING);
  const uploadingCountsRef = useRef<UploadingCounts>(EMPTY_UPLOADING);
  const [bgmUploading, setBgmUploading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  const { startPolling, stopPolling } = useKeffectsTaskPolling({
    onRefreshHistory,
    fetchTask: getClothingDupeTask,
    isActive: isClothingDupeActive,
  });

  const applyPolledTask = useCallback((next: ClothingDupeTask) => {
    setTask((prev) => mergeKeffectsTask(prev, next));
  }, []);

  const bumpUploading = useCallback((field: ClothingDupeMediaField, delta: 1 | -1) => {
    const next: UploadingCounts = {
      ...uploadingCountsRef.current,
      [field]: Math.max(0, uploadingCountsRef.current[field] + delta),
    };
    uploadingCountsRef.current = next;
    setUploadingCounts(next);
  }, []);

  const isFieldUploading = useCallback(
    (field: ClothingDupeMediaField) => uploadingCounts[field] > 0,
    [uploadingCounts],
  );

  const active = isClothingDupeActive(task?.status);
  const isBusy = submitting || active || cancelling;
  const isUploading = Object.values(uploadingCounts).some((count) => count > 0);
  const canGenerate =
    sourceVideo.length === 1 && productImages.length >= 1 && !isUploading && !bgmUploading;

  useEffect(
    () => () => {
      stopPolling();
    },
    [stopPolling],
  );

  const upload = useCallback(
    async (field: ClothingDupeMediaField, file: File) => {
      const expected = MEDIA_KIND_BY_FIELD[field];
      if (resolvePlatformMediaType(file) !== expected) {
        message.warning(UPLOAD_TYPE_HINT[expected]);
        return;
      }
      const current = {
        sourceVideo,
        productImages,
        modelImages,
        bgm,
      }[field];
      const limit = MEDIA_LIMIT_BY_FIELD[field];
      if (current.length + uploadingCountsRef.current[field] >= limit) {
        message.warning(`最多上传 ${limit} 个素材`);
        return;
      }
      bumpUploading(field, 1);
      try {
        const result = await uploadMedia(file);
        const url = result.url;
        const item = { id: `${field}-${Date.now()}`, name: result.name || file.name, url };
        mediaSetters({ setSourceVideo, setProductImages, setModelImages, setBgm })[field]((prev) => [
          ...prev,
          item,
        ]);
      } catch (error) {
        message.error(error instanceof Error ? error.message : '素材上传失败');
      } finally {
        bumpUploading(field, -1);
      }
    },
    [bgm, bumpUploading, modelImages, productImages, sourceVideo],
  );

  const removeMedia = useCallback((field: ClothingDupeMediaField, id: string) => {
    mediaSetters({ setSourceVideo, setProductImages, setModelImages, setBgm })[field]((items) =>
      items.filter((item) => item.id !== id),
    );
  }, []);

  const replaceMedia = useCallback((field: ClothingDupeMediaField, items: ClothingDupeMedia[]) => {
    mediaSetters({ setSourceVideo, setProductImages, setModelImages, setBgm })[field](
      items.slice(0, MEDIA_LIMIT_BY_FIELD[field]),
    );
  }, []);

  const generate = useCallback(async () => {
    if (!canGenerate || isBusy) return;
    stopPolling();
    const contents: ClothingDupeContent[] = [
      ...(prompt.trim() ? [{ type: 'prompt' as const, text: prompt.trim() }] : []),
      { type: 'source_video', url: sourceVideo[0].url },
      ...productImages.map((item) => ({ type: 'product_image' as const, url: item.url })),
      ...modelImages.map((item) => ({ type: 'model_image' as const, url: item.url })),
      ...(bgm[0] ? [{ type: 'bgm' as const, url: bgm[0].url }] : []),
      ...(voiceId.trim() ? [{ type: 'voice' as const, voiceId: voiceId.trim() }] : []),
    ];
    setSubmitting(true);
    try {
      const requestId = createKeffectsRequestId();
      const created = await createClothingDupeTask({
        taskType: CLOTHING_DUPE_TASK_TYPE,
        requestId,
        input: { contents, settings: { resolution, aspectRatio } },
      });
      const normalized = {
        ...created,
        taskType: created.taskType || CLOTHING_DUPE_TASK_TYPE,
        requestId: created.requestId || requestId,
      };
      setTask(normalized);
      void onFocusTask?.(normalized.taskId);
      if (isClothingDupeActive(normalized.status)) {
        startPolling(normalized.taskId, applyPolledTask);
      }
    } catch {
      // request 层已统一提示
    } finally {
      setSubmitting(false);
    }
  }, [
    aspectRatio,
    bgm,
    canGenerate,
    isBusy,
    modelImages,
    productImages,
    prompt,
    resolution,
    sourceVideo,
    voiceId,
    onFocusTask,
    startPolling,
    stopPolling,
    applyPolledTask,
  ]);

  const cancel = useCallback(async () => {
    if (!task?.taskId || cancelling) return;
    if (!active && task.status !== 'CANCELLING') return;
    setCancelling(true);
    try {
      const cancelled = await cancelClothingDupeTask(task.taskId);
      stopPolling();
      setTask((prev) => (prev ? resolveCancelledTask(prev, cancelled) : cancelled));
      onRefreshHistory?.();
    } catch {
      if (task.status !== 'CANCELLING') return;
      setTask((prev) => (prev ? { ...prev, status: 'PROCESSING' } : prev));
      startPolling(task.taskId, applyPolledTask);
    } finally {
      setCancelling(false);
    }
  }, [active, applyPolledTask, cancelling, onRefreshHistory, startPolling, stopPolling, task]);

  const outputEntries = useMemo(() => Object.entries(task?.output ?? {}), [task?.output]);

  const applyTemplateExtra = useCallback((extra: Record<string, unknown>) => {
    const next = parseClothingDupeTemplate(extra);
    setSourceVideo(next.sourceVideo);
    setProductImages(next.productImages);
    setModelImages(next.modelImages);
    setBgm(next.bgm);
    setPrompt(next.prompt);
    setVoiceId(next.voiceId);
    setResolution(next.resolution ?? DEFAULT_CLOTHING_DUPE_RESOLUTION);
    setAspectRatio(next.aspectRatio ?? DEFAULT_CLOTHING_DUPE_ASPECT_RATIO);
  }, []);

  const restoreFromExec = useCallback(
    (exec: PipelineExecListRecord) => {
      stopPolling();
      const next = parseClothingDupeTemplate(exec.argsJson ?? {});
      setSourceVideo(next.sourceVideo);
      setProductImages(next.productImages);
      setModelImages(next.modelImages);
      setBgm(next.bgm);
      setPrompt(next.prompt);
      setVoiceId(next.voiceId);
      setResolution(next.resolution ?? DEFAULT_CLOTHING_DUPE_RESOLUTION);
      setAspectRatio(next.aspectRatio ?? DEFAULT_CLOTHING_DUPE_ASPECT_RATIO);
      const taskId = execToTaskId(exec);
      setTask({
        taskId,
        status: exec.status,
        output: (exec.outputJson ?? null) as ClothingDupeTask['output'],
      });
      if (isClothingDupeActive(exec.status)) {
        startPolling(taskId, applyPolledTask);
      }
    },
    [applyPolledTask, startPolling, stopPolling],
  );

  return {
    sourceVideo,
    productImages,
    modelImages,
    bgm,
    prompt,
    voiceId,
    resolution,
    aspectRatio,
    task,
    outputEntries,
    isFieldUploading,
    isUploading,
    bgmUploading,
    cancelling,
    isBusy,
    canGenerate,
    active,
    setPrompt,
    setVoiceId,
    setResolution,
    setAspectRatio,
    upload,
    removeMedia,
    replaceMedia,
    setBgmUploading,
    generate,
    cancel,
    applyTemplateExtra,
    restoreFromExec,
  };
}
