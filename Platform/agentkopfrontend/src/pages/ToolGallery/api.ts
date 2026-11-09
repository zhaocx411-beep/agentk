/**
 * 工具广场接口
 */
import { get, post } from '@/utils/request';
import type {
  CapabilityExecution,
  CapabilityRunHistoryData,
  CapabilityRunHistoryQuery,
  CapabilityRunResult,
  CapabilityRunStatus,
  StartCapabilityRunRequest,
} from '@/types/capabilityRuns';

export { getOfficialTools, OFFICIAL_TOOLS_QUERY_KEY } from '@/services/agentLoop';

const INVOKE_BASE = '/agent/operation/backend/capabilities/invoke';
const RUN_BASE = '/agent/operation/backend/capabilities/runs';

export const CAPABILITY_RUN_HISTORY_KEY = 'capability-run-history';

interface CapabilityInvokeResponse {
  invocationId: string;
  platformSessionId?: number;
  capabilityId: string;
  status: CapabilityRunStatus;
  args?: Record<string, unknown>;
  output?: Record<string, unknown> | null;
  mediaArtifacts?: CapabilityExecution['mediaArtifacts'];
  imageUrls?: string[];
  videoUrls?: string[];
  execution?: Partial<CapabilityExecution>;
}

function normalizeInvokeResponse(raw: CapabilityInvokeResponse): CapabilityRunResult {
  const invocationId = raw.invocationId;
  const sessionId = raw.execution?.sessionId || invocationId;
  return {
    sessionId,
    executionId: invocationId,
    status: raw.status,
    execution: {
      id: raw.execution?.id || invocationId,
      sessionId,
      name: raw.execution?.name || raw.capabilityId,
      status: raw.execution?.status || raw.status,
      output: raw.execution?.output ?? raw.output,
      mediaArtifacts: raw.execution?.mediaArtifacts ?? raw.mediaArtifacts,
      imageUrls: raw.execution?.imageUrls ?? raw.imageUrls,
      videoUrls: raw.execution?.videoUrls ?? raw.videoUrls,
      profileVersion: raw.execution?.profileVersion,
      error: raw.execution?.error,
      createdAt: raw.execution?.createdAt,
      finishedAt: raw.execution?.finishedAt,
      args: raw.execution?.args ?? raw.args,
    },
  };
}

/** 启动一次 capability 执行 */
export async function startCapabilityRun(body: StartCapabilityRunRequest) {
  const raw = await post<CapabilityInvokeResponse>(INVOKE_BASE, body, { timeout: 120_000 });
  return normalizeInvokeResponse(raw);
}

const SILENT_POLL = {
  timeout: 60_000,
  headers: { 'X-No-Loading': '1', 'X-Skip-Error-Toast': '1' },
};

/** 轮询新接口创建的瞬态调用，不触发 AgentK */
export async function getCapabilityInvocation(invocationId: string) {
  const raw = await get<CapabilityInvokeResponse>(
    INVOKE_BASE,
    { invocationId },
    SILENT_POLL,
  );
  return normalizeInvokeResponse(raw);
}

/** 回溯已入库的执行状态和输出 */
export function getCapabilityRun(executionId: string, sessionId = executionId) {
  return get<CapabilityRunResult>(
    RUN_BASE,
    { executionId, sessionId },
    SILENT_POLL,
  );
}

/** 取消该 session 上正在运行的 capability 执行 */
export function cancelCapabilityRun(sessionId: string, silent = false) {
  return post<void>(
    `${RUN_BASE}/cancel`,
    { sessionId },
    silent ? SILENT_POLL : undefined,
  );
}

/** Lab 历史：谁、哪个工具、入参、状态 */
export function getCapabilityRunHistory(query: CapabilityRunHistoryQuery = {}) {
  return get<CapabilityRunHistoryData>(`${RUN_BASE}/history`, query);
}
