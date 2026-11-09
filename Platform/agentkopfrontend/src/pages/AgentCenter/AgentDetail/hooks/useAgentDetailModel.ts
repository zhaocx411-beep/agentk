/**
 * useAgentDetailModel - 详情数据、版本选择、可编辑配置与草稿持久化
 * 「基于官方创建」：未保存前仅本地编辑，首次保存草稿时 create + saveVersionDraft
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { message, modal } from '@/utils/feedback';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AGENT_LOOPS_QUERY_KEY,
  AGENT_LOOP_DEFAULT_TEMPLATE_QUERY_KEY,
  CAPABILITIES_QUERY_KEY,
  MODELS_QUERY_KEY,
  agentLoopVersionsKey,
  createAgentLoop,
  fetchCapabilities,
  fetchModels,
  getAgentLoopDefaultTemplate,
  getAgentLoops,
  getAgentLoopVersions,
  publishAgentLoopVersion,
  saveVersionDraft,
} from '../../api';
import { OFFICIAL_TOOLS_QUERY_KEY, getOfficialTools } from '@/services/agentLoop';
import { chatModelItems, pickDefaultChatModel } from '@/utils/models';
import { sanitizeProfile, toToolNames, toToolRefs } from '@/utils/profile';
import {
  BUILTIN_AGENT_KEY,
  normalizeHttpTools,
  toAgentDetail,
  toBuiltinDetail,
  toFreshAgentDetail,
} from '../../utils';
import type { AgentDetailData } from '../../types';
import type { AgentLoop, AgentLoopVersion } from '@/types/agentLoop';
import type { OfficialToolItem } from '@/types/officialTools';
import {
  clearPendingOfficialAgent,
  isFromOfficialAgentId,
  readPendingOfficialAgent,
  writePendingOfficialAgent,
  type PendingOfficialAgentDraft,
} from '../lib/pendingOfficialAgent';
import { buildAgentDetailBaseline, isAgentDetailDirty } from '../lib/configCompare';
import { pruneToolDefaults, type ToolDefaultsMap } from '../lib/toolDefaultsUtils';

function toPendingOfficialDetail(
  pending: PendingOfficialAgentDraft,
  catalog: Awaited<ReturnType<typeof fetchCapabilities>> | null | undefined,
  officialTools?: OfficialToolItem[] | null,
): AgentDetailData {
  // official/tools 现已返回 ref；清洗时补进 profile.tools，创建会话才能带上
  const profile = sanitizeProfile(pending.profileYaml, catalog ?? null, officialTools ?? []);
  const tools = toToolNames(profile.tools);
  return {
    isBuiltin: false,
    name: pending.suggestedName?.trim() || '未命名 Agent',
    description: pending.description?.trim() || '基于官方模版，保存草稿后才会创建我的 Agent',
    status: 'draft',
    system: profile.system ?? '',
    defaultModel: profile.defaultModel ?? '',
    tools,
    pipelines: profile.pipelines ?? [],
    httpTools: normalizeHttpTools(profile.httpTools),
    rawProfile: profile,
    catalogTools: Array.from(new Set([...(catalog?.toolNames ?? []), ...tools])),
    catalogPipelines: catalog?.pipelines ?? [],
  };
}

interface CreateFromOfficialResult {
  agentId: number;
  version: AgentLoopVersion;
  created: boolean;
  loop?: AgentLoop;
}

function prependAgentLoop(prev: AgentLoop[] | undefined, item: AgentLoop): AgentLoop[] {
  const list = Array.isArray(prev) ? prev : [];
  if (list.some((row) => row.id === item.id)) return list;
  return [item, ...list];
}

export function useAgentDetailModel(agentId: string) {
  const isBuiltin = agentId === BUILTIN_AGENT_KEY;
  const isFromOfficial = isFromOfficialAgentId(agentId);
  const numericId = Number(agentId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [pending, setPending] = useState<PendingOfficialAgentDraft | null>(() =>
    isFromOfficial ? readPendingOfficialAgent() : null,
  );
  const [nameModalOpen, setNameModalOpen] = useState(false);
  const [selectedVersionId, setSelectedVersionId] = useState<number | undefined>();
  const [autoSavedAt, setAutoSavedAt] = useState<Date | null>(null);
  const [system, setSystem] = useState('');
  const [defaultModel, setDefaultModel] = useState('');
  const [tools, setTools] = useState<string[]>([]);
  const [toolDefaults, setToolDefaults] = useState<ToolDefaultsMap>({});

  /** 真实详情页的 agent id（官方暂存页在未完整保存前不走实体查询） */
  const entityId =
    !isBuiltin && !isFromOfficial && Number.isFinite(numericId) ? numericId : undefined;
  const hasEntity = entityId != null;
  /** 官方模版尚未落库：允许点「保存草稿」弹出命名（即使尚未改配置） */
  const isPendingUnsaved = isFromOfficial && pending != null;

  // 刷新残留：session 里已有 create 结果则进真实详情（仅挂载时检查一次）
  useEffect(() => {
    if (!isFromOfficial) return;
    const residual = readPendingOfficialAgent();
    if (residual?.createdAgentLoopId == null) return;
    const id = residual.createdAgentLoopId;
    clearPendingOfficialAgent();
    navigate(`/agents/${id}`, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const { data: catalog, isLoading: catalogLoading } = useQuery({
    queryKey: CAPABILITIES_QUERY_KEY,
    queryFn: fetchCapabilities,
  });
  const { data: officialToolsData } = useQuery({
    queryKey: OFFICIAL_TOOLS_QUERY_KEY,
    queryFn: getOfficialTools,
    staleTime: 5 * 60 * 1000,
  });
  const officialTools = useMemo(() => officialToolsData?.tools ?? [], [officialToolsData?.tools]);
  const { data: modelCatalog, isLoading: modelsLoading } = useQuery({
    queryKey: MODELS_QUERY_KEY,
    queryFn: fetchModels,
  });
  const { data: agentsData, isLoading: agentsLoading } = useQuery({
    queryKey: AGENT_LOOPS_QUERY_KEY,
    queryFn: getAgentLoops,
    enabled: !isBuiltin && hasEntity,
  });
  const { data: versionsData, isLoading: versionsLoading } = useQuery({
    queryKey: agentLoopVersionsKey(entityId ?? -1),
    queryFn: () => getAgentLoopVersions(entityId!),
    enabled: !isBuiltin && hasEntity,
  });
  const agents = useMemo(() => (Array.isArray(agentsData) ? agentsData : []), [agentsData]);
  const versions = useMemo(() => (Array.isArray(versionsData) ? versionsData : []), [versionsData]);
  const needsDefaultTemplate = hasEntity && !versionsLoading && versions.length === 0;
  const { data: defaultTemplate, isLoading: defaultTemplateLoading } = useQuery({
    queryKey: AGENT_LOOP_DEFAULT_TEMPLATE_QUERY_KEY,
    queryFn: getAgentLoopDefaultTemplate,
    enabled: needsDefaultTemplate,
    staleTime: 5 * 60 * 1000,
  });

  useEffect(() => {
    if (!hasEntity || versions.length === 0) return;
    setSelectedVersionId((prev) => {
      if (prev != null && versions.some((v) => v.id === prev)) return prev;
      return versions[0].id;
    });
  }, [hasEntity, versions]);

  const selectedVersion = useMemo(
    () => versions.find((v) => v.id === selectedVersionId) ?? versions[0] ?? null,
    [versions, selectedVersionId],
  );

  const detail = useMemo<AgentDetailData | null>(() => {
    if (isBuiltin) return catalog ? toBuiltinDetail(catalog) : null;
    if (isFromOfficial) {
      if (!pending) return null;
      return toPendingOfficialDetail(pending, catalog, officialTools);
    }
    if (entityId == null) return null;
    const agent = agents.find((a) => a.id === entityId);
    if (!agent) return null;
    if (versionsLoading && versions.length === 0) return null;
    if (versions.length === 0) {
      if (defaultTemplateLoading) return null;
      if (defaultTemplate?.profile) {
        return toFreshAgentDetail(agent, defaultTemplate.profile, catalog ?? null, officialTools);
      }
      return toAgentDetail(agent, versions, catalog ?? null, selectedVersion, officialTools);
    }
    return toAgentDetail(agent, versions, catalog ?? null, selectedVersion, officialTools);
  }, [
    isBuiltin,
    isFromOfficial,
    pending,
    catalog,
    officialTools,
    entityId,
    agents,
    versions,
    versionsLoading,
    defaultTemplate,
    defaultTemplateLoading,
    selectedVersion,
  ]);

  const detailKey = detail
    ? `${agentId}:${detail.agentId ?? 'pending'}:${detail.versionId ?? detail.versionLabel ?? ''}`
    : '';
  useEffect(() => {
    if (!detail) return;
    setSystem(detail.system);
    setDefaultModel(detail.defaultModel);
    setTools(detail.tools);
    setToolDefaults(detail.rawProfile.toolDefaults ?? {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detailKey]);

  const baseline = useMemo(
    () => (detail ? buildAgentDetailBaseline(detail, modelCatalog) : null),
    [detail, modelCatalog],
  );

  const dirty = useMemo(() => {
    if (!baseline) return false;
    const pruned = pruneToolDefaults(toolDefaults, tools) ?? {};
    return isAgentDetailDirty({ system, defaultModel, tools, toolDefaults: pruned }, baseline);
  }, [baseline, system, defaultModel, tools, toolDefaults]);

  // 版本未写 defaultModel 时，用模型目录 chatModels 首项补齐（不标 dirty）
  useEffect(() => {
    if (!modelCatalog) return;
    setDefaultModel((prev) => prev || pickDefaultChatModel(modelCatalog));
  }, [modelCatalog]);

  // 本地编辑同步回 pending，避免刷新丢失未保存改动（仅 dirty 后写，避免首屏空值覆盖）
  useEffect(() => {
    if (!isPendingUnsaved || !dirty) return;
    const current = readPendingOfficialAgent();
    if (!current) return;
    const pruned = pruneToolDefaults(toolDefaults, tools);
    writePendingOfficialAgent({
      ...current,
      profileYaml: {
        ...current.profileYaml,
        system,
        ...(defaultModel ? { defaultModel } : {}),
        tools: toToolRefs(tools, officialTools, catalog),
        ...(pruned ? { toolDefaults: pruned } : { toolDefaults: undefined }),
      },
    });
  }, [isPendingUnsaved, dirty, system, defaultModel, tools, toolDefaults, officialTools, catalog]);

  const handleVersionChange = (versionId: number) => {
    if (!hasEntity || versionId === selectedVersionId) return;
    if (!dirty) {
      setSelectedVersionId(versionId);
      return;
    }
    modal.confirm({
      title: '切换版本？',
      content: '当前有未保存的修改，切换后将丢失这些改动。',
      okText: '仍要切换',
      cancelText: '取消',
      onOk: () => {
        setSelectedVersionId(versionId);
      },
    });
  };

  const invalidate = useCallback(
    (id: number) => {
      queryClient.invalidateQueries({ queryKey: agentLoopVersionsKey(id) });
      queryClient.invalidateQueries({ queryKey: AGENT_LOOPS_QUERY_KEY });
    },
    [queryClient],
  );

  const buildProfileYaml = useCallback(() => {
    const pruned = pruneToolDefaults(toolDefaults, tools);
    const profile = {
      ...(detail?.rawProfile ?? {}),
      system,
      ...(defaultModel ? { defaultModel } : {}),
      // 优先 official/tools 的 ref，能力目录作兜底
      tools: toToolRefs(tools, officialTools, catalog),
    };
    if (pruned) {
      profile.toolDefaults = pruned;
    } else {
      delete profile.toolDefaults;
    }
    return profile;
  }, [detail?.rawProfile, system, defaultModel, tools, toolDefaults, officialTools, catalog]);

  const setToolsConfig = (nextTools: string[], nextDefaults: ToolDefaultsMap) => {
    setTools(nextTools);
    setToolDefaults(pruneToolDefaults(nextDefaults, nextTools) ?? {});
  };

  const persistDraft = useCallback(
    async (silent: boolean): Promise<boolean> => {
      // 官方模版未创建：手动保存弹命名窗；自动保存跳过
      if (isPendingUnsaved) {
        if (!silent) setNameModalOpen(true);
        return false;
      }
      if (isBuiltin || entityId == null) return false;
      try {
        const v = await saveVersionDraft(entityId, { profileYaml: buildProfileYaml() });
        setSelectedVersionId(v.id);
        invalidate(entityId);
        setAutoSavedAt(new Date());
        if (!silent) message.success(`草稿 v${v.version} 已保存`);
        return true;
      } catch {
        if (!silent) message.error('保存草稿失败');
        return false;
      }
    },
    [isPendingUnsaved, isBuiltin, entityId, buildProfileYaml, invalidate],
  );

  const saveMutation = useMutation({ mutationFn: () => persistDraft(false) });

  const createFromOfficialMutation = useMutation({
    mutationFn: async ({
      name,
      description,
    }: {
      name: string;
      description: string;
    }): Promise<CreateFromOfficialResult> => {
      if (!pending) throw new Error('暂存配置已失效，请返回官方页重新「基于此创建」');

      // 以 sessionStorage 为准，避免 React state 滞后；已有 id 则不再 create
      const latest = readPendingOfficialAgent() ?? pending;
      if (latest.createdAgentLoopId != null) {
        const v = await saveVersionDraft(latest.createdAgentLoopId, {
          profileYaml: buildProfileYaml(),
        });
        return { agentId: latest.createdAgentLoopId, version: v, created: false };
      }

      const loop = await createAgentLoop({ name, description });
      // create 成功立刻写入 session：即便后续草稿失败，重试也不再 create
      writePendingOfficialAgent({
        ...latest,
        suggestedName: name,
        description,
        createdAgentLoopId: loop.id,
      });

      const v = await saveVersionDraft(loop.id, { profileYaml: buildProfileYaml() });
      return { agentId: loop.id, version: v, created: true, loop };
    },
    onSuccess: (result) => {
      if (result.created && result.loop) {
        queryClient.setQueryData<AgentLoop[]>(AGENT_LOOPS_QUERY_KEY, (prev) =>
          prependAgentLoop(prev, result.loop!),
        );
      }
      queryClient.setQueryData(agentLoopVersionsKey(result.agentId), [result.version]);
      invalidate(result.agentId);
      clearPendingOfficialAgent();
      setPending(null);
      setNameModalOpen(false);
      setAutoSavedAt(new Date());
      message.success(`草稿 v${result.version.version} 已保存`);
      navigate(`/agents/${result.agentId}`, { replace: true });
    },
    onError: (error) => {
      const latest = readPendingOfficialAgent();
      if (latest?.createdAgentLoopId != null) {
        const id = latest.createdAgentLoopId;
        clearPendingOfficialAgent();
        setPending(null);
        setNameModalOpen(false);
        message.error('Agent 已创建，但草稿写入失败，请在详情页再次保存');
        navigate(`/agents/${id}`, { replace: true });
        return;
      }
      message.error(error instanceof Error ? error.message : '保存失败，请稍后重试');
    },
  });

  const confirmCreateFromOfficial = (name: string, description: string) => {
    const trimmed = name.trim();
    if (!trimmed) {
      message.warning('请填写 Agent 名称');
      return;
    }
    createFromOfficialMutation.mutate({ name: trimmed, description: description.trim() });
  };

  const publishMutation = useMutation({
    mutationFn: (versionId: number) => publishAgentLoopVersion(versionId),
    onSuccess: (v) => {
      message.success(`v${v.version} 已发布`);
      setSelectedVersionId(v.id);
      if (entityId != null) invalidate(entityId);
    },
    onError: () => message.error('发布失败'),
  });

  const handlePublish = () => {
    if (isPendingUnsaved) {
      message.warning('请先保存草稿创建我的 Agent，再发布');
      setNameModalOpen(true);
      return;
    }
    if (dirty) {
      message.warning('有未保存的修改，请先保存草稿再发布');
      return;
    }
    const draft =
      selectedVersion?.status === 'draft'
        ? selectedVersion
        : versions.find((v) => v.status === 'draft');
    if (!draft) {
      message.info('暂无草稿版本，请先保存草稿');
      return;
    }
    publishMutation.mutate(draft.id);
  };

  /** 返回 / 离开：有改动或官方模版尚未落库时提示保存 */
  const confirmLeave = useCallback(
    (leave: () => void) => {
      if (isPendingUnsaved) {
        modal.confirm({
          title: '尚未保存为我的 Agent',
          content: '当前配置仍在本地，离开将丢失未保存的草稿。是否先保存？',
          okText: '去保存',
          cancelText: '直接离开',
          onOk: () => setNameModalOpen(true),
          onCancel: () => {
            clearPendingOfficialAgent();
            setPending(null);
            leave();
          },
        });
        return;
      }
      if (dirty) {
        modal.confirm({
          title: '有未保存的修改',
          content: '配置已变更但尚未保存草稿，离开将丢失这些改动。',
          okText: '去保存',
          cancelText: '直接离开',
          onOk: () => {
            void persistDraft(false);
          },
          onCancel: leave,
        });
        return;
      }
      leave();
    },
    [isPendingUnsaved, dirty, persistDraft],
  );

  const versionMap = useMemo(() => {
    const map: Record<number, number> = {};
    for (const v of versions) map[v.id] = v.version;
    return map;
  }, [versions]);

  const loading =
    catalogLoading ||
    modelsLoading ||
    (!isBuiltin &&
      !isFromOfficial &&
      (agentsLoading || versionsLoading || (needsDefaultTemplate && defaultTemplateLoading)));

  return {
    isBuiltin,
    isFromOfficial,
    isPendingUnsaved,
    officialAgentLoopId: pending?.officialAgentLoopId,
    detail,
    versions,
    selectedVersion,
    versionMap,
    loading,
    modelsLoading,
    system,
    defaultModel,
    modelCatalog,
    // 回显用 defaultModel（profile）；下拉选项来自 /agentk/models
    chatModels: chatModelItems(modelCatalog, defaultModel),
    tools,
    /** 创建会话 / 保存草稿时写入 profile.tools[].ref */
    officialTools,
    catalog,
    toolDefaults,
    dirty,
    /** 保存按钮：有改动，或官方模版首次尚未创建 */
    canSaveDraft: dirty || isPendingUnsaved,
    autoSavedAt,
    nameModalOpen,
    setNameModalOpen,
    pendingSuggestedName: pending?.suggestedName ?? '',
    pendingDescription: pending?.description ?? '',
    creatingFromOfficial: createFromOfficialMutation.isPending,
    confirmCreateFromOfficial,
    confirmLeave,
    setSystem,
    setDefaultModel,
    setToolsConfig,
    handleVersionChange,
    persistDraft,
    saveMutation,
    publishMutation,
    handlePublish,
  };
}
