/**
 * Pipeline 共享 API 层
 * 从 AgentPlayground/api.ts 抽取，供 PipelineEditor 和 AgentPlayground 共用
 */
import { get, post } from '@/utils/request';
import { assertOutboundMediaWithinLimit } from './mediaSizeGuard';
import { queryClient } from './queryClient';
import type {
  WorkflowsResponse,
  RunWorkflowRequest,
  RunWorkflowResponse,
  WorkflowRunsResponse,
  GetWorkflowRunsParams,
  WorkflowRunDetail,
  CreateSessionRequest,
  Session,
  CapabilitiesResponse,
  CapabilityCatalog,
  CatalogPipeline,
  CatalogTool,
  ModelCatalog,
  AgentkToolCatalog,
  AgentkToolItem,
} from '@/pages/AgentPlayground/types';

/** capabilities 原始响应 → 页面消费的能力目录 */
function toCatalog(response: CapabilitiesResponse | null | undefined): CapabilityCatalog {
  const capabilities = response?.capabilities ?? [];
  const tools: CatalogTool[] = [];
  const pipelines: CatalogPipeline[] = [];

  for (const capability of capabilities) {
    if (capability.tool?.name) {
      tools.push({
        name: capability.tool.name,
        ref: capability.toolRef,
        description: capability.tool.description || capability.description,
        mode: capability.tool.mode,
      });
    }
    // 没有 pipelineRef 说明这条能力没注册成 Pipeline，写进 Profile 会被拒
    if (capability.pipeline?.name && capability.pipelineRef) {
      pipelines.push({
        name: capability.pipeline.name,
        ref: capability.pipelineRef,
        description: capability.pipeline.description || capability.description,
        timeoutSeconds: capability.timeoutSeconds,
        inputSchema: capability.inputSchema,
        outputSchema: capability.outputSchema,
      });
    }
  }

  return { capabilities, tools, toolNames: tools.map((tool) => tool.name), pipelines };
}

/** 能力目录的共享缓存键；所有消费方必须复用它，否则会各拉一次 */
export const CAPABILITIES_QUERY_KEY = ['agentk-capabilities'] as const;

/**
 * 能力目录底层请求。给 useQuery 的 queryFn 用这个，不要用 getCapabilities：
 * getCapabilities 内部 ensureQueryData 与同 key 的 useQuery 会互相等待，请求发不出去。
 */
export async function fetchCapabilities(): Promise<CapabilityCatalog> {
  const response = await get<CapabilitiesResponse>('/agent/operation/backend/agentk/capabilities');
  return toCatalog(response);
}

/**
 * 获取实例能力目录（tools / pipelines）
 * 模型清单不在这里，见 getModels()
 *
 * 命令式调用走缓存去重；useQuery 请用 fetchCapabilities 作 queryFn。
 */
export function getCapabilities(): Promise<CapabilityCatalog> {
  return queryClient.ensureQueryData({
    queryKey: CAPABILITIES_QUERY_KEY,
    queryFn: fetchCapabilities,
  });
}

/** 模型目录的共享缓存键；所有选模型入口必须复用它 */
export const MODELS_QUERY_KEY = ['agentk-models'] as const;

/**
 * 模型目录底层请求。给 useQuery 的 queryFn 用这个，不要用 getModels：
 * getModels 内部 ensureQueryData 与同 key 的 useQuery 会互相等待，Network 里看不到 /models。
 */
export async function fetchModels(): Promise<ModelCatalog> {
  const response = await get<ModelCatalog>('/agent/operation/backend/agentk/models');
  return {
    kconfKey: response?.kconfKey ?? 'klingai.agentk.models',
    version: response?.version ?? null,
    names: response?.names ?? [],
    chatModels: response?.chatModels ?? [],
    toolModels: response?.toolModels ?? {},
    models: response?.models ?? [],
  };
}

/**
 * 获取模型目录（命令式，带缓存）
 * 全平台唯一模型列表：主对话模型、工具子模型、自定义 Agent Loop / Pipeline 选模型都走这里
 */
export function getModels(): Promise<ModelCatalog> {
  return queryClient.ensureQueryData({
    queryKey: MODELS_QUERY_KEY,
    queryFn: fetchModels,
  });
}

/** 工具名单明细的共享缓存键 */
export const TOOLS_QUERY_KEY = ['agentk-tools'] as const;

/**
 * 工具名单明细底层请求（GET /agentk/tools）
 * 给 useQuery 的 queryFn 用这个，不要用 getTools（同 models 的 ensureQueryData 陷阱）
 */
export async function fetchTools(): Promise<AgentkToolCatalog> {
  const response = await get<AgentkToolCatalog>('/agent/operation/backend/agentk/tools');
  const tools: AgentkToolItem[] = Array.isArray(response?.tools) ? response.tools : [];
  return {
    source: response?.source,
    tools: tools.filter((item) => !!item?.name),
  };
}

/** 获取工具名单明细（命令式，带缓存） */
export function getTools(): Promise<AgentkToolCatalog> {
  return queryClient.ensureQueryData({
    queryKey: TOOLS_QUERY_KEY,
    queryFn: fetchTools,
  });
}

/** 创建新会话 */
export function createSession(body: CreateSessionRequest) {
  return post<Session>('/agent/operation/backend/agentk/sessions', body);
}

/** 获取当前 Session 可用的 Pipeline 模板列表 */
export function getWorkflows(sessionId: string) {
  return get<WorkflowsResponse>(`/agent/operation/backend/agentk/sessions/${sessionId}/pipelines`);
}

/** 启动一次 Pipeline 执行 */
export function runWorkflow(sessionId: string, body: RunWorkflowRequest) {
  return post<RunWorkflowResponse>(
    `/agent/operation/backend/agentk/sessions/${sessionId}/pipeline-runs`,
    body,
  );
}

/**
 * 官方工作流体验页专用的入参体：该场景后端读的是 args，不是通用接口的 inputs。
 * 具体每个 Pipeline 的 args 结构见 OfficialExperience/studio/officialPipelines.ts。
 */
export interface RunOfficialPipelineRequest {
  /** Pipeline 名称，取 URL 上的 workflowKey */
  workflow: string;
  /** official/defaults 的 pipelineName（如 generate_brand_ad），须是 session profile 暴露的名字 */
  pipeline?: string;
  args: Record<string, unknown>;
}

/** 启动一次官方工作流体验的 Pipeline 执行 */
export async function runOfficialPipeline(sessionId: string, body: RunOfficialPipelineRequest) {
  await assertOutboundMediaWithinLimit({ payload: body.args });
  return post<RunWorkflowResponse>(
    `/agent/operation/backend/agentk/sessions/${sessionId}/pipeline-runs`,
    body,
  );
}

/** 获取 Session 下的 Pipeline 执行列表 */
export function getWorkflowRuns(sessionId: string, params?: GetWorkflowRunsParams) {
  return get<WorkflowRunsResponse>(
    `/agent/operation/backend/agentk/sessions/${sessionId}/pipeline-runs`,
    { params },
  );
}

/** 查询当前 Session 下的单次 Pipeline 执行详情（轮询用） */
export function getWorkflowRunDetail(sessionId: string, execId: string) {
  return get<WorkflowRunDetail>(
    `/agent/operation/backend/agentk/sessions/${sessionId}/pipeline-runs/${execId}`,
  );
}
