/**
 * Profile YAML 构建与解析工具
 *
 * 编辑器里展示的就是 agent-k Profile 文档本体：
 *   system: |
 *     ...
 *   tools:
 *     - ref: tool:agentk.generate_brand_ad
 *       name: generate_brand_ad
 *   pipelines:
 *     - ref: pipeline:agentk.storyboard_film
 *       name: storyboard_film
 *
 * 解析结果一律经 sanitizeProfile 清洗，保证历史字段不会被回写给后端。
 */
import { dump, load } from 'js-yaml';
import { sanitizeProfile, toToolNames, toToolRefs } from '@/utils/profile';
import type {
  BaseProfile,
  CapabilityCatalog,
  ProfilePipelineRef,
} from '@/pages/AgentPlayground/types';

export interface ProfileYamlData {
  /** System Prompt */
  systemPrompt?: string;
  /** 主对话模型（profile.defaultModel） */
  defaultModel?: string;
  /** 启用工具名；undefined 表示文档未写 tools 字段，空数组表示显式清空 */
  tools?: string[];
  /** 暴露给会话的 Pipeline */
  pipelines?: ProfilePipelineRef[];
}

/**
 * 将 YAML 每行前导 Tab 转回 2 空格
 * 编辑器内为视觉效果会用 Tab 缩进（tab-size: 8），但 YAML 规范禁止 Tab 缩进，
 * 发送给后端前必须调用本方法规范化，否则服务端解析报错
 */
export function tabsToSpaces(text: string): string {
  return text.replace(/^\t+/gm, (tabs) => '  '.repeat(tabs.length));
}

/** 把 Profile object 转为编辑器可读 YAML；字段保持 Agent-k 官方命名 */
export function sessionProfileToYaml(profile: BaseProfile): string {
  return dump(profile, { lineWidth: -1, noRefs: true, sortKeys: false }).trimEnd();
}

/**
 * 将 Agent-k 配置构建为 Profile YAML 原文
 * 传入 catalog 时工具会带上 Catalog ref，否则按内置工具名下发
 */
export function buildProfileYaml(
  config: ProfileYamlData,
  catalog?: CapabilityCatalog | null,
): string {
  const profile: BaseProfile = {};
  const system = (config.systemPrompt || '').replace(/\t/g, '  ').trim();
  if (system) profile.system = system;
  const defaultModel = (config.defaultModel || '').trim();
  if (defaultModel) profile.defaultModel = defaultModel;
  if (config.tools?.length) profile.tools = toToolRefs(config.tools, catalog);
  if (config.pipelines?.length) profile.pipelines = config.pipelines;
  return Object.keys(profile).length > 0 ? sessionProfileToYaml(profile) : '';
}

/** 将编辑器里的 Profile YAML 转为创建会话接口要求的 JSON object */
export function profileYamlToSessionProfile(
  yaml: string,
  catalog?: CapabilityCatalog | null,
): BaseProfile {
  const parsed = load(tabsToSpaces(yaml));
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Profile 必须是一个有效对象');
  }
  return sanitizeProfile(parsed, catalog);
}

/**
 * 从 Profile YAML 原文解析出展示所需的 system / tools / pipelines
 * 编辑过程中 YAML 可能暂时不合法，此时退化为行级正则解析，保证左栏勾选态不闪断
 */
export function parseProfileYaml(yaml: string): ProfileYamlData {
  if (!yaml.trim()) return {};
  const text = tabsToSpaces(yaml).replace(/\t/g, '  ');

  try {
    const parsed = load(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const source = parsed as Record<string, unknown>;
      const profile = sanitizeProfile(source);
      const result: ProfileYamlData = {};
      if (typeof source.system === 'string') result.systemPrompt = source.system;
      if (profile.defaultModel) result.defaultModel = profile.defaultModel;
      if (Array.isArray(source.tools)) result.tools = toToolNames(profile.tools);
      if (profile.pipelines?.length) result.pipelines = profile.pipelines;
      return result;
    }
  } catch {
    // 编辑中途的半成品 YAML，走下面的行级解析
  }

  return parsePartialProfileYaml(text);
}

/** 行级兜底解析：只认 system 块与 tools 列表，够左栏回显即可 */
function parsePartialProfileYaml(text: string): ProfileYamlData {
  const result: ProfileYamlData = {};

  const systemMatch = text.match(/^system:\s*\|?\s*\n([\s\S]*?)(?=\n^[^ \t]|\n$|$)/m);
  if (systemMatch) {
    result.systemPrompt = systemMatch[1]
      .split('\n')
      .map((line) => line.replace(/^[ \t]+/, ''))
      .join('\n')
      .replace(/\n+$/, '');
  }

  const inlineMatch = text.match(/^tools:\s*\[([^\]]*)\]/m);
  if (inlineMatch) {
    result.tools = inlineMatch[1]
      .split(',')
      .map((tool) => tool.trim().replace(/['"]/g, ''))
      .filter(Boolean);
    return result;
  }

  const blockMatch = text.match(/^tools:\s*\n((?:[ \t]+.+\n?)+)/m);
  if (blockMatch) {
    result.tools = blockMatch[1]
      .split('\n')
      // 同时兼容 `- name: x`（新 schema）与 `- x`（历史写法）
      .map((line) => line.match(/^[ \t]+-?\s*(?:name:\s*)?([\w-]+)\s*$/)?.[1])
      .filter((name): name is string => !!name);
  }

  return result;
}

/**
 * 把当前工具勾选同步进 Profile YAML，system / pipelines 等其余内容原样保留
 * YAML 暂时不合法时原样返回，等用户改好再同步
 */
export function patchProfileYamlTools(
  yaml: string,
  toolNames: string[],
  catalog?: CapabilityCatalog | null,
): string {
  if (!yaml.trim()) return yaml;
  let parsed: unknown;
  try {
    parsed = load(tabsToSpaces(yaml));
  } catch {
    return yaml;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return yaml;

  const profile = sanitizeProfile(parsed, catalog);
  if (toolNames.length > 0) profile.tools = toToolRefs(toolNames, catalog);
  else delete profile.tools;
  return sessionProfileToYaml(profile);
}

/** Pipeline 输入参数信息 */
export interface WorkflowParamInfo {
  name: string;
  type?: string;
  description?: string;
  required: boolean;
  /** 其余约束原文（如 minimum: 2, maximum: 6） */
  constraints?: string;
}

/** Pipeline 节点信息 */
export interface WorkflowNodeInfo {
  name: string;
  /** 节点类型：llm / tool / map 等 */
  type?: string;
  /** 工具名：tool 节点为自身 tool；map 节点取内层 node 的 tool */
  tool?: string;
  /** map 节点的 over 表达式 */
  over?: string;
  /** 节点原始定义（仅 JSON 来源有，用于 tooltip 展示 args/prompt 等完整内容） */
  raw?: unknown;
}

/** Pipeline 详情（用于版本详情可视化） */
export interface WorkflowDetail {
  name: string;
  description?: string;
  inputs: WorkflowParamInfo[];
  outputs: Array<{ name: string; value: string }>;
  nodes: WorkflowNodeInfo[];
}
