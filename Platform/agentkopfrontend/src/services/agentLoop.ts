/**
 * AgentLoop 持久化共享 API 层
 * 对接 /agent/operation/backend/platform/* 接口
 */
import { get, post } from '@/utils/request';
import { bareWorkflowKey, workflowKeyMatches } from '@/utils/workflowKey';
import type { PlatformPageResult } from '@/types/api';
import type {
  AgentLoop,
  AgentLoopVersion,
  CreateAgentLoopRequest,
  SaveVersionDraftRequest,
} from '@/types/agentLoop';
import type {
  OfficialDefaultsData,
  OfficialQuickSession,
  OfficialWorkflowUiSchemaData,
  PlatformCapabilitiesData,
  PlatformCapabilityItem,
} from '@/types/officialCatalog';
import type { AgentLoopDefaultTemplateData } from '@/types/agentLoopTemplate';
import type { OfficialToolItem, OfficialToolsData } from '@/types/officialTools';

const BASE = '/agent/operation/backend/platform';

/** 拉取 agent-loops 时的默认 pageSize，与我的 Agent 列表翻页一致 */
export const AGENT_LOOPS_PAGE_SIZE = 20;

/** React Query key：我的 AgentLoop 列表 */
export const AGENT_LOOPS_QUERY_KEY = ['agent-loops'] as const;
/** React Query key：某 AgentLoop 的版本列表 */
export const agentLoopVersionsKey = (agentLoopId: number) =>
  ['agent-loop-versions', agentLoopId] as const;

/**
 * 兼容列表接口：裸数组，或分页包一层 { records | list | items }
 * （agent-loops 分页请求用 pageNum，响应可能是 page 或 pageNum）
 */
function unwrapList<T>(data: unknown): T[] {
  if (Array.isArray(data)) return data as T[];
  if (data && typeof data === 'object') {
    const obj = data as Record<string, unknown>;
    for (const key of ['records', 'list', 'items', 'agentLoops']) {
      if (Array.isArray(obj[key])) return obj[key] as T[];
    }
  }
  return [];
}

function isPlatformPageResult<T>(data: unknown): data is PlatformPageResult<T> {
  return (
    !!data &&
    typeof data === 'object' &&
    Array.isArray((data as PlatformPageResult<T>).records) &&
    typeof (data as PlatformPageResult<T>).total === 'number'
  );
}

/** 创建 AgentLoop（逻辑实体，latestVersion=0，status=active） */
export function createAgentLoop(body: CreateAgentLoopRequest) {
  return post<AgentLoop>(`${BASE}/agent-loops`, body);
}

function readPositiveInt(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : fallback;
}

/**
 * 我的 AgentLoop 单页
 * GET .../agent-loops?pageNum=&pageSize= → { page | pageNum, pageSize, total, records }
 */
export async function getAgentLoopsPage(
  pageNum = 1,
  pageSize = AGENT_LOOPS_PAGE_SIZE,
): Promise<PlatformPageResult<AgentLoop>> {
  const data = await get<PlatformPageResult<AgentLoop> | AgentLoop[]>(`${BASE}/agent-loops`, {
    pageNum,
    pageSize,
  });

  if (Array.isArray(data)) {
    return { page: 1, pageSize: data.length, total: data.length, records: data };
  }
  if (!isPlatformPageResult<AgentLoop>(data)) {
    const records = unwrapList<AgentLoop>(data);
    return { page: 1, pageSize: records.length, total: records.length, records };
  }

  const raw = data as PlatformPageResult<AgentLoop> & { pageNum?: number };
  return {
    page: readPositiveInt(raw.pageNum, readPositiveInt(raw.page, pageNum)),
    pageSize: readPositiveInt(raw.pageSize, pageSize),
    total: raw.total,
    records: raw.records,
  };
}

/**
 * 我的 AgentLoop 全量列表（详情 / 选择器等需要按 id 查找时使用）
 * 对外仍返回完整数组，页数不足时自动翻页拉齐
 */
export async function getAgentLoops(): Promise<AgentLoop[]> {
  const first = await getAgentLoopsPage(1, AGENT_LOOPS_PAGE_SIZE);
  const all = [...first.records];
  let page = first.page > 0 ? first.page : 1;

  while (all.length < first.total) {
    page += 1;
    const next = await getAgentLoopsPage(page, first.pageSize > 0 ? first.pageSize : AGENT_LOOPS_PAGE_SIZE);
    if (next.records.length === 0) break;
    all.push(...next.records);
  }

  return all;
}

/** React Query key：AgentLoop 新建默认 Profile 模版 */
export const AGENT_LOOP_DEFAULT_TEMPLATE_QUERY_KEY = ['agent-loop-default-template'] as const;

