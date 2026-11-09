/**
 * 详情页预览区：官方模版来源且能解析 pipelineName 时，接入 agent-loop 灵感模版
 * 普通「我的 Agent」无官方 pipeline 绑定，不走 datasets TEMPLATE 接口，也不展示快捷提示
 */
import { useMemo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { OFFICIAL_DEFAULTS_QUERY_KEY, getOfficialDefaults } from '../../api';
import { toOfficialLoopCard } from '../../utils';
import OfficialLoopPromptStrip from '../../OfficialExperience/OfficialFreeChat/templates/OfficialLoopPromptStrip';
import OfficialLoopTemplateLauncher from '../../OfficialExperience/OfficialFreeChat/templates/OfficialLoopTemplateLauncher';
import { getOfficialFreeChatGuide } from '../../OfficialExperience/OfficialFreeChat/templates/promptTemplates';
import { useOfficialLoopPromptTemplates } from '../../OfficialExperience/OfficialFreeChat/templates/useOfficialLoopPromptTemplates';
import { resolveOfficialTemplatePipelineName } from '../../OfficialExperience/studio/lib/templateTasks';

export interface AgentDetailPromptShellModel {
  composerPromptStrip?: ReactNode;
  composerToolbarExtra?: ReactNode;
  emptyTitle?: string;
  emptyDescription?: string;
  showEmptyIcon: boolean;
}

export function useAgentDetailPromptShell(
  isFromOfficial: boolean,
  officialAgentLoopId: number | undefined,
  onTemplatePick: (item: ChatSuggestionItem) => void,
  disabled: boolean,
): AgentDetailPromptShellModel {
  const { data: defaults } = useQuery({
    queryKey: OFFICIAL_DEFAULTS_QUERY_KEY,
    queryFn: getOfficialDefaults,
    enabled: isFromOfficial && officialAgentLoopId != null,
    staleTime: 60_000,
  });

  const catalogItem = useMemo<OfficialCatalogCardData | null>(() => {
    if (!isFromOfficial || officialAgentLoopId == null || !defaults?.agentLoops?.length) {
      return null;
    }
    const loop = defaults.agentLoops.find((row) => row.agentLoopId === officialAgentLoopId);
    if (!loop) return null;
    return toOfficialLoopCard(loop);
  }, [defaults?.agentLoops, isFromOfficial, officialAgentLoopId]);

  const pipelineName = resolveOfficialTemplatePipelineName(catalogItem);
  const templates = useOfficialLoopPromptTemplates(catalogItem);
  const guide = useMemo(
    () => (catalogItem ? getOfficialFreeChatGuide(catalogItem) : null),
    [catalogItem],
  );

  const showTemplateUi =
    !!pipelineName && (templates.loading || templates.fromApi);

  const composerPromptStrip = showTemplateUi ? (
    <OfficialLoopPromptStrip
      items={templates.strip}
      loading={templates.loading}
      refreshing={templates.refreshingStrip}
      skeletonCount={6}
      onPick={onTemplatePick}
      onRefresh={templates.fromApi ? templates.refreshStrip : undefined}
    />
  ) : undefined;

  const composerToolbarExtra =
    showTemplateUi && templates.fromApi ? (
      <OfficialLoopTemplateLauncher
        catalog={templates}
        onPick={onTemplatePick}
        disabled={disabled}
      />
    ) : undefined;

  return {
    composerPromptStrip,
    composerToolbarExtra,
    emptyTitle: showTemplateUi ? guide?.emptyTitle : undefined,
    emptyDescription: showTemplateUi ? guide?.emptyDescription : undefined,
    showEmptyIcon: !showTemplateUi,
  };
}
