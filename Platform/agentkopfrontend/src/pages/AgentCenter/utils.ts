/**
 * AgentCenter - 视图模型映射与展示工具
 */
import type { AgentLoop, AgentLoopVersion } from '@/types/agentLoop';
import type { WorkflowEntity } from '@/types/workflowPersistence';
import type {
  OfficialAgentLoopItem,
  OfficialCatalogCardData,
  OfficialWorkflowItem,
} from '@/types/officialCatalog';
import { pickOfficialUiFields } from '@/utils/officialUiFields';
import type { CapabilityCatalog } from '@/pages/AgentPlayground/types';
import type { OfficialToolItem } from '@/types/officialTools';
import { allPipelineRefs, profileFromAgentLoopTemplate, sanitizeProfile, toToolNames } from '@/utils/profile';
import type { AgentLoopDefaultTemplateProfile } from '@/types/agentLoopTemplate';
import { formatUpdatedTime } from '@/utils/format';
import { matchKeyword, normalizeKeyword } from '@/utils/text';
import { parseEnumParam } from '@/utils/url';
import type {
  AgentCardData,
  AgentDetailData,
  HttpToolDef,
  BaseProfile,
  MineAgentLayout,
  MineStatusFilter,
  OfficialCatalogType,
} from './types';
import { BUILTIN_AGENT_TEXT } from './mock';
import { createOfficialLoopQuickSession, createOfficialWorkflowQuickSession } from './api';

/** 内置 Agent 的路由参数标识 */
export const BUILTIN_AGENT_KEY = 'builtin';

/** 官方目录类型白名单：自主规划 / 工作流 */
const CATALOG_TYPES: readonly OfficialCatalogType[] = ['planning', 'workflow'];

/** 我的 Agent 状态筛选白名单 */
const MINE_STATUS_FILTERS: readonly MineStatusFilter[] = ['all', 'published', 'draft'];

/** 我的 Agent 列表排布白名单 */
const MINE_LAYOUTS: readonly MineAgentLayout[] = ['grid', 'list'];

/** URL 的 status 参数 → 我的 Agent 状态筛选，缺省为不限 */
export function parseMineStatus(value: string | null): MineStatusFilter {
  return parseEnumParam(value, MINE_STATUS_FILTERS, 'all');
}

/** URL 的 layout 参数 → 我的 Agent 列表排布，缺省为卡片网格 */
export function parseMineLayout(value: string | null): MineAgentLayout {
  return parseEnumParam(value, MINE_LAYOUTS, 'grid');
}

/** URL 的 page 参数 → 页码，非法值回落到第 1 页 */
export function parseMinePage(value: string | null): number {
  const page = Number(value);
  return Number.isInteger(page) && page >= 1 ? page : 1;
}

/** URL 的 type 参数 → 官方体验 / 目录类型，缺省按自主规划处理 */
export function parseExperienceType(value: string | null): OfficialCatalogType {
  return parseEnumParam(value, CATALOG_TYPES, 'planning');
}

/** 从 baseProfile.httpTools 提取结构化的 HTTP 工具定义 */
export function normalizeHttpTools(httpTools?: unknown[]): HttpToolDef[] {
  if (!Array.isArray(httpTools)) return [];
  return httpTools.flatMap((item) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return [];
    const record = item as Record<string, unknown>;
    if (typeof record.name !== 'string' || !record.name) return [];
    return [record as unknown as HttpToolDef];
  });
}

/** 真实 AgentLoop → 列表卡片 */
export function toAgentCard(agent: AgentLoop): AgentCardData {
  const published = agent.latestVersion > 0;
  return {
    key: String(agent.id),
    kind: 'planning',
    agentId: agent.id,
    name: agent.name,
    description: agent.description || '暂无描述',
    typeLabel: 'Agent-Loop Agent',
    status: published ? 'published' : 'draft',
    versionLabel: published ? `v${agent.latestVersion}` : undefined,
    updatedText: formatUpdatedTime(agent.updatedAt),
  };
}

/** 工作流实体 → 列表卡片 */
export function toWorkflowAgentCard(entity: WorkflowEntity): AgentCardData {
  const published = entity.latestVersion > 0;
  return {
    key: `wf-${entity.id}`,
    kind: 'workflow',
    workflowId: entity.id,
    name: entity.name,
    description: entity.description || '暂无描述',
    typeLabel: '工作流Agent',
    status: published ? 'published' : 'draft',
    versionLabel: published ? `v${entity.latestVersion}` : undefined,
    updatedText: formatUpdatedTime(entity.updatedAt),
  };
}

