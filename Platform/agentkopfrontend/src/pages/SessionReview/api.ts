/**
 * 数据回看 - API 接口
 */
import { get, post } from '@/utils/request';
import type {
  PipelineExecRecord,
  SessionCaseData,
  SessionCaseQueryParams,
  SessionReviewQueryParams,
  SessionReviewResponse,
} from './types';

/** 查询管理员视角的全量 Session 列表 */
export function queryAdminSessions(
  params: SessionReviewQueryParams,
): Promise<SessionReviewResponse> {
  return post<SessionReviewResponse>(
    '/agent/operation/backend/platform/admin/chat/sessions/query',
    params,
  );
}

/**
 * 单会话案例聚合：绑定信息 + runs + messages + callLogs
 * GET /platform/admin/chat/sessions/by-agentk/{agentkSessionId}/case
 */
export function getAdminSessionCase(
  agentkSessionId: string,
  params?: SessionCaseQueryParams,
): Promise<SessionCaseData> {
  return get<SessionCaseData>(
    `/agent/operation/backend/platform/admin/chat/sessions/by-agentk/${encodeURIComponent(agentkSessionId)}/case`,
    params,
  );
}

/**
 * Pipeline 回溯账本（workflow 会话对话回看）
 * GET /platform/chat/pipeline-execs/by-agentk/{agentkSessionId}
 */
export function getPipelineExecsByAgentkSession(
  agentkSessionId: string,
): Promise<PipelineExecRecord[]> {
  return get<PipelineExecRecord[]>(
    `/agent/operation/backend/platform/chat/pipeline-execs/by-agentk/${encodeURIComponent(agentkSessionId)}`,
  );
}
