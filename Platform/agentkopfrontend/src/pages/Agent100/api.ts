/**
 * Agent100 接口
 */
import { get, post } from '@/utils/request';
import type {
  Agent100Config,
  Agent100Execution,
  Agent100HistoryData,
  Agent100ListData,
  Agent100StartRequest,
} from './types';

const BASE = '/agent/operation/backend/platform/agent100';
const silent = { headers: { 'X-No-Loading': '1' } };

export const AGENT100_LIST_QUERY_KEY = ['agent100', 'list'] as const;

export const AGENT100_CONFIG_QUERY_KEY = ['agent100', 'config'] as const;

export const AGENT100_HISTORY_QUERY_KEY = ['agent100', 'executions'] as const;

/** GET /platform/agent100 */
export function getAgent100List() {
  return get<Agent100ListData>(BASE);
}

/** GET /platform/agent100/{agentId}/config */
export function getAgent100Config(agentId: string) {
  return get<Agent100Config>(`${BASE}/${encodeURIComponent(agentId)}/config`);
}

/** POST /platform/agent100/{agentId}/executions */
export function startAgent100Execution(agentId: string, body: Agent100StartRequest) {
  return post<Agent100Execution>(`${BASE}/${encodeURIComponent(agentId)}/executions`, body, {
    timeout: 120_000,
  });
}

/** GET /platform/agent100/{agentId}/executions/{sessionId}/{executionId} */
export function getAgent100Execution(agentId: string, sessionId: string, executionId: string) {
  return get<Agent100Execution>(
    `${BASE}/${encodeURIComponent(agentId)}/executions/${encodeURIComponent(sessionId)}/${encodeURIComponent(executionId)}`,
    undefined,
    {
      timeout: 60_000,
      headers: { 'X-No-Loading': '1', 'X-Skip-Error-Toast': '1' },
    },
  );
}

/** GET /platform/agent100/{agentId}/executions */
export function listAgent100Executions(agentId: string) {
  return get<Agent100HistoryData>(`${BASE}/${encodeURIComponent(agentId)}/executions`, undefined, silent);
}

/** POST /agentk/sessions/{sessionId}/cancel */
export function cancelAgent100Session(sessionId: string) {
  return post<void>(`/agent/operation/backend/agentk/sessions/${encodeURIComponent(sessionId)}/cancel`, {
    reason: 'user_cancel',
  });
}
