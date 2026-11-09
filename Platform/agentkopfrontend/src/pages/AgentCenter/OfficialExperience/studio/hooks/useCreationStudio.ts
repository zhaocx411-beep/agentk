/**
 * 创作工作台状态：执行方式 / 模版 / 样本 / 动态入参 / 参考图 / 生成
 *
 * - agent-loop：去掉停止；每次生成优先新建 ses_（空会话可直接发），
 *   「我的生成」= 该 Agent 下的会话历史
 * - pipeline  ：POST /pipeline-runs（args）；「我的生成」同样是会话历史，
 *   切换会话后按 ses_ 拉取该会话的 pipeline-runs 与成品
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { message } from '@/utils/feedback';
import { isRequestCanceled } from '@/utils/request';
import { resolvePlatformMediaType, uploadMedia } from '@/services/media';
import { assertOutboundMediaWithinLimit } from '@/services/mediaSizeGuard';
import { getWorkflowRunDetail, getWorkflowRuns, runOfficialPipeline } from '@/services/workflow';
import {
  createOfficialLoopQuickSession,
  createOfficialWorkflowQuickSession,
} from '@/services/agentLoop';
import type { Session, WorkflowRunDetail } from '@/pages/AgentPlayground/types';
import type { SSEActions, SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import { bareWorkflowKey } from '@/utils/workflowKey';
import { isOfficialPendingSessionId } from '../../../utils';
import type { OfficialCatalogType } from '../../../types';
import { EMPTY_CREATION_TEMPLATE, fillPrompt } from '../lib/templates';
import {
  fetchCreationTemplatePage,
  resolveTemplateQueryPipelineName,
  TEMPLATE_PAGE_SIZE,
} from '../lib/templateTasks';
import {
  buildNodesFromRunDetail,
  buildPipelineNodes,
  collectArtifacts,
  pipelineProgress,
  pickLatestPipelineRun,
  pipelineRunArgs,
  normalizePipelineRunDetail,
  toRunDetail,
} from '../lib/pipeline';
import type { RunOutput } from '../lib/pipeline';
import { previewPayload, resolveExecutionSpec } from '../lib/executionSpecs';
import type { ExecutionSpec, FieldValue, FieldValues } from '../lib/executionSpecs';
import { mergeComicDramaUiFields } from '../lib/comicDrama';
import { buildSchemaDrivenSpec, emptyPipelineSpec, fieldOverridesFromInputs, refImagesFromPipelineInputs } from '../lib/schemaFields';
import { savePendingGenerate, takePendingGenerate } from '../lib/pendingGenerate';
import {
  peekPendingPipelineGenerate,
  savePendingPipelineGenerate,
  takePendingPipelineGenerate,
} from '../lib/pendingPipelineGenerate';
import { sessionsToRecords, statusFromPipelineRuns } from '../lib/sessionHistory';
import type { CreationSettings, GenerationRecord, ReferenceImage, SampleRow } from '../lib/types';
import type { OfficialUiField } from '@/types/officialCatalog';
import { PROJECT_OPTIONS } from '../lib/constants';

const INITIAL_CREATION_SETTINGS: CreationSettings = {
  scene: '效果广告',
  project: PROJECT_OPTIONS[0],
  tag: '',
};

function buildEmptyFieldValues(spec: Pick<ExecutionSpec, 'fields'>): FieldValues {
  const values: FieldValues = {};
  for (const field of spec.fields ?? []) {
    if (!field.key) continue;
    values[field.key] = field.control === 'array' ? [] : '';
  }
  return values;
}

interface UseCreationStudioOptions {
  sessionId: string;
  /** URL 上的 type：planning 走 agent-loop，workflow 走 pipeline-runs */
  catalogType: OfficialCatalogType;
  /** URL 上的 workflowKey，工作流模式下决定具体 Pipeline */
  workflowKey?: string;
  /**
   * official/defaults 的 pipelineName（如 generate_brand_ad）
   * 用于 pipeline-runs 的 pipeline 字段；TEMPLATE 查询会再映射到数据集 key
   */
  pipelineName?: string;
  /**
   * 入参 schema 字段（来自 getOfficialWorkflowUiSchema → capabilities.fields）
   * 有值则侧栏动态渲染，否则回退内置 executionSpecs / 通用 JSON
   */
  schemaFields?: OfficialUiField[];
  /** 自主规划 Agent 的 agentLoopId，用来每次生成时新建会话 */
  agentLoopId?: number;
  /** 该 Agent 下的会话列表（「我的生成」数据源） */
  sessions: Session[];
  /** 会话账本是否还在加载；agent-loop 加载完才能消费挂起的发送 */
  historyLoading: boolean;
  /** 跳到新会话（由页面层 navigate + 刷新列表） */
  onOpenSession: (sessionId: string) => void;
  /** 刷新会话列表 */
  onRefreshSessions: () => void;
  sseState: SSEState;
  sseActions: SSEActions;
}

