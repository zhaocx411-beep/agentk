/**
 * 将 Pipeline inputSchema 转为表单默认值、校验与提交 args
 */
import { defaultTtsValue } from './ttsConfig';
import { fieldLabel, isPlainObject, prettyJson } from './utils';
import type { Agent100Config, Agent100Pipeline, JsonSchemaNode } from './types';

export type PipelineFormValues = Record<string, unknown>;

export function isComplexSchemaObject(schema: JsonSchemaNode | undefined): boolean {
  if (!schema || schema.type !== 'object' || !schema.properties) return false;
  return Object.values(schema.properties).some((child) => {
    if (child.type === 'array') return true;
    if (child.type === 'object') return isComplexSchemaObject(child);
    return false;
  });
}

export function skeletonFromSchema(schema: JsonSchemaNode): unknown {
  if (schema.const !== undefined) return schema.const;
  if (schema.default !== undefined) return schema.default;
  if (schema.type === 'object') {
    const obj: Record<string, unknown> = {};
    const required = new Set(schema.required ?? []);
    for (const [key, child] of Object.entries(schema.properties ?? {})) {
      if (!required.has(key) && child.default === undefined && child.const === undefined) continue;
      obj[key] = skeletonFromSchema(child);
    }
    return obj;
  }
  if (schema.type === 'array') {
    return schema.items ? [skeletonFromSchema(schema.items)] : [];
  }
  if (schema.type === 'boolean') return false;
  if (schema.type === 'number' || schema.type === 'integer') return 0;
  if (schema.type === 'string') return schema.enum?.[0] ?? '';
  return null;
}

function defaultForProperty(
  name: string,
  schema: JsonSchemaNode,
  config: Agent100Config,
): unknown {
  if (schema.const !== undefined) return schema.const;
  if (name === 'tts' && schema.type === 'object') {
    const tts = defaultTtsValue(config.tts);
    if (tts.provider) return tts;
  }
  if (name === 'models' && schema.type === 'object') {
    const nested: Record<string, unknown> = {};
    for (const key of Object.keys(schema.properties ?? {})) {
      const fromConfig = config.models.defaults[key];
      const childDefault = schema.properties?.[key]?.default;
      nested[key] = fromConfig ?? childDefault ?? undefined;
    }
    return nested;
  }
  if (schema.default !== undefined) return schema.default;
  if (schema.type === 'object' && isComplexSchemaObject(schema)) {
    return prettyJson(skeletonFromSchema(schema));
  }
  if (schema.type === 'object') {
    const nested: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(schema.properties ?? {})) {
      nested[key] = defaultForProperty(key, child, config);
    }
    return nested;
  }
  if (schema.enum?.length && schema.type !== 'boolean') return schema.enum[0];
  if (schema.type === 'boolean') return undefined;
  return undefined;
}

export function buildPipelineDefaults(
  pipeline: Agent100Pipeline,
  config: Agent100Config,
): PipelineFormValues {
  const values: PipelineFormValues = {};
  for (const [name, schema] of Object.entries(pipeline.inputSchema.properties ?? {})) {
    values[name] = defaultForProperty(name, schema, config);
  }
  return values;
}

/** 历史回填 / 跨 Pipeline 填入：复杂对象转成 JSON 文本以适配表单 */
export function normalizePrefillArgs(
  schema: JsonSchemaNode,
  args: Record<string, unknown>,
): PipelineFormValues {
  const next: PipelineFormValues = { ...args };
  for (const [name, child] of Object.entries(schema.properties ?? {})) {
    if (!(name in args)) continue;
    if (child.type === 'object' && isComplexSchemaObject(child) && isPlainObject(args[name])) {
      next[name] = prettyJson(args[name]);
    }
  }
  return next;
}

function isEmptyValue(value: unknown): boolean {
  if (value === undefined || value === null) return true;
  if (typeof value === 'string' && !value.trim()) return true;
  return false;
}

function coerceScalar(schema: JsonSchemaNode, raw: unknown): unknown {
  if (raw === undefined || raw === null) return undefined;
  if (typeof raw === 'string') return raw;
  if (isEmptyValue(raw)) return undefined;
  if (schema.type === 'boolean') return Boolean(raw);
  if (schema.type === 'integer') {
    const n = typeof raw === 'number' ? raw : Number(raw);
    return Number.isFinite(n) ? Math.trunc(n) : undefined;
  }
  if (schema.type === 'number') {
    const n = typeof raw === 'number' ? raw : Number(raw);
    return Number.isFinite(n) ? n : undefined;
  }
  if (typeof raw === 'string' || typeof raw === 'number' || typeof raw === 'boolean') {
    return raw;
  }
  return undefined;
}

