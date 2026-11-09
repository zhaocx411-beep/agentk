/**
 * 官方体验页历史会话查询（支持分页）
 */
import { getSessions } from '@/pages/AgentPlayground/api';
import type { SessionListResult } from '@/pages/AgentPlayground/types';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { SESSIONS_PAGE_SIZE } from '../AgentDetail/hooks/useSessionListPagination';

export async function fetchOfficialSessionsPage(
  item: OfficialCatalogCardData,
  page: number,
  pageSize = SESSIONS_PAGE_SIZE,
): Promise<SessionListResult> {
  // 工作流只查 pipeline 会话；自主规划只查 agent_loop，互不混查
  if (item.variant === 'workflow' && item.workflowId != null) {
    return getSessions({
      page,
      pageSize,
      sessionKind: 'pipeline',
      workflowId: item.workflowId,
    });
  }

  return getSessions({
    page,
    pageSize,
    sessionKind: 'agent_loop',
    ...(item.agentLoopId != null ? { agentLoopId: item.agentLoopId } : {}),
  });
}
