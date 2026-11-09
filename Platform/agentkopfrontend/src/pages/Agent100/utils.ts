/**
 * Agent100 展示与选项辅助
 */
import { sanitizeDocumentaryScript } from './documentaryV2';
import { AGENT100_FIELD_LABELS } from './constants';
import type {
  Agent100Config,
  Agent100FieldOption,
  Agent100Summary,
  Agent100Voice,
  FieldOptionItem,
  JsonSchemaNode,
} from './types';

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

export function fieldLabel(name: string): string {
  return AGENT100_FIELD_LABELS[name] ?? name;
}

export function isUsableIcon(icon: string | undefined): boolean {
  if (!icon) return false;
  const trimmed = icon.trim();
  if (!trimmed || trimmed.toUpperCase() === 'N/A') return false;
  return true;
}

export function isIconUrl(icon: string): boolean {
  return /^(https?:)?\/\//.test(icon) || icon.startsWith('/');
}

export function collectVoices(voices: Record<string, Agent100Voice[]> | undefined): Agent100Voice[] {
  if (!voices) return [];
  const seen = new Set<string>();
  const list: Agent100Voice[] = [];
  for (const group of Object.values(voices)) {
    for (const voice of group ?? []) {
      if (!voice?.id || seen.has(voice.id)) continue;
      seen.add(voice.id);
      list.push(voice);
    }
  }
  return list;
}

export function findAgentSummary(
  agents: Agent100Summary[] | undefined,
  agentId: string,
): Agent100Summary | undefined {
  return agents?.find((item) => item.id === agentId);
}

export function capabilityOf(
  config: Agent100Config | undefined,
  capabilityId: string,
) {
  return config?.capabilities.find((item) => item.id === capabilityId);
}

function optionFromField(item: Agent100FieldOption): FieldOptionItem {
  return { value: String(item.id), label: item.label || String(item.id) };
}

export function resolveSelectOptions(
  name: string,
  schema: JsonSchemaNode,
  context: {
    fieldOptions: Record<string, Agent100FieldOption[]>;
    voices: Agent100Voice[];
    modelOptions: Record<string, string[]>;
  },
): FieldOptionItem[] {
  const fromField = context.fieldOptions[name];
  if (fromField?.length) return fromField.map(optionFromField);

  if (name === 'voiceId' && context.voices.length > 0) {
    return context.voices.map((voice) => ({
      value: voice.id,
      label: voice.description ? `${voice.id} · ${voice.description}` : voice.id,
    }));
  }

  const fromModels = context.modelOptions[name];
  if (fromModels?.length) {
    return fromModels.map((id) => ({ value: id, label: id }));
  }

  if (schema.enum?.length) {
    return schema.enum.map((item) => ({ value: String(item), label: String(item) }));
  }

  return [];
}

export function extractScriptPayload(output: Record<string, unknown> | null | undefined): unknown {
  if (!output) return undefined;
  if (isPlainObject(output.script)) return sanitizeDocumentaryScript(output.script);
  if (typeof output.script === 'string' && output.script.trim()) {
    try {
      return sanitizeDocumentaryScript(JSON.parse(output.script) as unknown);
    } catch {
      return output.script;
    }
  }
  if (Array.isArray(output.segments) || output.version != null) {
    return sanitizeDocumentaryScript(output);
  }
  if (isPlainObject(output.data)) return extractScriptPayload(output.data);
  if (isPlainObject(output.result)) return extractScriptPayload(output.result);
  return undefined;
}

export function prettyJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}

export function findRenderPipelineName(
  pipelines: { name: string; inputSchema: JsonSchemaNode }[],
): string | undefined {
  return pipelines.find((item) => item.inputSchema.properties?.script)?.name;
}