function parseJsonValue(raw: unknown): { ok: true; value: unknown } | { ok: false } {
  if (raw !== undefined && raw !== null && typeof raw !== 'string') {
    return { ok: true, value: raw };
  }
  if (typeof raw !== 'string' || !raw.trim()) return { ok: false };
  try {
    return { ok: true, value: JSON.parse(raw) as unknown };
  } catch {
    return { ok: false };
  }
}

export function collectPipelineArgs(
  schema: JsonSchemaNode,
  values: PipelineFormValues,
): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [name, child] of Object.entries(schema.properties ?? {})) {
    const collected = collectNode(child, values[name]);
    if (collected === undefined) continue;
    result[name] = collected;
  }
  return result;
}

function collectNode(schema: JsonSchemaNode, raw: unknown): unknown {
  if (schema.type === 'object' && isComplexSchemaObject(schema)) {
    const parsed = parseJsonValue(raw);
    return parsed.ok ? parsed.value : undefined;
  }
  if (schema.type === 'object') {
    if (!isPlainObject(raw)) return undefined;
    const nested: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(schema.properties ?? {})) {
      const value = collectNode(child, raw[key]);
      if (value === undefined) continue;
      nested[key] = value;
    }
    return Object.keys(nested).length > 0 ? nested : undefined;
  }
  if (schema.type === 'array') {
    const parsed = parseJsonValue(raw);
    return parsed.ok ? parsed.value : undefined;
  }
  return coerceScalar(schema, raw);
}

export function validatePipelineArgs(
  schema: JsonSchemaNode,
  values: PipelineFormValues,
): string | null {
  const required = schema.required ?? [];
  for (const name of required) {
    const child = schema.properties?.[name];
    if (!child) continue;
    const error = validateNode(name, child, values[name], true);
    if (error) return error;
  }
  for (const [name, child] of Object.entries(schema.properties ?? {})) {
    if (required.includes(name)) continue;
    const error = validateNode(name, child, values[name], false);
    if (error) return error;
  }
  return null;
}

function validateNode(
  name: string,
  schema: JsonSchemaNode,
  raw: unknown,
  required: boolean,
): string | null {
  const label = fieldLabel(name);
  if (schema.type === 'object' && isComplexSchemaObject(schema)) {
    if (required && isEmptyValue(raw)) return `请填写「${label}」`;
    if (isEmptyValue(raw)) return null;
    const parsed = parseJsonValue(raw);
    if (!parsed.ok) return `「${label}」格式不正确，请检查后再试`;
    return null;
  }
  if (schema.type === 'object') {
    if (!isPlainObject(raw)) {
      return required ? `请填写「${label}」` : null;
    }
    for (const key of schema.required ?? []) {
      const child = schema.properties?.[key];
      if (!child) continue;
      const error = validateNode(key, child, raw[key], true);
      if (error) return error;
    }
    return null;
  }
  if (schema.type === 'array') {
    if (required && isEmptyValue(raw)) return `请填写「${label}」`;
    if (isEmptyValue(raw)) return null;
    const parsed = parseJsonValue(raw);
    if (!parsed.ok) return `「${label}」格式不正确，请检查后再试`;
    return null;
  }
  if (required && isEmptyValue(raw)) return `请填写「${label}」`;
  if (!isEmptyValue(raw) && (schema.type === 'number' || schema.type === 'integer')) {
    const n = typeof raw === 'number' ? raw : Number(raw);
    if (!Number.isFinite(n)) return `「${label}」请输入数字`;
    if (schema.minimum != null && n < schema.minimum) {
      return `「${label}」不能小于 ${schema.minimum}`;
    }
    if (schema.maximum != null && n > schema.maximum) {
      return `「${label}」不能大于 ${schema.maximum}`;
    }
  }
  return null;
}

export function getInvalidPipelineFields(
  schema: JsonSchemaNode,
  values: PipelineFormValues,
): Set<string> {
  const invalid = new Set<string>();
  walkInvalid(schema, values, '', invalid);
  return invalid;
}

function walkInvalid(
  schema: JsonSchemaNode,
  raw: unknown,
  path: string,
  invalid: Set<string>,
): void {
  if (schema.type !== 'object' || !schema.properties) return;

  const required = new Set(schema.required ?? []);
  const obj = isPlainObject(raw) ? raw : {};
  for (const [name, child] of Object.entries(schema.properties)) {
    const nextPath = path ? `${path}.${name}` : name;
    const value = obj[name];
    const childRequired = required.has(name);

    if (child.type === 'object' && isComplexSchemaObject(child)) {
      if (childRequired && isEmptyValue(value)) {
        invalid.add(nextPath);
        continue;
      }
      if (!isEmptyValue(value) && !parseJsonValue(value).ok) invalid.add(nextPath);
      continue;
    }

    if (child.type === 'object') {
      walkInvalid(child, value, nextPath, invalid);
      continue;
    }

    if (childRequired && isEmptyValue(value)) {
      invalid.add(nextPath);
    }
  }
}