/** 官方 AgentLoop 体验默认 Profile（tools 来自 capability 缓存；pipelines 为空） */
export function getAgentLoopDefaultTemplate() {
  return get<AgentLoopDefaultTemplateData>(
    `${BASE}/session-profiles/templates/agent-loop-default`,
  );
}

/** React Query key：官方示范目录 */
export const OFFICIAL_DEFAULTS_QUERY_KEY = ['official-defaults'] as const;

/** 官方示范目录：自主规划 Agent + 工作流 Agent（仅目录身份，不含入参 schema） */
export function getOfficialDefaults() {
  return get<OfficialDefaultsData>(`${BASE}/official/default`);
}

/** React Query key：官方工作流入参 schema（fields / uiSchema） */
export const officialWorkflowSchemaKey = (workflowKey: string) =>
  ['official-workflow-schema', workflowKey] as const;

/** 从 capabilities 列表中匹配 workflowKey 对应的 pipeline */
function matchCapabilityByWorkflowKey(
  list: PlatformCapabilityItem[],
  workflowKey: string,
): PlatformCapabilityItem | null {
  const key = workflowKey.trim();
  if (!key || list.length === 0) return null;

  const matched = list.find((item) => {
    const candidates = [item.name, item.title, item.id, item.pipelineRef, item.toolRef];
    return candidates.some((candidate) => !!candidate && workflowKeyMatches(candidate, key));
  });
  if (matched) return matched;

  // 服务端按 workflowKey 过滤时通常只返回一条
  if (list.length === 1) return list[0];

  // 再按短名兜底（多条时优先精确短名）
  const bare = bareWorkflowKey(key);
  return list.find((item) => bareWorkflowKey(item.name || item.id || '') === bare) ?? null;
}

/**
 * 官方工作流入参 schema（供填参弹窗 / CreationStudio 动态表单）
 *
 * GET /agent/operation/backend/platform/agentk/capabilities?workflowKey=
 * 从 capabilities[].fields / uiSchema.fields 取出配置字段。
 */
export async function getOfficialWorkflowUiSchema(
  workflowKey: string,
): Promise<OfficialWorkflowUiSchemaData | null> {
  const key = workflowKey?.trim();
  if (!key) return null;

  const data = await get<PlatformCapabilitiesData>(`${BASE}/agentk/capabilities`, {
    workflowKey: key,
  });
  const capability = matchCapabilityByWorkflowKey(data?.capabilities ?? [], key);
  if (!capability) return null;

  return {
    workflowKey: key,
    name: capability.name,
    title: capability.title,
    description: capability.description,
    fields: capability.fields,
    uiSchema: capability.uiSchema,
    inputSchema: capability.inputSchema,
    outputSchema: capability.outputSchema,
  };
}

/** React Query key：官方工具广场 */
export const OFFICIAL_TOOLS_QUERY_KEY = ['official-tools'] as const;

/** 官方工具广场列表 */
export async function getOfficialTools(): Promise<OfficialToolsData> {
  const res = await get<OfficialToolsData | OfficialToolItem[]>(`${BASE}/official/tools`);
  if (Array.isArray(res)) return { tools: res };
  return { ...res, tools: res?.tools ?? [] };
}

/** 官方自主规划 Agent 快速开对话；title 可选，默认不传 body 字段 */
export function createOfficialLoopQuickSession(agentLoopId: number, title?: string) {
  return post<OfficialQuickSession>(
    `${BASE}/official/sessions/quick/agent-loops/${agentLoopId}`,
    title ? { title } : {},
  );
}

/** 官方工作流 Agent 快速开对话；title 可选，默认不传 body 字段 */
export function createOfficialWorkflowQuickSession(workflowKey: string, title?: string) {
  return post<OfficialQuickSession>(
    `${BASE}/official/sessions/quick/workflows/${workflowKey}`,
    title ? { title } : {},
  );
}

/** AgentLoop 的 profile 版本列表（按 version 降序，含草稿与已发布） */
export function getAgentLoopVersions(agentLoopId: number) {
  return get<AgentLoopVersion[]>(`${BASE}/agent-loops/${agentLoopId}/versions`);
}

/** 保存版本草稿（version 自增，status=draft，不更新 latestVersion） */
export function saveVersionDraft(agentLoopId: number, body: SaveVersionDraftRequest) {
  return post<AgentLoopVersion>(`${BASE}/agent-loops/${agentLoopId}/versions`, body);
}

/** 发布版本（draft → published；若版本号更大则回写 latestVersion） */
export function publishAgentLoopVersion(agentLoopVersionId: number) {
  return post<AgentLoopVersion>(`${BASE}/agent-loop-versions/${agentLoopVersionId}/publish`);
}