/** 官方自主规划示范 → 目录卡片 */
export function toOfficialLoopCard(item: OfficialAgentLoopItem): OfficialCatalogCardData {
  const skills = ['Agent-Loop'];
  if (item.version != null) skills.push(`v${item.version}`);
  if (item.profileVersionLabel) skills.push(item.profileVersionLabel);
  return {
    key: `loop-${item.agentLoopId}`,
    name: item.name,
    description: item.description || '暂无描述',
    detail: item.description || '',
    subtitle: '官方示范',
    skills,
    variant: 'planning',
    agentLoopId: item.agentLoopId,
    agentLoopVersionId: item.agentLoopVersionId,
    pipelineName: item.pipelineName?.trim() || undefined,
    // default 当前不含 schema；若偶发下发则透传，正式来源见 getOfficialWorkflowUiSchema
    fields: pickOfficialUiFields(item),
  };
}

/** 官方工作流示范 → 目录卡片 */
export function toOfficialWorkflowCard(item: OfficialWorkflowItem): OfficialCatalogCardData {
  const skills = ['工作流'];
  if (item.version != null) skills.push(`v${item.version}`);
  if (item.workflowKey) skills.push(item.workflowKey);
  const detail = item.description?.trim() || '';
  return {
    key: `wf-${item.workflowId}`,
    name: item.name,
    description: detail || '暂无描述',
    detail,
    subtitle: '官方示范',
    skills,
    variant: 'workflow',
    workflowId: item.workflowId,
    workflowVersionId: item.workflowVersionId,
    workflowKey: item.workflowKey || item.name,
    // TEMPLATE 查询必须用目录里的 pipelineName，不能用 workflowKey
    pipelineName: item.pipelineName?.trim() || undefined,
    fields: pickOfficialUiFields(item),
  };
}

/** Agent Market 详情落地页路径 */
export function buildOfficialMarketPath(item: OfficialCatalogCardData): string {
  return `/agents/official/app/${encodeURIComponent(item.key)}`;
}

/** 官方体验定位 query（chat / console 共用） */
function buildOfficialExperienceParams(item: OfficialCatalogCardData): URLSearchParams {
  const params = new URLSearchParams({ type: item.variant });
  if (item.key) params.set('cardKey', item.key);
  if (item.agentLoopId != null) params.set('agentLoopId', String(item.agentLoopId));
  if (item.agentLoopVersionId != null) {
    params.set('agentLoopVersionId', String(item.agentLoopVersionId));
  }
  if (item.workflowId != null) params.set('workflowId', String(item.workflowId));
  if (item.workflowVersionId != null)
    params.set('workflowVersionId', String(item.workflowVersionId));
  if (item.workflowKey) params.set('workflowKey', item.workflowKey);
  return params;
}

/** 占位 sessionId：先跳转体验页，再在页内调 quick session 创建真实 ses_ */
export const OFFICIAL_PENDING_SESSION_ID = 'new';

/** 是否仍为「立即使用」跳转占位，尚未换成真实 ses_ */
export function isOfficialPendingSessionId(sessionId: string | undefined | null): boolean {
  if (!sessionId) return true;
  const id = sessionId.trim();
  return id === OFFICIAL_PENDING_SESSION_ID || id === 'pending';
}

/** 官方体验对话页路径（自主规划主路径） */
export function buildOfficialChatPath(sessionId: string, item: OfficialCatalogCardData): string {
  return `/agents/official/chat/${encodeURIComponent(sessionId)}?${buildOfficialExperienceParams(item).toString()}`;
}

/** 漫剧动态漫：对话页内加创作画布，不改入口路由 */
export function isOfficialMangaLoop(item: OfficialCatalogCardData): boolean {
  if (item.variant !== 'planning') return false;
  return /漫剧|动态漫/.test(`${item.name} ${item.description}`);
}

/** 官方语音合成 Agent（agentLoopId=27）：发消息需带 tts 参数 */
export const OFFICIAL_TTS_AGENT_LOOP_ID = 27;

