/**
 * 工作流 Agent 详情：实体 / 版本 / definitionJson 草稿持久化
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { message, modal } from '@/utils/feedback';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { prettyDefinitionJson } from '@/utils/workflowDefinition';
import {
  WORKFLOW_ENTITIES_QUERY_KEY,
  getWorkflowEntities,
  getWorkflowVersions,
  publishWorkflowVersion,
  saveWorkflowVersionDraft,
  workflowVersionsKey,
} from '../api';

export function useWorkflowAgentModel(workflowId: number) {
  const queryClient = useQueryClient();
  const [selectedVersionId, setSelectedVersionId] = useState<number | undefined>();
  const [definitionJson, setDefinitionJson] = useState('{\n  "workflows": {}\n}');
  const [dirty, setDirty] = useState(false);
  const [autoSavedAt, setAutoSavedAt] = useState<Date | null>(null);

  const { data: entities = [], isLoading: entitiesLoading } = useQuery({
    queryKey: WORKFLOW_ENTITIES_QUERY_KEY,
    queryFn: getWorkflowEntities,
  });
  const { data: versions = [], isLoading: versionsLoading } = useQuery({
    queryKey: workflowVersionsKey(workflowId),
    queryFn: () => getWorkflowVersions(workflowId),
    enabled: Number.isFinite(workflowId),
  });

  const entity = entities.find((item) => item.id === workflowId) ?? null;

  useEffect(() => {
    if (versions.length === 0) return;
    setSelectedVersionId((prev) => {
      if (prev != null && versions.some((v) => v.id === prev)) return prev;
      return versions[0].id;
    });
  }, [versions]);

  const selectedVersion = useMemo(
    () => versions.find((v) => v.id === selectedVersionId) ?? versions[0] ?? null,
    [versions, selectedVersionId],
  );

  const versionKey = selectedVersion ? `${selectedVersion.id}:${selectedVersion.version}` : '';
  useEffect(() => {
    if (!selectedVersion) return;
    setDefinitionJson(prettyDefinitionJson(selectedVersion.definitionJson));
    setDirty(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [versionKey]);

  const status: 'published' | 'draft' =
    selectedVersion?.status === 'draft'
      ? 'draft'
      : entity && entity.latestVersion > 0
        ? 'published'
        : 'draft';

  const handleDefinitionChange = (value: string) => {
    setDefinitionJson(value);
    setDirty(true);
  };

  /** 版本与左侧定义同一轮更新，避免右侧按上一版 definitionJson 推导 */
  const applyVersion = (versionId: number) => {
    const next = versions.find((item) => item.id === versionId);
    setSelectedVersionId(versionId);
    if (next) setDefinitionJson(prettyDefinitionJson(next.definitionJson));
    setDirty(false);
  };

  const handleVersionChange = (versionId: number) => {
    if (versionId === selectedVersionId) return;
    if (!dirty) {
      applyVersion(versionId);
      return;
    }
    modal.confirm({
      title: '切换版本？',
      content: '当前有未保存的修改，切换后将丢失这些改动。',
      okText: '仍要切换',
      cancelText: '取消',
      onOk: () => applyVersion(versionId),
    });
  };

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: workflowVersionsKey(workflowId) });
    queryClient.invalidateQueries({ queryKey: WORKFLOW_ENTITIES_QUERY_KEY });
  }, [queryClient, workflowId]);

  const persistDraft = useCallback(
    async (silent: boolean): Promise<boolean> => {
      try {
        JSON.parse(definitionJson);
      } catch {
        if (!silent) message.error('definitionJson 不是合法 JSON');
        return false;
      }
      try {
        const v = await saveWorkflowVersionDraft(workflowId, { definitionJson });
        setDirty(false);
        setSelectedVersionId(v.id);
        setAutoSavedAt(new Date());
        invalidate();
        if (!silent) message.success(`草稿 v${v.version} 已保存`);
        return true;
      } catch {
        if (!silent) message.error('保存草稿失败');
        return false;
      }
    },
    [definitionJson, workflowId, invalidate],
  );

  const saveMutation = useMutation({ mutationFn: () => persistDraft(false) });
  const publishMutation = useMutation({
    mutationFn: (versionId: number) => publishWorkflowVersion(versionId),
    onSuccess: (v) => {
      message.success(`v${v.version} 已发布`);
      setSelectedVersionId(v.id);
      invalidate();
    },
    onError: () => message.error('发布失败'),
  });

  const handlePublish = () => {
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

  const versionMap = useMemo(() => {
    const map: Record<number, number> = {};
    for (const v of versions) map[v.id] = v.version;
    return map;
  }, [versions]);

  return {
    entity,
    versions,
    selectedVersion,
    versionMap,
    definitionJson,
    dirty,
    autoSavedAt,
    status,
    loading: entitiesLoading || versionsLoading,
    setDefinitionJson: handleDefinitionChange,
    handleVersionChange,
    persistDraft,
    saveMutation,
    publishMutation,
    handlePublish,
  };
}
