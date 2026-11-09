/**
 * Pipeline definitionJson 工具
 * - workflowDetailsFromJson：把持久化的定义 JSON 映射为 WorkflowDetail（复用版本详情可视化）
 * - buildDefinitionSkeleton：从运行态 Pipeline 摘要生成可编辑的定义骨架
 */
import type { WorkflowDetail, WorkflowNodeInfo, WorkflowParamInfo } from './profileYaml';
import type { WorkflowDefinition } from '@/pages/AgentPlayground/types';

export type DefinitionJsonValue = string | Record<string, unknown>;

/** 兼容后端返回 JSON 字符串、双重编码字符串或已反序列化对象。 */
function parseDefinitionJson(value: DefinitionJsonValue): Record<string, unknown> | null {
  let parsed: unknown = value;
  try {
    for (let depth = 0; depth < 2 && typeof parsed === 'string'; depth += 1) {
      parsed = JSON.parse(parsed);
    }
  } catch {
    return null;
  }
  return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
    ? (parsed as Record<string, unknown>)
    : null;
}

/** definitionJson 中单个 workflow 的原始结构（宽松定义，字段缺失自动容错） */
interface RawWorkflowDef {
  description?: string;
  inputs?: {
    required?: string[];
    properties?: Record<string, Record<string, unknown>>;
  };
  outputs?: Record<string, unknown>;
  nodes?: Record<string, RawNodeDef>;
}

interface RawNodeDef {
  type?: string;
  tool?: string;
  over?: string;
  node?: RawNodeDef;
}

/** 输入属性 schema → 约束文本（如 minimum: 2, maximum: 6） */
function schemaConstraints(schema: Record<string, unknown>): string | undefined {
  const parts = Object.entries(schema)
    .filter(([k]) => !['type', 'description', 'default'].includes(k))
    .map(([k, v]) => `${k}: ${typeof v === 'object' ? JSON.stringify(v) : String(v)}`);
  return parts.length > 0 ? parts.join(', ') : undefined;
}

/**
 * 把 definitionJson 解析为 WorkflowDetail 列表（用于版本详情可视化）。
 * 解析失败或结构不含 workflows 时返回空数组
 */
export function workflowDetailsFromJson(definitionJson: DefinitionJsonValue): WorkflowDetail[] {
  const root = parseDefinitionJson(definitionJson) as {
    workflows?: Record<string, RawWorkflowDef>;
  } | null;
  if (!root || typeof root !== 'object' || !root.workflows) return [];

  return Object.entries(root.workflows).map(([name, def]) => {
    // 输入参数
    const required = def.inputs?.required ?? [];
    const inputs: WorkflowParamInfo[] = Object.entries(def.inputs?.properties ?? {}).map(
      ([pname, schema]) => ({
        name: pname,
        type: typeof schema.type === 'string' ? schema.type : undefined,
        description: typeof schema.description === 'string' ? schema.description : undefined,
        required: required.includes(pname),
        constraints: schemaConstraints(schema),
      }),
    );
    // 输出
    const outputs = Object.entries(def.outputs ?? {}).map(([oname, value]) => ({
      name: oname,
      value: typeof value === 'string' ? value : JSON.stringify(value),
    }));
    // 节点：map 节点取内层 node 的 tool 作为实际执行工具；raw 保留完整定义供 tooltip 展示
    const nodes: WorkflowNodeInfo[] = Object.entries(def.nodes ?? {}).map(([nname, n]) => ({
      name: nname,
      type: n.type,
      tool: n.tool ?? n.node?.tool,
      over: n.over,
      raw: n,
    }));
    return { name, description: def.description, inputs, outputs, nodes };
  });
}

/** 安全美化 definitionJson（非法 JSON 原样返回） */
export function prettyDefinitionJson(definitionJson: DefinitionJsonValue): string {
  const parsed = parseDefinitionJson(definitionJson);
  return parsed ? JSON.stringify(parsed, null, 2) : String(definitionJson);
}

/**
 * JSON 值 → YAML 文本（无第三方依赖）。
 * 字符串一律输出 JSON 双引号形式（JSON string 是 YAML 1.2 合法 flow scalar），
 * 从根上避开 ${...} / # / 冒号 / 多行 prompt 的转义坑；结构用两空格块缩进
 */
export function jsonToYaml(value: unknown, indent = 0): string {
  const pad = '  '.repeat(indent);
  if (value === null || value === undefined) return 'null';
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  if (typeof value === 'string') return JSON.stringify(value);
  if (Array.isArray(value)) {
    if (value.length === 0) return '[]';
    // 标量数组用 inline（如 required: [topic]）
    if (value.every((v) => v === null || typeof v !== 'object')) {
      return `[${value.map((v) => jsonToYaml(v, 0)).join(', ')}]`;
    }
    // 对象数组项：首行接在 "- " 后（"- " 宽度恰好等于一级缩进），其余行保持原缩进
    return value
      .map((item) => {
        const lines = jsonToYaml(item, indent + 1).split('\n');
        lines[0] = `${pad}- ${lines[0].slice(pad.length + 2)}`;
        return lines.join('\n');
      })
      .join('\n');
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return '{}';
    const childPad = '  '.repeat(indent + 1);
    return entries
      .map(([k, v]) => {
        const rendered = jsonToYaml(v, indent + 1);
        // 块级值（嵌套对象/对象数组，渲染结果自带子级缩进或含换行）：key 独占一行，值换行缩进
        if (rendered.startsWith(childPad) || rendered.includes('\n')) {
          return `${pad}${k}:\n${rendered}`;
        }
        return `${pad}${k}: ${rendered}`;
      })
      .join('\n');
  }
  return 'null';
}

/**
 * definitionJson → 完整 workflows YAML 段（含 "workflows:" 根行），
 * 用于合并进会话 profile YAML 后建会话运行；解析失败返回 null
 */
export function definitionJsonToWorkflowsYaml(definitionJson: DefinitionJsonValue): string | null {
  const root = parseDefinitionJson(definitionJson) as {
    workflows?: Record<string, unknown>;
  } | null;
  if (!root || typeof root !== 'object' || !root.workflows) return null;
  return `workflows:\n${jsonToYaml(root.workflows, 1)}`;
}

/**
 * 从运行态 Pipeline 摘要生成可编辑的定义骨架：
 * inputs 由参数表单 schema 重建，outputs/nodes 留空由用户补全
 */
export function buildDefinitionSkeleton(wf: WorkflowDefinition | null): string {
  const name = wf?.name || 'my_workflow';
  const properties: Record<string, Record<string, unknown>> = {};
  const required: string[] = [];
  for (const p of wf?.params ?? []) {
    const schema: Record<string, unknown> = {
      type:
        p.type === 'text' || p.type === 'select'
          ? 'string'
          : p.type === 'number'
            ? 'integer'
            : p.type,
      description: p.label,
    };
    if (p.min !== undefined) schema.minimum = p.min;
    if (p.max !== undefined) schema.maximum = p.max;
    if (p.defaultValue !== undefined) schema.default = p.defaultValue;
    if (p.options) schema.enum = p.options.map((o) => o.value);
    properties[p.name] = schema;
    if (p.required) required.push(p.name);
  }

  const skeleton = {
    workflows: {
      [name]: {
        description: wf?.description || '',
        inputs: {
          type: 'object',
          ...(required.length > 0 ? { required } : {}),
          properties,
        },
        outputs: {},
        nodes: {},
      },
    },
  };
  return JSON.stringify(skeleton, null, 2);
}