const POLL_INTERVAL_MS = 2000;

export function useCreationStudio({
  sessionId,
  catalogType,
  workflowKey,
  pipelineName,
  schemaFields,
  agentLoopId,
  sessions,
  historyLoading,
  onOpenSession,
  onRefreshSessions,
  sseState,
  sseActions,
}: UseCreationStudioOptions) {
  /**
   * 工作流创作参数认 capabilities.fields；
   * 漫剧成片只叠加中文名，字段与模版都走接口。
   */
  const spec = useMemo(() => {
    if (catalogType !== 'workflow') {
      return resolveExecutionSpec(catalogType, workflowKey);
    }
    if (!workflowKey) {
      return emptyPipelineSpec('', '缺少 workflowKey，无法加载创作参数');
    }
    const fields = mergeComicDramaUiFields(workflowKey || pipelineName || '', schemaFields ?? []);
    if (fields.length > 0) {
      try {
        return buildSchemaDrivenSpec(workflowKey, fields);
      } catch {
        return emptyPipelineSpec(workflowKey, '配置字段解析失败，请稍后重试');
      }
    }
    return emptyPipelineSpec(
      workflowKey,
      '未从 capabilities 获取到配置字段，请确认 workflowKey 对应官配 Pipeline 是否已发布',
    );
  }, [catalogType, workflowKey, pipelineName, schemaFields]);
  /** 创作模版：usage=TEMPLATE + 数据集 pipelineName（漫剧成片等与 defaults 展示名可能不同） */
  const templatePipelineName = resolveTemplateQueryPipelineName(pipelineName || '', {
    workflowKey,
  });
  /** pipeline-runs 的 pipeline 名：取 official/defaults 的 pipelineName，缺失时退回 workflowKey 短名 */
  const runPipelineName = (pipelineName || '').trim() || bareWorkflowKey(workflowKey || '');
  const {
    data: templatePages,
    isLoading: templatesLoading,
    isError: templatesError,
    isFetchingNextPage: templatesLoadingMore,
    hasNextPage: templatesHasMore,
    fetchNextPage: fetchNextTemplatePage,
  } = useInfiniteQuery({
    queryKey: ['creation-templates', templatePipelineName, TEMPLATE_PAGE_SIZE],
    queryFn: ({ pageParam }) =>
      fetchCreationTemplatePage(templatePipelineName, pageParam, TEMPLATE_PAGE_SIZE, spec.id),
    initialPageParam: 1,
    getNextPageParam: (last) => (last.hasMore ? last.current + 1 : undefined),
    enabled: catalogType === 'workflow' && !!templatePipelineName,
    staleTime: 60_000,
    retry: 1,
  });

  const templateBundles = useMemo(
    () => (templatePages?.pages ?? []).flatMap((page) => page.bundles),
    [templatePages],
  );
  const templatesTotal = Math.max(
    templatePages?.pages?.[0]?.total ?? 0,
    templateBundles.length,
  );

  // 接口无数据时保持空列表，不再塞「自定义参数」假模版
  const templates = useMemo(() => templateBundles.map((item) => item.template), [templateBundles]);

  const sampleByTemplateId = useMemo(() => {
    const map = new Map<string, SampleRow>();
    for (const item of templateBundles) {
      map.set(item.template.id, item.sample);
    }
    return map;
  }, [templateBundles]);

  const loadMoreTemplates = useCallback(() => {
    if (!templatesHasMore || templatesLoadingMore) return;
    void fetchNextTemplatePage();
  }, [templatesHasMore, templatesLoadingMore, fetchNextTemplatePage]);

  const [templateId, setTemplateId] = useState('');
  /** 用户改过的入参；没改过的字段实时跟随模版与样本推导出的默认值 */
  const [fieldOverrides, setFieldOverrides] = useState<FieldValues>({});
  const [settings, setSettings] = useState<CreationSettings>(INITIAL_CREATION_SETTINGS);
  const [refImages, setRefImages] = useState<ReferenceImage[]>([]);
  const [refUploading, setRefUploading] = useState(false);
  /** 当前会话下 pipeline-runs 的成品快照 */
  const [runOutputs, setRunOutputs] = useState<RunOutput[]>([]);
  /** 当前会话 pipeline-runs 是否仍在拉取 */
  const [pipelineRunsLoading, setPipelineRunsLoading] = useState(false);
  /** 当前会话根据 pipeline-runs 推导的状态（覆盖列表当前条） */
  const [currentPipelineStatus, setCurrentPipelineStatus] =
    useState<GenerationRecord['status']>(null);
  /** 当前会话最近一次 pipeline-runs 详情（右侧执行过程） */
  const [pipelineDetail, setPipelineDetail] = useState<WorkflowRunDetail | null>(null);
  /** 「我的生成」选中项；两种模式都等于当前会话 id */
  const [selectedRecordId, setSelectedRecordId] = useState<string | null>(null);
  const [isCreatingSession, setIsCreatingSession] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const pendingSendLockRef = useRef(false);

  // 仅在有 templateId 时绑定模版；不再 silently 回落到 templates[0]
  const template = useMemo(() => {
    if (!templateId) return EMPTY_CREATION_TEMPLATE;
    return templates.find((item) => item.id === templateId) ?? EMPTY_CREATION_TEMPLATE;
  }, [templates, templateId]);

  const hasTemplate = Boolean(templateId && templates.some((item) => item.id === templateId));

  // 模版被下线时清空选择，不再自动替用户选中其它模版
  useEffect(() => {
    if (!templateId || templates.length === 0) return;
    if (!templates.some((item) => item.id === templateId)) {
      setTemplateId('');
      setFieldOverrides({});
    }
  }, [templates, templateId]);

  /** 商品信息已并入创作模版项，随模版切换 */
  const sample = useMemo(
    () => sampleByTemplateId.get(template.id) ?? null,
    [sampleByTemplateId, template.id],
  );

  /** 模版 prompt（接口已含商品文案；若仍有占位符则用样本再填） */
  const filledPrompt = useMemo(() => fillPrompt(template.prompt, sample), [template, sample]);

  const fieldDefaults = useMemo(() => {
    try {
      return spec.defaults({ filledPrompt, sample }) ?? {};
    } catch {
      return {};
    }
  }, [spec, filledPrompt, sample]);
  const fieldValues = useMemo(() => {
    if (!hasTemplate) {
      return { ...buildEmptyFieldValues(spec), ...fieldOverrides };
    }
    return { ...fieldDefaults, ...fieldOverrides };
  }, [fieldDefaults, fieldOverrides, hasTemplate, spec]);
  const isFormDirty = Object.keys(fieldOverrides).length > 0;

  const imageUrls = useMemo(
    () =>
      spec.usesImages
        ? refImages
            .map((ref) => ref.url)
            .filter(
              (url): url is string =>
                typeof url === 'string' && url.length > 0 && !url.startsWith('blob:'),
            )
        : [],
    [spec.usesImages, refImages],
  );

  /** 最终请求载荷；校验不过时带上原因，用来禁用生成按钮并提示 */
  const built = useMemo(() => {
    try {
      return spec.build(fieldValues, { imageUrls, settings });
    } catch (error) {
      return {
        ok: false as const,
        error: error instanceof Error ? error.message : '参数组装失败，请检查填写内容',
      };
    }
  }, [spec, fieldValues, imageUrls, settings]);
  /** 只看必填参数是否齐；创作模版仅用于预填，缺模版不阻塞生成 */
  const payloadError = built.ok ? null : built.error;
  const payloadPreview = built.ok ? previewPayload(built.payload) : '';

  /** 右栏「意图」的内容；提交时随记录一起留档，切回历史记录时展示当时的那份 */
  const intentPreview = useMemo(
    () =>
      JSON.stringify(
        {
          执行方式: spec.mode === 'pipeline' ? `pipeline · ${spec.id}` : 'agent-loop',
          模版: template.name,
          创作场景: settings.scene,
          归属项目: settings.project,
          作品标签: settings.tag || null,
          样本数据: sample
            ? {
                序号: sample.id,
                类目: sample.category,
                标题: sample.title,
                价格: sample.price,
                属性: sample.attributes,
                链接: sample.link,
                首图: sample.coverUrls,
                详情图: sample.detailImages,
              }
            : null,
          参考图: refImages.map((ref) => ref.url),
          预期链路: template.stages.map((stage) => `${stage.title}（${stage.badge}）`),
        },
        null,
        2,
      ),
    [spec, template, settings, sample, refImages],
  );

  // 换模版即整体重置为模版默认值，用户不需要手动清理上一个模版的残留
  const selectTemplate = useCallback((id: string) => {
    setTemplateId(id);
    setFieldOverrides({});
  }, []);

  const clearTemplate = useCallback(() => {
    setTemplateId('');
    setFieldOverrides({});
    setSettings(INITIAL_CREATION_SETTINGS);
    setRefImages((prev) => {
      for (const ref of prev) {
        if (ref.url.startsWith('blob:')) URL.revokeObjectURL(ref.url);
      }
      return [];
    });
  }, []);

  useEffect(() => {
    if (!hasTemplate) return;
    setSettings((prev) => ({ ...prev, scene: template.scene }));
  }, [hasTemplate, template.scene]);

  // 切换创作模版时，用 TEMPLATE 任务的 referImages 预填参考图
  const sampleImageKey = sample
    ? `${sample.id}:${[...sample.coverUrls, ...sample.detailImages].join('|')}`
    : '';
  useEffect(() => {
    if (!sample || !sampleImageKey) {
      setRefImages((prev) => {
        for (const ref of prev) {
          if (ref.url.startsWith('blob:')) URL.revokeObjectURL(ref.url);
        }
        return [];
      });
      return;
    }
    const urls = [...sample.coverUrls, ...sample.detailImages].filter(Boolean);
    setRefImages((prev) => {
      for (const ref of prev) {
        if (ref.url.startsWith('blob:')) URL.revokeObjectURL(ref.url);
      }
      return urls.slice(0, template.refImageLimit).map((url, index) => ({
        uid: `template-${sample.id}-${index}`,
        name: url,
        url,
      }));
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sampleImageKey 已编码样本图片
  }, [sampleImageKey, template.refImageLimit]);

  const setFieldValue = useCallback((key: string, value: FieldValue) => {
    setFieldOverrides((prev) => ({ ...prev, [key]: value }));
  }, []);

  const resetFields = useCallback(() => setFieldOverrides({}), []);

  const addRefImage = useCallback(
    async (file: File) => {
      if (refUploading) return;
      if (resolvePlatformMediaType(file) !== 'image') {
        message.warning('请上传 png / jpg / webp / gif 图片');
        return;
      }
      if (refImages.length >= template.refImageLimit) {
        message.warning(`最多选择 ${template.refImageLimit} 张参考图`);
        return;
      }
      setRefUploading(true);
      try {
        const result = await uploadMedia(file);
        const url = result.url;
        if (!url) {
          message.error('上传成功但未拿到文件地址');
          return;
        }
        setRefImages((prev) => {
          if (prev.length >= template.refImageLimit) {
            message.warning(`最多选择 ${template.refImageLimit} 张参考图`);
            return prev;
          }
          return [
            ...prev,
            { uid: `${file.name}-${Date.now()}`, name: result.name ?? file.name, url },
          ];
        });
      } catch (err: unknown) {
        message.error(err instanceof Error ? err.message : '上传失败');
      } finally {
        setRefUploading(false);
      }
    },
    [refImages.length, refUploading, template.refImageLimit],
  );

  const addRefUrl = useCallback(
    (url: string) => {
      const trimmed = url.trim();
      if (!trimmed) return;
      setRefImages((prev) => {
        if (prev.length >= template.refImageLimit) {
          message.warning(`最多选择 ${template.refImageLimit} 张参考图`);
          return prev;
        }
        return [...prev, { uid: `url-${Date.now()}`, name: trimmed, url: trimmed }];
      });
    },
    [template.refImageLimit],
  );

  const replaceRefImages = useCallback(
    (next: ReferenceImage[]) => {
      setRefImages((prev) => {
        const keep = new Set(next.map((item) => item.url));
        for (const ref of prev) {
          if (ref.url.startsWith('blob:') && !keep.has(ref.url)) {
            URL.revokeObjectURL(ref.url);
          }
        }
        return next.slice(0, template.refImageLimit);
      });
    },
    [template.refImageLimit],
  );

  /** 一键把样本自带的商品图当参考图，省掉用户找图上传这一步 */
  const useSampleImages = useCallback(() => {
    if (!sample) return;
    const pool = [...sample.coverUrls, ...sample.detailImages].filter(Boolean);
    setRefImages((prev) => {
      if (pool.length === 0) {
        message.info('当前模版没有商品图');
        return prev;
      }
      if (prev.length >= template.refImageLimit) {
        message.warning(`参考图已满 ${template.refImageLimit} 张`);
        return prev;
      }
      const existing = new Set(prev.map((ref) => ref.url));
      const picked = pool
        .filter((url) => !existing.has(url))
        .slice(0, template.refImageLimit - prev.length)
        .map((url, i) => ({ uid: `sample-${sample.id}-${i}-${Date.now()}`, name: url, url }));
      if (picked.length === 0) {
        // 已达上限以外：常见是选模版时已预填，再点「用商品图」没有新图可加
        message.info('商品图已全部添加');
        return prev;
      }
      return [...prev, ...picked];
    });
  }, [sample, template.refImageLimit]);

  const removeRefImage = useCallback((uid: string) => {
    setRefImages((prev) => {
      const target = prev.find((item) => item.uid === uid);
      if (target?.url.startsWith('blob:')) URL.revokeObjectURL(target.url);
      return prev.filter((item) => item.uid !== uid);
    });
  }, []);

  const nodes = useMemo(
    () =>
      spec.mode === 'pipeline'
        ? buildNodesFromRunDetail(pipelineDetail, template)
        : buildPipelineNodes(sseState, template, payloadPreview),
    [spec.mode, pipelineDetail, sseState, template, payloadPreview],
  );
  const progress = useMemo(() => pipelineProgress(nodes), [nodes]);
  const artifacts = useMemo(() => collectArtifacts(sseState, runOutputs), [sseState, runOutputs]);

  const records = useMemo(
    () =>
      sessionsToRecords(sessions, sessionId, sseState, {
        mode: spec.mode,
        historyReady: spec.mode === 'agent-loop' ? !historyLoading : !pipelineRunsLoading,
        currentPipelineStatus: spec.mode === 'pipeline' ? currentPipelineStatus : undefined,
      }),
    [
      sessions,
      sessionId,
      sseState,
      spec.mode,
      historyLoading,
      pipelineRunsLoading,
      currentPipelineStatus,
    ],
  );

  /**
   * agent-loop：只有「正在建会话 / 正在发消息」挡重复提交（可另开新会话）。
   * pipeline：当前会话仍有 running 的 pipeline-run 时挡重复提交。
   */
  const isBusy =
    isCreatingSession ||
    isSending ||
    (spec.mode === 'pipeline' && currentPipelineStatus === 'running');

  /* ---------- pipeline 模式：按会话拉 runs + 轮询 ---------- */

  const timersRef = useRef<Record<string, ReturnType<typeof setInterval>>>({});
  const sessionIdRef = useRef(sessionId);
  sessionIdRef.current = sessionId;
  const onRefreshSessionsRef = useRef(onRefreshSessions);
  onRefreshSessionsRef.current = onRefreshSessions;

  const stopPolling = useCallback((runId: string) => {
    const timer = timersRef.current[runId];
    if (!timer) return;
    clearInterval(timer);
    delete timersRef.current[runId];
  }, []);

  const stopAllPolling = useCallback(() => {
    for (const runId of Object.keys(timersRef.current)) stopPolling(runId);
  }, [stopPolling]);

  const startPolling = useCallback(
    (runId: string) => {
      stopPolling(runId);
      timersRef.current[runId] = setInterval(async () => {
        const sid = sessionIdRef.current;
        try {
          const detail = await getWorkflowRunDetail(sid, runId);
          if (!detail) return;
          setPipelineDetail(normalizePipelineRunDetail(detail));
          if (detail.status !== 'completed' && detail.status !== 'failed') return;

          stopPolling(runId);
          if (detail.status === 'completed' && detail.output != null) {
            setRunOutputs((prev) => {
              if (prev.some((item) => item.runId === runId)) return prev;
              return [{ runId, name: detail.name, output: detail.output }, ...prev];
            });
          }
          const stillRunning = Object.keys(timersRef.current).length > 0;
          if (stillRunning) {
            setCurrentPipelineStatus('running');
          } else if (detail.status === 'failed') {
            setCurrentPipelineStatus('failed');
          } else {
            setCurrentPipelineStatus('done');
          }
          onRefreshSessionsRef.current();
        } catch {
          // 瞬时失败下次再试
        }
      }, POLL_INTERVAL_MS);
    },
    [stopPolling],
  );

  const startPollingRef = useRef(startPolling);
  startPollingRef.current = startPolling;
  const stopAllPollingRef = useRef(stopAllPolling);
  stopAllPollingRef.current = stopAllPolling;
  const pipelineLoadRequestRef = useRef(0);
  const schemaFieldsRef = useRef(schemaFields);
  schemaFieldsRef.current = schemaFields;
  const specRef = useRef(spec);
  specRef.current = spec;

  const applyFormFromPipelineInputs = useCallback((inputs?: Record<string, unknown>) => {
    const currentSpec = specRef.current;
    const currentSchema = schemaFieldsRef.current;

    if (!inputs || Object.keys(inputs).length === 0) {
      setFieldOverrides({});
      if (currentSpec.usesImages) {
        setRefImages((prev) => {
          for (const ref of prev) {
            if (ref.url.startsWith('blob:')) URL.revokeObjectURL(ref.url);
          }
          return [];
        });
      }
      return;
    }

    setFieldOverrides(fieldOverridesFromInputs(currentSpec, currentSchema, inputs));

    if (!currentSpec.usesImages) return;

    if (currentSchema?.length) {
      setRefImages(refImagesFromPipelineInputs(currentSchema, inputs));
      return;
    }

    const imageUrls = Array.isArray(inputs.images)
      ? inputs.images.map((item) => String(item ?? '').trim()).filter(Boolean)
      : [];
    setRefImages(
      imageUrls.map((url, index) => ({
        uid: `restore-images-${index}`,
        name: url,
        url,
      })),
    );
  }, []);

  const loadPipelineRunsForSession = useCallback(async (sid: string) => {
    const requestId = pipelineLoadRequestRef.current + 1;
    pipelineLoadRequestRef.current = requestId;
    stopAllPollingRef.current();
    setRunOutputs([]);
    setPipelineDetail(null);
    setPipelineRunsLoading(true);
    setCurrentPipelineStatus(null);
    setFieldOverrides({});

    try {
      const res = await getWorkflowRuns(sid);
      if (pipelineLoadRequestRef.current !== requestId) return;
      const runs = res?.pipelineRuns ?? [];
      setCurrentPipelineStatus(statusFromPipelineRuns(runs));

      if (runs.length === 0) {
        applyFormFromPipelineInputs(undefined);
      }

      const outputs: RunOutput[] = [];
      for (const run of runs) {
        if (run.status === 'running') {
          startPollingRef.current(run.id);
          continue;
        }
        if (run.status === 'completed' && run.output != null) {
          outputs.push({ runId: run.id, name: run.name, output: run.output });
          continue;
        }
        if (run.status !== 'completed') continue;
        try {
          const detail = await getWorkflowRunDetail(sid, run.id);
          if (pipelineLoadRequestRef.current !== requestId || !detail) continue;
          if (detail.status === 'running') {
            startPollingRef.current(run.id);
            setCurrentPipelineStatus('running');
          } else if (detail.status === 'completed' && detail.output != null) {
            outputs.push({ runId: run.id, name: detail.name, output: detail.output });
          }
        } catch {
          // 单条失败不影响其余
        }
      }
      if (pipelineLoadRequestRef.current !== requestId) return;
      setRunOutputs(outputs);

      const latest = pickLatestPipelineRun(runs);
      if (latest) {
        try {
          const detail = await getWorkflowRunDetail(sid, latest.id);
          if (pipelineLoadRequestRef.current !== requestId) return;
          if (detail) {
            const normalized = normalizePipelineRunDetail(detail);
            setPipelineDetail(normalized);
            applyFormFromPipelineInputs(pipelineRunArgs(normalized));
            if (detail.status === 'running') startPollingRef.current(detail.id);
          } else {
            setPipelineDetail(toRunDetail(latest));
            applyFormFromPipelineInputs(pipelineRunArgs(latest));
          }
        } catch {
          if (pipelineLoadRequestRef.current === requestId) {
            setPipelineDetail(toRunDetail(latest));
            applyFormFromPipelineInputs(pipelineRunArgs(latest));
          }
        }
      }
    } catch (error) {
      if (pipelineLoadRequestRef.current === requestId) {
        setCurrentPipelineStatus(null);
        if (!isRequestCanceled(error)) {
          message.error('加载工作流运行记录失败');
        }
      }
    } finally {
      if (pipelineLoadRequestRef.current === requestId) setPipelineRunsLoading(false);
    }
  }, [applyFormFromPipelineInputs]);

  /** 会话变化：拉该会话最近一次 pipeline-runs 详情；占位 / 待提交会话跳过 */
  useEffect(() => {
    if (spec.mode !== 'pipeline') {
      setPipelineRunsLoading(false);
      setCurrentPipelineStatus(null);
      setRunOutputs([]);
      setPipelineDetail(null);
      return;
    }

    if (isOfficialPendingSessionId(sessionId) || peekPendingPipelineGenerate(sessionId)) {
      stopAllPollingRef.current();
      setRunOutputs([]);
      setPipelineDetail(null);
      setCurrentPipelineStatus(peekPendingPipelineGenerate(sessionId) ? 'running' : null);
      setPipelineRunsLoading(false);
      return;
    }

    stopAllPollingRef.current();
    void loadPipelineRunsForSession(sessionId);
  }, [spec.mode, sessionId, loadPipelineRunsForSession]);

  useEffect(() => {
    const timers = timersRef;
    return () => {
      for (const timer of Object.values(timers.current)) clearInterval(timer);
      timers.current = {};
    };
  }, []);

  /* ---------- 选中项跟随当前会话；agent-loop 挂起发送在账本就绪后消费 ---------- */

  useEffect(() => {
    setSelectedRecordId(sessionId);
  }, [sessionId]);

  // 占位跳转后的待跑 pipeline：进真实 ses_ 后自动提交
  useEffect(() => {
    if (spec.mode !== 'pipeline' || isOfficialPendingSessionId(sessionId)) return;
    const pending = takePendingPipelineGenerate(sessionId);
    if (!pending) return;
    setCurrentPipelineStatus('running');
    void (async () => {
      try {
        const data = await runOfficialPipeline(sessionId, {
          workflow: pending.workflow,
          pipeline: pending.pipeline || pending.workflow,
          args: pending.args,
        });
        if (!data?.id) throw new Error('无返回数据');
        setPipelineDetail(
          normalizePipelineRunDetail(
            toRunDetail({
              id: data.id,
              name: data.name,
              status: data.status || 'running',
              args: data.args,
              inputs: data.inputs ?? pipelineRunArgs(data) ?? pending.args,
              profileVersion: data.profileVersion,
              createdAt: data.createdAt,
            }),
          ),
        );
        startPollingRef.current(data.id);
        onRefreshSessionsRef.current();
      } catch (error) {
        const errorText = error instanceof Error ? error.message : '启动 Pipeline 失败';
        setCurrentPipelineStatus('failed');
        setPipelineDetail(
          toRunDetail({
            id: `local-${Date.now()}`,
            name: pending.workflow,
            status: 'failed',
            inputs: pending.args,
            error: errorText,
            createdAt: new Date().toISOString(),
          }),
        );
        message.error(errorText);
      }
    })();
  }, [spec.mode, sessionId]);

  useEffect(() => {
    if (spec.mode !== 'agent-loop') return;
    if (historyLoading || pendingSendLockRef.current) return;
    const pending = takePendingGenerate(sessionId);
    if (!pending) return;
    pendingSendLockRef.current = true;
    setIsSending(true);
    void (async () => {
      try {
        await sseActions.send(sessionId, pending.text);
        onRefreshSessions();
      } catch {
        message.error('发送失败，请重试');
      } finally {
        pendingSendLockRef.current = false;
        setIsSending(false);
      }
    })();
  }, [spec.mode, historyLoading, sessionId, sseActions, onRefreshSessions]);

  /* ---------- 提交 ---------- */

  const generate = useCallback(async () => {
    if (isBusy) return;
    if (refUploading) {
      message.warning('图片正在上传，请稍候');
      return;
    }
    if (!built.ok) {
      message.warning(built.error);
      return;
    }
    const { payload } = built;
    try {
      await assertOutboundMediaWithinLimit({
        payload: payload.mode === 'pipeline' ? payload.args : { text: payload.text },
        extraUrls: refImages.map((item) => item.url),
      });
    } catch (error) {
      message.error(error instanceof Error ? error.message : '素材超过 500MB 上限');
      return;
    }

    if (payload.mode === 'agent-loop') {
      // 账本没加载完时不能判断「空会话」，避免误在旧对话上叠消息
      if (historyLoading) {
        message.warning('会话加载中，请稍后再试');
        return;
      }
      if (agentLoopId == null) {
        message.error('缺少 agentLoopId，无法开始生成');
        return;
      }

      const sessionOccupied =
        sseState.messages.some((msg) => msg.role === 'user') ||
        sseState.isRunning ||
        sseState.jobs.some((job) => job.status === 'submitted');

      const pendingPayload = {
        text: payload.text,
        prompt: spec.summarize(payload),
        intentPreview,
        payloadPreview,
        templateName: template.name,
        scene: settings.scene,
      };

      // 空会话：直接在当前 ses_ 上发送，少一次跳转
      if (!sessionOccupied) {
        setIsSending(true);
        try {
          await sseActions.send(sessionId, payload.text);
          onRefreshSessions();
        } catch {
          message.error('发送失败，请重试');
        } finally {
          setIsSending(false);
        }
        return;
      }

      setIsCreatingSession(true);
      try {
        // quick session 不传 title：长 prompt 不应塞进会话标题，后端用默认名即可
        const session = await createOfficialLoopQuickSession(agentLoopId);
        if (!session?.id) throw new Error('新建会话失败');
        savePendingGenerate(session.id, pendingPayload);
        onRefreshSessions();
        onOpenSession(session.id);
      } catch (error) {
        message.error(error instanceof Error ? error.message : '新建会话失败，请稍后重试');
      } finally {
        setIsCreatingSession(false);
      }
      return;
    }

    // 工作流：占位 session 时先建会话并跳转，到新页再跑 pipeline-runs
    const args = payload.args as unknown as Record<string, unknown>;
    if (isOfficialPendingSessionId(sessionId)) {
      const key = workflowKey || payload.workflow;
      if (!key) {
        message.error('缺少 workflowKey，无法开启工作流体验');
        return;
      }
      setIsCreatingSession(true);
      try {
        const session = await createOfficialWorkflowQuickSession(key);
        if (!session?.id) throw new Error('新建会话失败');
        savePendingPipelineGenerate(session.id, {
          workflow: payload.workflow,
          pipeline: runPipelineName || payload.workflow,
          args,
        });
        onRefreshSessions();
        onOpenSession(session.id);
      } catch (error) {
        message.error(error instanceof Error ? error.message : '新建会话失败，请稍后重试');
      } finally {
        setIsCreatingSession(false);
      }
      return;
    }

    setCurrentPipelineStatus('running');
    try {
      const data = await runOfficialPipeline(sessionId, {
        workflow: payload.workflow,
        pipeline: runPipelineName || payload.workflow,
        args,
      });
      if (!data?.id) throw new Error('无返回数据');
      setPipelineDetail(
        normalizePipelineRunDetail(
          toRunDetail({
            id: data.id,
            name: data.name,
            status: data.status || 'running',
            args: data.args,
            inputs: data.inputs ?? pipelineRunArgs(data) ?? args,
            profileVersion: data.profileVersion,
            createdAt: data.createdAt,
          }),
        ),
      );
      startPolling(data.id);
      onRefreshSessions();
    } catch (error) {
      const errorText = error instanceof Error ? error.message : '启动 Pipeline 失败';
      setCurrentPipelineStatus('failed');
      setPipelineDetail(
        toRunDetail({
          id: `local-${Date.now()}`,
          name: payload.workflow,
          status: 'failed',
          inputs: args,
          error: errorText,
          createdAt: new Date().toISOString(),
        }),
      );
      message.error(errorText);
    }
  }, [
    isBusy,
    refUploading,
    built,
    historyLoading,
    agentLoopId,
    workflowKey,
    runPipelineName,
    sseState.messages,
    sseState.isRunning,
    sseState.jobs,
    spec,
    intentPreview,
    payloadPreview,
    template.name,
    settings.scene,
    sessionId,
    sseActions,
    onRefreshSessions,
    onOpenSession,
    startPolling,
    refImages,
  ]);

  /** 自主规划 / 工作流：点击 = 切换会话 */
  const selectRecord = useCallback(
    (id: string) => {
      if (id === sessionId) return;
      onOpenSession(id);
    },
    [sessionId, onOpenSession],
  );

  const selectedRecord = records.find((item) => item.id === selectedRecordId) ?? null;

  // 卸载时释放本地预览用的 objectURL；用 ref 兜住最新列表，避免闭包停在初始空数组
  const refImagesRef = useRef(refImages);
  refImagesRef.current = refImages;
  useEffect(
    () => () => {
      for (const ref of refImagesRef.current) {
        if (ref.url.startsWith('blob:')) URL.revokeObjectURL(ref.url);
      }
    },
    [],
  );

  return {
    spec,
    templates,
    templatesTotal,
    templatesLoading,
    templatesLoadingMore,
    templatesHasMore: !!templatesHasMore,
    templatesError,
    hasTemplate,
    loadMoreTemplates,
    projectOptions: PROJECT_OPTIONS,
    template,
    sample,
    filledPrompt,
    fieldValues,
    isFormDirty,
    payloadError,
    payloadPreview,
    intentPreview,
    settings,
    refImages,
    refUploading,
    records,
    selectedRecordId,
    selectedRecord,
    nodes,
    progress,
    artifacts,
    pipelineDetail,
    pipelineRunsLoading,
    isBusy,
    pipelineRunStatus: currentPipelineStatus,
    /** 官方体验去掉停止：始终不可取消 */
    canCancel: false,
    isStopping: false,
    waitingJobs:
      spec.mode === 'agent-loop' &&
      !sseState.isRunning &&
      sseState.jobs.some((job) => job.status === 'submitted'),
    selectTemplate,
    clearTemplate,
    setFieldValue,
    resetFields,
    setSettings,
    addRefImage,
    addRefUrl,
    replaceRefImages,
    setRefUploading,
    useSampleImages,
    removeRefImage,
    generate,
    selectRecord,
  };
}

export type CreationStudioModel = ReturnType<typeof useCreationStudio>;
