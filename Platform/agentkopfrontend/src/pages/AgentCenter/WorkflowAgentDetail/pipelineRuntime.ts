/**
 * 工作流 Agent 预览：草稿定义 → 运行参数表单模型与运行入参组装
 * 会话创建与节点状态映射等跨页面共用逻辑见 @/utils/workflowRun
 */
import type { WorkflowDefinition } from '@/pages/AgentPlayground/types';
import { workflowDetailsFromJson } from '@/utils/workflowDefinition';

/** 草稿 definitionJson → 运行参数表单模型（/pipelines 尚未回填时兜底） */
export function definitionsFromDraft(
  definitionJson: string | Record<string, unknown>,
): WorkflowDefinition[] {
  return workflowDetailsFromJson(definitionJson).map((detail) => ({
    id: detail.name,
    name: detail.name,
    description: detail.description || '',
    nodes: detail.nodes.map((node) => ({
      name: node.name,
      label: node.name,
      description: node.tool,
    })),
    params: detail.inputs.map((input) => ({
      name: input.name,
      label: input.description || input.name,
      type:
        input.type === 'number' || input.type === 'integer'
          ? 'number'
          : input.type === 'array'
            ? 'array'
            : input.type === 'object'
              ? 'object'
              : 'text',
      required: input.required,
      placeholder: input.description,
    })),
  }));
}

/** 组装运行入参；失败时返回错误文案 */
export function buildSubmitInputs(
  wf: WorkflowDefinition,
  paramMode: 'form' | 'json',
  rawArgsJson: string,
  workflowParams: Record<string, string | number>,
): { ok: true; inputs: Record<string, unknown> } | { ok: false; error: string } {
  if (paramMode === 'json') {
    try {
      const parsed: unknown = JSON.parse(rawArgsJson.trim() || '{}');
      if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
        return { ok: false, error: 'args 必须是 JSON object' };
      }
      return { ok: true, inputs: parsed as Record<string, unknown> };
    } catch {
      return { ok: false, error: 'args 不是合法 JSON，请检查格式' };
    }
  }

  const inputs: Record<string, unknown> = {};
  for (const p of wf.params) {
    const raw = workflowParams[p.name];
    if (p.required && (raw === '' || raw === undefined)) {
      return { ok: false, error: `请填写「${p.label}」` };
    }
    if ((p.type === 'array' || p.type === 'object') && typeof raw === 'string' && raw.trim()) {
      try {
        inputs[p.name] = JSON.parse(raw);
      } catch {
        return { ok: false, error: `「${p.label}」不是合法 JSON` };
      }
    } else if (raw !== '' && raw !== undefined) {
      inputs[p.name] = raw;
    }
  }
  return { ok: true, inputs };
}

/** 按 schema 回填默认参数（array/object 序列化为 JSON 文本） */
export function buildDefaultParams(wf: WorkflowDefinition): Record<string, string | number> {
  const defaults: Record<string, string | number> = {};
  for (const p of wf.params) {
    if (p.defaultValue !== undefined) {
      defaults[p.name] =
        (p.type === 'array' || p.type === 'object') && typeof p.defaultValue !== 'string'
          ? JSON.stringify(p.defaultValue)
          : p.defaultValue;
    } else if (p.type === 'number') {
      defaults[p.name] = p.min ?? '';
    } else {
      defaults[p.name] = '';
    }
  }
  return defaults;
}