export function isOfficialTtsLoop(item: OfficialCatalogCardData): boolean {
  if (item.variant !== 'planning') return false;
  if (item.agentLoopId === OFFICIAL_TTS_AGENT_LOOP_ID) return true;
  return /语音合成|TTS|ASR/.test(`${item.name} ${item.description}`);
}

/** 官方文档解析与研究 Agent（agentLoopId=26）：附件 URL 按环境选 blob / CDN */
export const OFFICIAL_DOC_PARSE_LOOP_ID = 26;

/** 官方虚拟试穿 Agent（agentLoopId=32） */
export const OFFICIAL_VIRTUAL_TRYON_AGENT_LOOP_ID = 32;

/** 文档解析 / 研究类 Agent：依赖 read_file，附件 URL 须 Agent 可达 */
export function isOfficialDocLoop(item: OfficialCatalogCardData): boolean {
  if (item.variant !== 'planning') return false;
  if (item.agentLoopId === OFFICIAL_DOC_PARSE_LOOP_ID) return true;
  return /文档解析|文档研究|表格解析|文件解析|文档分析/.test(`${item.name} ${item.description}`);
}

/** 虚拟试穿 Agent：发消息需带 userInput（商品图 / 模特图） */
export function isOfficialVirtualTryOnLoop(item: OfficialCatalogCardData): boolean {
  if (item.variant !== 'planning') return false;
  if (item.agentLoopId === OFFICIAL_VIRTUAL_TRYON_AGENT_LOOP_ID) return true;
  return /虚拟试穿/.test(`${item.name} ${item.description}`);
}

/** 立即使用：先进入对话页（占位 session），页内再创建会话 */
export function buildOfficialLaunchChatPath(item: OfficialCatalogCardData): string {
  return buildOfficialChatPath(OFFICIAL_PENDING_SESSION_ID, item);
}

/** 官方工作流生成记录控制台路径 */
export function buildOfficialConsolePath(sessionId: string, item: OfficialCatalogCardData): string {
  const params = buildOfficialExperienceParams(item);
  params.set('type', 'workflow');
  return `/agents/official/console/${encodeURIComponent(sessionId)}?${params.toString()}`;
}

/** 立即使用：先进入控制台（占位 session），页内再创建会话 / 填参 */
export function buildOfficialLaunchConsolePath(item: OfficialCatalogCardData): string {
  return buildOfficialConsolePath(OFFICIAL_PENDING_SESSION_ID, item);
}

/** 开启官方 Agent 快速使用会话 */
export async function startOfficialQuickSession(item: OfficialCatalogCardData) {
  if (item.variant === 'workflow') {
    if (!item.workflowKey) throw new Error('缺少 workflowKey，无法开启工作流体验');
    return createOfficialWorkflowQuickSession(item.workflowKey);
  }
  if (item.agentLoopId == null) throw new Error('缺少 agentLoopId，无法开启体验');
  return createOfficialLoopQuickSession(item.agentLoopId);
}

/** 我的 Agent 列表过滤：先按状态，再按名称 / 描述关键字 */
export function filterMineAgentCards(
  cards: AgentCardData[],
  keyword: string,
  status: MineStatusFilter,
): AgentCardData[] {
  const kw = normalizeKeyword(keyword);
  return cards.filter((card) => {
    if (status !== 'all' && card.status !== status) return false;
    return matchKeyword([card.name, card.description], kw);
  });
}

/** 按 URL 参数在官方目录中定位卡片：工作流优先 workflowId，其次 workflowKey */
export function resolveOfficialCard(
  type: OfficialCatalogType,
  agentLoopId: number | undefined,
  workflowId: number | undefined,
  workflowKey: string | undefined,
  loops: OfficialCatalogCardData[],
  workflows: OfficialCatalogCardData[],
): OfficialCatalogCardData | null {
  if (type === 'workflow') {
    return (
      workflows.find(
        (item) =>
          (workflowId != null && item.workflowId === workflowId) ||
          (!!workflowKey && item.workflowKey === workflowKey),
      ) ?? null
    );
  }
  return loops.find((item) => item.agentLoopId === agentLoopId) ?? null;
}

