/**
 * 将 capabilities 下发的 fields / uiSchema.fields 转为可渲染的 ExecutionSpec
 * 后端新增字段时前端无需改代码，侧栏按 widget 自动出控件
 *
 * 边界策略：脏数据清洗 → 未知 widget 降级 → 默认值/组装全程 try-catch，避免白屏
 */
import type { OfficialUiField } from '@/types/officialCatalog';
import { normalizeOfficialUiFields } from '@/utils/officialUiFields';
import { bareWorkflowKey } from '@/utils/workflowKey';
import { applyComicDramaFieldSpecOverlay, isComicDramaPipeline } from './comicDrama';
import {
  type ExecutionSpec,
  type FieldControl,
  type FieldSpec,
  type FieldValue,
  type FieldValues,
  type RunBuildResult,
  type RunPayload,
} from './executionSpecs';

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function asTrimmedString(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

function safeStringify(value: unknown, fallback = ''): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return fallback;
  }
}

/** 从 JSON Schema 片段提取 enum 选项（含 anyOf 里的 enum） */
export function extractSchemaEnum(schema?: Record<string, unknown>): string[] | undefined {
  if (!isPlainObject(schema)) return undefined;
  try {
    if (Array.isArray(schema.enum)) {
      return schema.enum
        .filter((item) => item != null)
        .map(String)
        .filter(Boolean);
    }
    if (!Array.isArray(schema.anyOf)) return undefined;
    for (const branch of schema.anyOf) {
      if (!isPlainObject(branch) || !Array.isArray(branch.enum)) continue;
      const options = branch.enum
        .filter((item) => item != null)
        .map(String)
        .filter(Boolean);
      if (options.length > 0) return options;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

function schemaNumberBound(schema: Record<string, unknown> | undefined, key: 'minimum' | 'maximum') {
  if (!isPlainObject(schema)) return undefined;
  const value = schema[key];
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

function hintOf(field: OfficialUiField): string | undefined {
  const text = asTrimmedString(field.description);
  const label = asTrimmedString(field.label);
  if (!text || text === label) return undefined;
  if (text.length > 120) return `${text.slice(0, 120)}…`;
  return text;
}

/** 供外部复用清洗能力 */
export { normalizeOfficialUiField, normalizeOfficialUiFields } from '@/utils/officialUiFields';

/** 后端 widget → 侧栏控件；有 schema enum 用下拉，模型字段不再请求 /agentk/models */
export function toFieldSpec(field: OfficialUiField): FieldSpec | null {
  try {
    if (!field?.name) return null;
    if (field.widget === 'image-url-list') return null;

    const enumOptions = extractSchemaEnum(field.schema);
    const widget = asTrimmedString(field.widget).toLowerCase();
    const base = {
      key: field.name,
      label: field.label || field.name,
      hint: hintOf(field),
      required: !!field.required,
      placeholder: field.description || field.label || field.name,
    };

    const keepFreeform =
      widget === 'textarea' ||
      widget === 'number' ||
      widget === 'array' ||
      widget === 'json' ||
      widget === 'json-editor';
    if (enumOptions?.length && !keepFreeform) {
      return {
        ...base,
        control: 'select' as const,
        options: enumOptions.map((value) => ({ value, label: value })),
      };
    }

    switch (widget) {
      case 'textarea':
        return { ...base, control: 'textarea' };
      case 'number':
        return {
          ...base,
          control: 'number',
          min: schemaNumberBound(field.schema, 'minimum'),
          max: schemaNumberBound(field.schema, 'maximum'),
        };
      case 'array': {
        const itemType = field.items?.type ?? field.schema?.items;
        const hasRef = isPlainObject(itemType) && typeof itemType.$ref === 'string';
        const itemsIsString =
          field.items?.type === 'string' ||
          (isPlainObject(itemType) && itemType.type === 'string');
        // 对象数组 / $ref / 元素类型不明 → JSON，避免伪 string 列表
        if (hasRef || !itemsIsString) {
          return { ...base, control: 'json' };
        }
        return { ...base, control: 'array' };
      }
      case 'json-editor':
      case 'json':
        return { ...base, control: 'json' };
      case 'input':
      case 'text':
      default: {
        const type = asTrimmedString(field.type).toLowerCase();
        if (type === 'integer' || type === 'number') {
          return {
            ...base,
            control: 'number',
            min: schemaNumberBound(field.schema, 'minimum'),
            max: schemaNumberBound(field.schema, 'maximum'),
          };
        }
        if (type === 'object' || type === 'array') return { ...base, control: 'json' };
        if (type === 'boolean') {
          return {
            ...base,
            control: 'select',
            options: [
              { value: 'true', label: 'true' },
              { value: 'false', label: 'false' },
            ],
          };
        }
        // 未知 widget：降级为文本，保证能填能传
        return { ...base, control: 'text' };
      }
    }
  } catch {
    if (!field?.name) return null;
    return {
      key: field.name,
      label: field.label || field.name,
      control: 'text',
      required: !!field.required,
    };
  }
}

function coerceDefault(field: OfficialUiField, control: FieldControl | 'image'): FieldValue {
  const raw = field.defaultValue;

  if (control === 'json') {
    if (raw == null || raw === '') return '{}';
    if (typeof raw === 'string') return raw;
    return safeStringify(raw, '{}');
  }
  if (control === 'array' || control === 'image') {
    if (Array.isArray(raw)) return raw.map((item) => String(item ?? ''));
    if (typeof raw === 'string' && raw.trim()) {
      try {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) return parsed.map((item) => String(item ?? ''));
      } catch {
        return [raw.trim()];
      }
    }
    return [];
  }
  if (control === 'number') {
    if (typeof raw === 'number' && Number.isFinite(raw)) return raw;
    if (typeof raw === 'string' && raw.trim() !== '') {
      const num = Number(raw);
      if (Number.isFinite(num)) return num;
    }
    return schemaNumberBound(field.schema, 'minimum') ?? 0;
  }
  if (control === 'select') {
    if (raw == null) return '';
    return String(raw);
  }
  // text / textarea
  if (raw == null) return '';
  if (typeof raw === 'string' || typeof raw === 'number' || typeof raw === 'boolean') {
    return String(raw);
  }
  return safeStringify(raw, '');
}

function defaultForField(field: OfficialUiField, control: FieldControl | null): FieldValue {
  try {
    if (field.widget === 'image-url-list') return [];
    if (control) return coerceDefault(field, control);

    // 未进表单的字段（理论上不会走到）
    if (field.type === 'array') return [];
    if (field.type === 'object') return '{}';
    return '';
  } catch {
    return '';
  }
}

/** 把 TEMPLATE 任务里的原始值转成控件可展示的 FieldValue */
function valueFromTemplateArg(
  raw: unknown,
  control: FieldControl | null,
): FieldValue | undefined {
  if (raw === undefined || raw === null) return undefined;
  if (control === 'json') {
    if (typeof raw === 'string') return raw;
    return safeStringify(raw, '{}');
  }
  if (control === 'array') {
    if (Array.isArray(raw)) return raw.map((item) => String(item ?? '')).filter(Boolean);
    if (typeof raw === 'string' && raw.trim()) return [raw.trim()];
    return undefined;
  }
  if (control === 'number') {
    if (typeof raw === 'number' && Number.isFinite(raw)) return raw;
    if (typeof raw === 'string' && raw.trim() !== '') {
      const num = Number(raw);
      if (Number.isFinite(num)) return num;
    }
    return undefined;
  }
  if (typeof raw === 'string' || typeof raw === 'number' || typeof raw === 'boolean') {
    return String(raw);
  }
  if (Array.isArray(raw) || isPlainObject(raw)) return safeStringify(raw, '');
  return undefined;
}

function isBlankFieldValue(value: FieldValue | undefined): boolean {
  if (value === undefined || value === null) return true;
  if (typeof value === 'string') return value.trim() === '';
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

function parseJsonValue(
  raw: FieldValue,
  label: string,
): { ok: true; value: unknown } | { ok: false; error: string } {
  if (raw == null || raw === '') return { ok: true, value: undefined };
  if (typeof raw !== 'string') return { ok: true, value: raw };
  const trimmed = raw.trim();
  if (!trimmed) return { ok: true, value: undefined };
  try {
    return { ok: true, value: JSON.parse(trimmed) };
  } catch {
    return { ok: false, error: `「${label}」不是合法 JSON` };
  }
}

function isEmptyValue(value: unknown): boolean {
  if (value == null) return true;
  if (typeof value === 'string') return value.trim() === '';
  if (Array.isArray(value)) return value.length === 0;
  if (isPlainObject(value)) return Object.keys(value).length === 0;
  return false;
}

function parseBooleanText(text: string): boolean | string {
  if (text === 'true') return true;
  if (text === 'false') return false;
  return text;
}

/**
 * capabilities 无 fields / 解析失败时的空表单。
 * 工作流创作参数只认接口 fields，不再回退内置 PIPELINE_SPECS。
 */
export function emptyPipelineSpec(workflowKey: string, error?: string): ExecutionSpec {
  const key = workflowKey || 'unknown_workflow';
  return {
    id: key,
    mode: 'pipeline',
    label: key,
    description: error || '暂无可用配置字段',
    usesDataset: false,
    usesImages: false,
    fields: [],
    defaults: () => ({}),
    build: () =>
      error
        ? { ok: false, error }
        : { ok: false, error: '当前工作流没有可配置的入参字段' },
    summarize: () => key,
  };
}

/**
 * 按后端 fields 组装通用 Pipeline ExecutionSpec。
 * workflowKey 原样作为 pipeline-runs 的 workflow 名。
 */
export function buildSchemaDrivenSpec(
  workflowKey: string,
  uiFields: OfficialUiField[] | unknown,
): ExecutionSpec {
  const key = asTrimmedString(workflowKey);
  if (!key) {
    return emptyPipelineSpec('', '缺少 workflowKey，无法运行工作流');
  }

  let fields: OfficialUiField[];
  try {
    fields = normalizeOfficialUiFields(uiFields);
  } catch {
    return emptyPipelineSpec(key, '配置字段解析失败，请稍后重试');
  }

  if (fields.length === 0) {
    return emptyPipelineSpec(key);
  }

  try {
    const imageField = fields.find((field) => field.widget === 'image-url-list');
    const formFieldsRaw = fields
      .map((field) => toFieldSpec(field))
      .filter((field): field is FieldSpec => !!field && !!field.key);
    const formFields = isComicDramaPipeline(key)
      ? applyComicDramaFieldSpecOverlay(formFieldsRaw)
      : formFieldsRaw;
    const controlByName = new Map(formFields.map((field) => [field.key, field.control]));
    const bare = bareWorkflowKey(key);
    const isBrandAd = bare === 'generate_brand_ad' || key.includes('generate_brand');

    return {
      id: key,
      mode: 'pipeline',
      label: bare || key,
      description: '按 capabilities 下发的字段配置运行',
      usesDataset: isBrandAd,
      usesImages: !!imageField,
      imagesRequired: !!imageField?.required,
      imageFieldKey: imageField?.name || 'images',
      imageFieldLabel: imageField?.label || imageField?.description || undefined,
      fields: formFields,
      defaults: ({ filledPrompt, sample }) => {
        try {
          const args = sample?.templateArgs ?? {};
          const values: FieldValues = {};
          for (const field of fields) {
            if (field.widget === 'image-url-list') continue;
            const control = controlByName.get(field.name) ?? null;
            // 优先用 /tasks/query 模版参数按字段名回填
            const fromTemplate = valueFromTemplateArg(args[field.name], control);
            if (!isBlankFieldValue(fromTemplate)) {
              values[field.name] = fromTemplate as FieldValue;
              continue;
            }
            values[field.name] = defaultForField(field, control);
          }
          // 模版未给到时再兜底：品牌广告 / 分镜常见字段
          if (
            'video_prompt' in values &&
            isBlankFieldValue(values.video_prompt) &&
            filledPrompt
          ) {
            values.video_prompt = filledPrompt;
          }
          if ('topic' in values && isBlankFieldValue(values.topic) && filledPrompt) {
            values.topic = filledPrompt;
          }
          if ('title' in values && isBlankFieldValue(values.title)) {
            if (isBrandAd) {
              // 历史约定：未给 title 时用业务类型；模版有 title 时已在上方回填
              values.title = sample?.category.startsWith('跨境') ? '跨境电商' : '电商';
            } else if (sample?.title) {
              values.title = sample.title;
            }
          }
          return values;
        } catch {
          return {};
        }
      },
      build: (values, ctx): RunBuildResult => {
        try {
          const args: Record<string, unknown> = {};
          const imageUrls = Array.isArray(ctx?.imageUrls)
            ? ctx.imageUrls.filter((url): url is string => typeof url === 'string' && !!url.trim())
            : [];

          for (const field of fields) {
            const label = field.label || field.name;

            if (field.widget === 'image-url-list') {
              if (field.required && imageUrls.length === 0) {
                return { ok: false, error: `请填写${label}` };
              }
              if (imageUrls.length > 0) args[field.name] = imageUrls;
              continue;
            }

            const control = controlByName.get(field.name) ?? 'text';
            const raw = values?.[field.name];

            if (control === 'json') {
              const parsed = parseJsonValue(raw, label);
              if (!parsed.ok) return parsed;
              if (field.required && isEmptyValue(parsed.value)) {
                return { ok: false, error: `请填写${label}` };
              }
              if (parsed.value !== undefined) args[field.name] = parsed.value;
              continue;
            }

            if (control === 'array') {
              const list = Array.isArray(raw)
                ? raw.map((item) => String(item ?? '').trim()).filter(Boolean)
                : typeof raw === 'string' && raw.trim()
                  ? [raw.trim()]
                  : [];
              if (field.required && list.length === 0) {
                return { ok: false, error: `请至少填写一项 ${label}` };
              }
              if (list.length > 0) args[field.name] = list;
              continue;
            }

            if (control === 'number') {
              const num = typeof raw === 'number' ? raw : Number(raw);
              const empty = raw === '' || raw == null || Number.isNaN(num) || !Number.isFinite(num);
              if (field.required && empty) {
                return { ok: false, error: `请填写${label}` };
              }
              if (!empty) {
                const min = schemaNumberBound(field.schema, 'minimum');
                const max = schemaNumberBound(field.schema, 'maximum');
                if (min != null && num < min) {
                  return { ok: false, error: `「${label}」不能小于 ${min}` };
                }
                if (max != null && num > max) {
                  return { ok: false, error: `「${label}」不能大于 ${max}` };
                }
                args[field.name] = field.type === 'integer' ? Math.trunc(num) : num;
              }
              continue;
            }

            const text =
              typeof raw === 'string'
                ? raw.trim()
                : raw == null
                  ? ''
                  : typeof raw === 'object'
                    ? ''
                    : String(raw).trim();

            if (field.required && !text) {
              return { ok: false, error: `请填写${label}` };
            }
            if (!text) continue;

            // boolean select 传真正的布尔值
            if (field.type === 'boolean') {
              args[field.name] = parseBooleanText(text);
            } else {
              args[field.name] = text;
            }
          }

          return {
            ok: true,
            payload: {
              mode: 'pipeline',
              workflow: key,
              args,
            },
          };
        } catch (error) {
          return {
            ok: false,
            error: error instanceof Error ? error.message : '参数组装失败，请检查填写内容',
          };
        }
      },
      summarize: (payload: RunPayload) => {
        if (payload.mode !== 'pipeline') return '';
        try {
          const text = JSON.stringify(payload.args);
          return text.length > 200 ? `${text.slice(0, 200)}…` : text;
        } catch {
          return key;
        }
      },
    };
  } catch (error) {
    return emptyPipelineSpec(
      key,
      error instanceof Error ? `配置解析失败：${error.message}` : '配置解析失败，请稍后重试',
    );
  }
}

/** 从 pipeline-runs 入参快照回填动态表单（对齐 valueFromTemplateArg / 模版回填逻辑） */
export function fieldOverridesFromPipelineInputs(
  uiFields: OfficialUiField[] | unknown,
  inputs: Record<string, unknown>,
): FieldValues {
  let fields: OfficialUiField[];
  try {
    fields = normalizeOfficialUiFields(uiFields);
  } catch {
    return {};
  }

  const overrides: FieldValues = {};
  for (const field of fields) {
    if (field.widget === 'image-url-list') continue;
    const spec = toFieldSpec(field);
    if (!spec) continue;
    const value = valueFromTemplateArg(inputs[field.name], spec.control);
    if (!isBlankFieldValue(value)) {
      overrides[field.name] = value as FieldValue;
    }
  }
  return overrides;
}

/** 从 pipeline-runs 入参恢复参考图列表 */
export function refImagesFromPipelineInputs(
  uiFields: OfficialUiField[] | unknown,
  inputs: Record<string, unknown>,
): { uid: string; name: string; url: string }[] {
  let fields: OfficialUiField[];
  try {
    fields = normalizeOfficialUiFields(uiFields);
  } catch {
    return [];
  }

  const imageField = fields.find((field) => field.widget === 'image-url-list');
  if (!imageField) return [];

  const raw = inputs[imageField.name];
  const urls = Array.isArray(raw)
    ? raw.map((item) => String(item ?? '').trim()).filter(Boolean)
    : typeof raw === 'string' && raw.trim()
      ? [raw.trim()]
      : [];

  return urls.map((url, index) => ({
    uid: `restore-${imageField.name}-${index}`,
    name: url,
    url,
  }));
}

/** schema fields 或内置 FieldSpec 通用回填 */
export function fieldOverridesFromInputs(
  spec: Pick<ExecutionSpec, 'fields'>,
  schemaFields: OfficialUiField[] | undefined,
  inputs: Record<string, unknown>,
): FieldValues {
  if (schemaFields?.length) {
    return fieldOverridesFromPipelineInputs(schemaFields, inputs);
  }

  const overrides: FieldValues = {};
  for (const field of spec.fields ?? []) {
    if (!field.key || !(field.key in inputs)) continue;
    const value = valueFromTemplateArg(inputs[field.key], field.control);
    if (!isBlankFieldValue(value)) {
      overrides[field.key] = value as FieldValue;
    }
  }
  return overrides;
}
