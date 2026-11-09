/**
 * 基于官方模版创建「我的 Agent」
 * - 自主规划：先本地带配置进详情，保存草稿时再 create agent-loop
 * - 工作流：仍立即复制实体（本需求仅约束 agent-loop）
 */
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { message } from '@/utils/feedback';
import { useQueryClient } from '@tanstack/react-query';
import type { AgentLoopVersion } from '@/types/agentLoop';
import type { WorkflowEntity, WorkflowVersion } from '@/types/workflowPersistence';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import type { BaseProfile } from '@/pages/AgentPlayground/types';
import { getOfficialTools } from '@/services/agentLoop';
import { sanitizeProfile } from '@/utils/profile';
import {
  WORKFLOW_ENTITIES_QUERY_KEY,
  createWorkflowEntity,
  getAgentLoopVersions,
  getWorkflowVersions,
  saveWorkflowVersionDraft,
  workflowVersionsKey,
} from '../api';
import {
  buildFromOfficialPath,
  writePendingOfficialAgent,
} from '../AgentDetail/lib/pendingOfficialAgent';

function toDefinitionJson(definitionJson: string | Record<string, unknown> | undefined): string {
  if (!definitionJson) return JSON.stringify({ workflows: {} }, null, 2);
  if (typeof definitionJson === 'string') return definitionJson;
  return JSON.stringify(definitionJson, null, 2);
}

function prependListItem<T extends { id: number }>(prev: T[] | undefined, item: T): T[] {
  const list = Array.isArray(prev) ? prev : [];
  if (list.some((row) => row.id === item.id)) return list;
  return [item, ...list];
}

export function useOfficialTemplate(defaultItem: OfficialCatalogCardData | null = null) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [usingKey, setUsingKey] = useState<string | null>(null);

  const applyTemplate = async (override?: OfficialCatalogCardData | null) => {
    const item = override ?? defaultItem;
    if (!item || usingKey) return;
    setUsingKey(item.key);
    try {
      if (item.variant === 'workflow') {
        if (item.workflowId == null) throw new Error('缺少工作流模版信息');

        let versions: WorkflowVersion[];
        try {
          versions = await getWorkflowVersions(item.workflowId);
        } catch {
          throw new Error('无法读取官方工作流配置，请稍后重试');
        }
        const sourceVersion =
          versions.find((row) => row.id === item.workflowVersionId) ?? versions[0];
        if (!sourceVersion?.definitionJson) {
          throw new Error('官方工作流暂无可用配置，无法创建');
        }

        const entity = await createWorkflowEntity({
          name: item.name,
          description: item.description,
        });
        let draft: WorkflowVersion;
        try {
          draft = await saveWorkflowVersionDraft(entity.id, {
            definitionJson: toDefinitionJson(sourceVersion.definitionJson),
          });
        } catch {
          queryClient.setQueryData<WorkflowEntity[]>(WORKFLOW_ENTITIES_QUERY_KEY, (prev) =>
            prependListItem(prev, entity),
          );
          await queryClient.invalidateQueries({ queryKey: WORKFLOW_ENTITIES_QUERY_KEY });
          message.warning(`已创建「${entity.name}」，但配置复制失败，请在详情页检查后手动保存`);
          navigate(`/agents/wf/${entity.id}`);
          return;
        }

        queryClient.setQueryData<WorkflowEntity[]>(WORKFLOW_ENTITIES_QUERY_KEY, (prev) =>
          prependListItem(prev, entity),
        );
        queryClient.setQueryData(workflowVersionsKey(entity.id), [draft]);
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: WORKFLOW_ENTITIES_QUERY_KEY }),
          queryClient.invalidateQueries({ queryKey: workflowVersionsKey(entity.id) }),
        ]);
        message.success(`已按模版创建工作流 Agent「${entity.name}」`);
        navigate(`/agents/wf/${entity.id}`);
        return;
      }

      if (item.agentLoopId == null) throw new Error('缺少 Agent-Loop 模版信息');

      let versions: AgentLoopVersion[];
      try {
        versions = await getAgentLoopVersions(item.agentLoopId);
      } catch {
        throw new Error('无法读取官方 Agent 配置，请稍后重试');
      }
      const sourceVersion =
        versions.find((row) => row.id === item.agentLoopVersionId) ?? versions[0];
      const profileYaml = sourceVersion?.profileYaml;
      if (!profileYaml || typeof profileYaml !== 'object') {
        throw new Error('官方 Agent 暂无可用配置，无法创建');
      }

      // 用 official/tools 的 ref 补齐历史模版里缺 ref 的 tools
      let enrichedProfile: BaseProfile = profileYaml as BaseProfile;
      try {
        const official = await getOfficialTools();
        enrichedProfile = sanitizeProfile(profileYaml, null, official.tools);
      } catch {
        // 目录拉取失败时仍用原配置进入详情，保存时会再尝试补齐
      }

      // 不立即 POST /agent-loops：配置进本地暂存，详情页保存草稿时再创建
      writePendingOfficialAgent({
        officialAgentLoopId: item.agentLoopId,
        officialAgentLoopVersionId: item.agentLoopVersionId,
        suggestedName: item.name,
        description: item.description || '',
        profileYaml: enrichedProfile,
      });
      message.info('已载入官方配置，保存草稿后才会创建我的 Agent');
      navigate(buildFromOfficialPath());
    } catch (error) {
      message.error(error instanceof Error ? error.message : '基于此创建失败，请稍后重试');
    } finally {
      setUsingKey(null);
    }
  };

  return {
    usingTemplate: usingKey != null,
    usingKey,
    applyTemplate,
  };
}