/** 实例能力目录 → 列表卡片（无 Agent 时铺开展示） */
export function toBuiltinCard(catalog: CapabilityCatalog): AgentCardData {
  return {
    key: BUILTIN_AGENT_KEY,
    kind: 'builtin',
    name: BUILTIN_AGENT_TEXT.name,
    description: BUILTIN_AGENT_TEXT.cardDescription,
    typeLabel: '内置Agent',
    status: 'builtin',
    toolCount: catalog.toolNames.length,
  };
}

/**
 * 实例能力目录 → 详情视图模型
 * 后端不再提供缺省 Profile 文档，这里铺开的是目录本身，没有 system / 版本号
 */
export function toBuiltinDetail(catalog: CapabilityCatalog): AgentDetailData {
  return {
    isBuiltin: true,
    name: BUILTIN_AGENT_TEXT.name,
    description: BUILTIN_AGENT_TEXT.detailDescription,
    status: 'builtin',
    system: '',
    defaultModel: '',
    tools: catalog.toolNames,
    pipelines: allPipelineRefs(catalog),
    httpTools: [],
    rawProfile: {},
    catalogTools: catalog.toolNames,
    catalogPipelines: catalog.pipelines,
  };
}

/** 新建 AgentLoop（尚无版本）→ 用默认模版 profile 初始化详情 */
export function toFreshAgentDetail(
  agent: AgentLoop,
  templateProfile: AgentLoopDefaultTemplateProfile,
  catalog: CapabilityCatalog | null,
  officialTools?: OfficialToolItem[] | null,
): AgentDetailData {
  const versionProfile = profileFromAgentLoopTemplate(
    templateProfile,
    catalog,
    officialTools ?? [],
  );
  const tools = toToolNames(versionProfile.tools);
  return {
    isBuiltin: false,
    agentId: agent.id,
    name: agent.name,
    description: agent.description || '暂无描述',
    status: 'draft',
    system: versionProfile.system ?? '',
    defaultModel: versionProfile.defaultModel ?? '',
    tools,
    pipelines: versionProfile.pipelines ?? [],
    httpTools: normalizeHttpTools(versionProfile.httpTools),
    rawProfile: versionProfile,
    catalogTools: Array.from(new Set([...(catalog?.toolNames ?? []), ...tools])),
    catalogPipelines: catalog?.pipelines ?? [],
  };
}

/** 真实 AgentLoop + 版本列表 → 详情视图模型（默认取最高版本，可指定当前查看版本） */
export function toAgentDetail(
  agent: AgentLoop,
  versions: AgentLoopVersion[],
  catalog: CapabilityCatalog | null,
  /** 当前查看的版本；不传则取 versions[0]（接口按 version 降序，即最高版本） */
  selectedVersion?: AgentLoopVersion | null,
  /** official/tools 含 ref，清洗历史 profile 时补齐 */
  officialTools?: OfficialToolItem[] | null,
): AgentDetailData {
  const active = selectedVersion ?? versions[0];
  // 历史版本可能是旧 schema（tools 为字符串数组、带 toolModels），统一清洗成新 schema，
  // 避免保存草稿时把后端已不接受的字段原样写回
  const versionProfile: BaseProfile =
    active?.profileYaml && typeof active.profileYaml === 'object'
      ? sanitizeProfile(active.profileYaml, catalog, officialTools ?? [])
      : {};
  const published = agent.latestVersion > 0;
  const tools = toToolNames(versionProfile.tools);
  let status: AgentCardData['status'] = 'draft';
  if (active?.status !== 'draft' && published) status = 'published';
  return {
    isBuiltin: false,
    agentId: agent.id,
    versionId: active?.id,
    name: agent.name,
    description: agent.description || '暂无描述',
    status,
    versionLabel: active ? `v${active.version}` : undefined,
    updatedText: formatUpdatedTime(active?.createdAt ?? agent.updatedAt),
    system: versionProfile.system ?? '',
    defaultModel: versionProfile.defaultModel ?? '',
    tools,
    pipelines: versionProfile.pipelines ?? [],
    httpTools: normalizeHttpTools(versionProfile.httpTools),
    rawProfile: versionProfile,
    // 目录工具与已启用工具取并集，保证自定义 HTTP 工具也能展示
    catalogTools: Array.from(new Set([...(catalog?.toolNames ?? []), ...tools])),
    catalogPipelines: catalog?.pipelines ?? [],
  };
}
