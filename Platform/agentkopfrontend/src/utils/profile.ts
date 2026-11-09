/**
 * 会话 Profile 文档组装工具
 *
 * agent-k 的 Profile schema 是 additionalProperties: false —— 出现未知字段不是被忽略，
 * 而是整份文档校验失败返回 400。所以所有下发前的组装、以及历史数据的清洗都收敛到这里，
 * 避免各页面各写一份导致字段漂移。
 */
import { toToolModelDefaults } from '@/utils/models';
import type {
  BaseProfile,
  CapabilityCatalog,
  ProfilePipelineRef,
  ProfileToolRef,
} from '@/pages/AgentPlayground/types';
import type { AgentLoopDefaultTemplateProfile } from '@/types/agentLoopTemplate';

/** Profile schema 允许出现的顶层字段 */
const ALLOWED_KEYS = new Set<keyof BaseProfile>([
  'defaultModel',
  'system',
  'tools',
  'toolDefaults',
  'httpTools',
  'pipelines',
]);

/** 可提供 name→ref 的来源：能力目录、官方工具广场、/agentk/tools 等 */
export type ToolRefSource =
  | CapabilityCatalog
  | null
  | undefined
  | readonly { name: string; ref?: string }[];

function toolRefEntries(source?: ToolRefSource): readonly { name: string; ref?: string }[] {
  if (!source) return [];
  if (Array.isArray(source)) return source;
  return (source as CapabilityCatalog).tools ?? [];
}

/** 合并多个来源的 name→ref（后者不覆盖已有非空 ref） */
export function collectToolRefMap(...sources: ToolRefSource[]): Map<string, string> {
  const map = new Map<string, string>();
  for (const source of sources) {
    for (const tool of toolRefEntries(source)) {
      if (!tool?.name || !tool.ref || map.has(tool.name)) continue;
      map.set(tool.name, tool.ref);
    }
  }
  return map;
}

/** 工具名 → Profile tools 项；有 Catalog / 官方工具 ref 时写入，否则仅 name */
export function toToolRefs(names: readonly string[], ...sources: ToolRefSource[]): ProfileToolRef[] {
  const refByName = collectToolRefMap(...sources);
  const seen = new Set<string>();
  return names.flatMap((name) => {
    if (!name || seen.has(name)) return [];
    seen.add(name);
    const ref = refByName.get(name);
    return [ref ? { ref, name } : { name }];
  });
}

/** Profile tools → 工具名列表（供勾选态与展示使用） */
export function toToolNames(tools: BaseProfile['tools']): string[] {
  return (tools ?? []).map((tool) => tool.name).filter(Boolean);
}

function capabilityToToolRef(capability: string): string {
  const trimmed = capability.trim();
  if (!trimmed) return '';
  return trimmed.startsWith('tool:') ? trimmed : `tool:${trimmed}`;
}

/**
 * AgentLoop 默认模版 profile → 可保存的 BaseProfile
 * 模版 tools 使用 capability 字段，需转为 Profile 所需的 tool ref
 */
export function profileFromAgentLoopTemplate(
  source: AgentLoopDefaultTemplateProfile,
  catalog?: CapabilityCatalog | null,
  ...toolRefSources: ToolRefSource[]
): BaseProfile {
  const tools = source.tools?.flatMap((item) => {
    if (!item?.name) return [];
    const ref =
      (typeof item.ref === 'string' && item.ref) ||
      (typeof item.capability === 'string' && item.capability
        ? capabilityToToolRef(item.capability)
        : undefined);
    const tool: ProfileToolRef = { name: item.name };
    if (ref) tool.ref = ref;
    if (typeof item.instructions === 'string' && item.instructions) {
      tool.instructions = item.instructions;
    }
    return [tool];
  });
  return sanitizeProfile({ ...source, tools }, catalog, ...toolRefSources);
}

/** Pipeline 名 → Profile pipelines 项；目录里没有 ref 的直接丢弃，写进文档必被拒 */
export function toPipelineRefs(
  names: readonly string[],
  catalog?: CapabilityCatalog | null,
): ProfilePipelineRef[] {
  const byName = new Map((catalog?.pipelines ?? []).map((pipeline) => [pipeline.name, pipeline]));
  const seen = new Set<string>();
  return names.flatMap((name) => {
    const pipeline = byName.get(name);
    if (!pipeline || seen.has(name)) return [];
    seen.add(name);
    return [{ ref: pipeline.ref, name: pipeline.name }];
  });
}

/** 目录中全部 Pipeline 的暴露声明 */
export function allPipelineRefs(catalog?: CapabilityCatalog | null): ProfilePipelineRef[] {
  return (catalog?.pipelines ?? []).map((pipeline) => ({ ref: pipeline.ref, name: pipeline.name }));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

/** 历史数据里 tools 可能是字符串数组，统一收敛成 ProfileToolRef；缺 ref 时从目录补齐 */
function normalizeTools(value: unknown, ...sources: ToolRefSource[]): ProfileToolRef[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const refByName = collectToolRefMap(...sources);
  return value.flatMap((item) => {
    if (typeof item === 'string') {
      if (!item) return [];
      const ref = refByName.get(item);
      return [ref ? { ref, name: item } : { name: item }];
    }
    if (!isRecord(item) || typeof item.name !== 'string' || !item.name) return [];
    const tool: ProfileToolRef = { name: item.name };
    const ref =
      typeof item.ref === 'string' && item.ref ? item.ref : refByName.get(item.name);
    if (ref) tool.ref = ref;
    if (typeof item.instructions === 'string' && item.instructions) {
      tool.instructions = item.instructions;
    }
    return [tool];
  });
}

/** 历史数据里 pipelines 带 kind/taskQueue 等执行细节，新 schema 只收 ref/name/displayName */
function pipelineItemName(item: unknown): unknown {
  if (typeof item === 'string') return item;
  if (isRecord(item)) return item.name;
  return undefined;
}

function normalizePipelines(
  value: unknown,
  catalog?: CapabilityCatalog | null,
): ProfilePipelineRef[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const refByName = new Map((catalog?.pipelines ?? []).map((item) => [item.name, item.ref]));
  return value.flatMap((item) => {
    const name = pipelineItemName(item);
    if (typeof name !== 'string' || !name) return [];
    const rawRef = isRecord(item) && typeof item.ref === 'string' ? item.ref : undefined;
    const ref = rawRef || refByName.get(name);
    if (!ref) return [];
    const pipeline: ProfilePipelineRef = { ref, name };
    if (isRecord(item) && typeof item.displayName === 'string' && item.displayName) {
      pipeline.displayName = item.displayName;
    }
    return [pipeline];
  });
}

/** toolDefaults 的值必须是对象；历史的扁平 `工具名: 模型名` 写法已无处安放，直接丢弃 */
function normalizeToolDefaults(value: unknown): BaseProfile['toolDefaults'] | undefined {
  if (!isRecord(value)) return undefined;
  const result: Record<string, Record<string, unknown>> = {};
  for (const [name, defaults] of Object.entries(value)) {
    if (isRecord(defaults)) result[name] = defaults;
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

/**
 * 清洗任意来源的 Profile 对象，只保留新 schema 允许的字段并归一化形状
 * 用于读取历史 AgentLoop 版本配置，避免旧字段被原样回写或下发
 * @param toolRefSources 额外的 name→ref 来源（如 official/tools），用于补齐历史缺 ref 的 tools
 */
export function sanitizeProfile(
  source: unknown,
  catalog?: CapabilityCatalog | null,
  ...toolRefSources: ToolRefSource[]
): BaseProfile {
  if (!isRecord(source)) return {};
  const profile: BaseProfile = {};

  if (typeof source.defaultModel === 'string' && source.defaultModel) {
    profile.defaultModel = source.defaultModel;
  }
  if (typeof source.system === 'string') profile.system = source.system;

  const tools = normalizeTools(source.tools, catalog, ...toolRefSources);
  if (tools) profile.tools = tools;

  const toolDefaults = normalizeToolDefaults(source.toolDefaults);
  if (toolDefaults) profile.toolDefaults = toolDefaults;

  if (Array.isArray(source.httpTools)) profile.httpTools = source.httpTools;

  const pipelines = normalizePipelines(source.pipelines, catalog);
  if (pipelines) profile.pipelines = pipelines;

  return profile;
}

/** 组装会话 Profile 的入参 */
export interface BuildSessionProfileOptions {
  catalog?: CapabilityCatalog | null;
  /** 已有的 Profile 基底（如 Override 编辑器内容、Agent 版本配置） */
  base?: BaseProfile | null;
  system?: string;
  /** 主对话模型；对应 GET /agentk/models 的 chatModels */
  defaultModel?: string;
  /** 启用的工具名；不传则沿用 base 里的 tools */
  toolNames?: readonly string[];
  /** 工具子模型选择（写入 toolDefaults.<tool>.model） */
  toolModels?: Record<string, string>;
  /** 暴露的 Pipeline 名；不传则沿用 base，base 也没有时暴露目录全量 */
  pipelineNames?: readonly string[];
}

/**
 * 组装一份可直接下发的会话 Profile
 * 主模型 / 工具子模型均来自 GET /agentk/models
 */
export function buildSessionProfile({
  catalog,
  base,
  system,
  defaultModel,
  toolNames,
  toolModels,
  pipelineNames,
}: BuildSessionProfileOptions): BaseProfile {
  const profile = sanitizeProfile(base ?? {}, catalog);

  if (system !== undefined) {
    const trimmed = system.trim();
    if (trimmed) profile.system = trimmed;
    else delete profile.system;
  }

  if (defaultModel !== undefined) {
    const trimmed = defaultModel.trim();
    if (trimmed) profile.defaultModel = trimmed;
    else delete profile.defaultModel;
  }

  if (toolNames) profile.tools = toToolRefs(toolNames, catalog);

  const enabledTools = toolNames ?? toToolNames(profile.tools);
  if (toolModels) {
    const merged = toToolModelDefaults(enabledTools, toolModels, profile.toolDefaults);
    if (merged) profile.toolDefaults = merged;
  }

  if (pipelineNames) {
    profile.pipelines = toPipelineRefs(pipelineNames, catalog);
  } else if (!profile.pipelines?.length) {
    profile.pipelines = allPipelineRefs(catalog);
  }

  if (!profile.tools?.length) delete profile.tools;
  if (!profile.pipelines?.length) delete profile.pipelines;

  return profile;
}

/** 供调试面板展示：文档里出现的、新 schema 不认识的字段 */
export function unknownProfileKeys(source: unknown): string[] {
  if (!isRecord(source)) return [];
  return Object.keys(source).filter((key) => !ALLOWED_KEYS.has(key as keyof BaseProfile));
}
