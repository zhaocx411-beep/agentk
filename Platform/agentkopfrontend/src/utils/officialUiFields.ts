/**
 * 官方目录 uiSchema / fields 清洗
 * 脏数据、未知 widget、缺 name 等一律降级或丢弃，保证渲染侧不抛错
 */
import type {
  OfficialFieldWidget,
  OfficialUiField,
  OfficialWorkflowItem,
  OfficialWorkflowUiSchemaData,
  PlatformCapabilityItem,
} from '@/types/officialCatalog';

/** 前端已实现的 widget；其余一律降级 */
const KNOWN_WIDGETS = new Set<string>([
  'input',
  'textarea',
  'number',
  'image-url-list',
  'array',
  'json-editor',
  'select',
  'json',
]);

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function asTrimmedString(value: unknown): string {
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

/** 清洗单条后端字段：缺 name / 非法结构直接丢弃；未知 widget 降级 */
export function normalizeOfficialUiField(raw: unknown, index = 0): OfficialUiField | null {
  if (!isPlainObject(raw)) return null;

  const name = asTrimmedString(raw.name) || asTrimmedString(raw.key);
  if (!name) return null;

  // 避免极端超长 key 搞坏 React / 请求体
  const safeName = name.slice(0, 128);
  const type = asTrimmedString(raw.type) || 'string';
  let widget = asTrimmedString(raw.widget).toLowerCase() || 'input';

  if (widget === 'text') widget = 'input';
  if (widget === 'json') widget = 'json-editor';
  const hasEnum =
    (isPlainObject(raw.schema) && Array.isArray(raw.schema.enum) && raw.schema.enum.length > 0) ||
    (Array.isArray(raw.enum) && raw.enum.length > 0);
  if ((widget === 'select' || widget === 'enum') && !hasEnum) widget = 'input';

  if (!KNOWN_WIDGETS.has(widget)) {
    widget = type === 'object' || type === 'array' ? 'json-editor' : 'input';
  }

  let items: OfficialUiField | undefined;
  if (isPlainObject(raw.items)) {
    const nested = normalizeOfficialUiField(raw.items, index);
    if (nested) items = nested;
  }

  let schema = isPlainObject(raw.schema) ? { ...raw.schema } : undefined;
  if (Array.isArray(raw.enum) && raw.enum.length > 0) {
    schema = { ...(schema ?? {}), enum: raw.enum };
  }

  return {
    name: safeName,
    label: asTrimmedString(raw.label) || safeName,
    description: asTrimmedString(raw.description) || undefined,
    type,
    widget: widget as OfficialFieldWidget,
    required: Boolean(raw.required),
    schema,
    defaultValue: raw.defaultValue !== undefined ? raw.defaultValue : schema?.default,
    items,
  };
}

/** 清洗字段列表：去无效项、按 name 去重（后者覆盖前者） */
export function normalizeOfficialUiFields(rawFields: unknown): OfficialUiField[] {
  if (!Array.isArray(rawFields)) return [];
  const byName = new Map<string, OfficialUiField>();
  try {
    rawFields.forEach((item, index) => {
      const normalized = normalizeOfficialUiField(item, index);
      if (!normalized) return;
      byName.set(normalized.name, normalized);
    });
  } catch {
    return Array.from(byName.values());
  }
  return Array.from(byName.values());
}

/** 可提取 fields / uiSchema.fields / inputSchema 的来源 */
type OfficialUiFieldsSource =
  | Pick<OfficialWorkflowItem, 'uiSchema' | 'fields'>
  | Pick<OfficialWorkflowUiSchemaData, 'uiSchema' | 'fields' | 'inputSchema'>
  | Pick<PlatformCapabilityItem, 'uiSchema' | 'fields' | 'inputSchema'>
  | null
  | undefined;

/** 无 fields 时从 JSON Schema properties 推导控件（仅作兜底） */
export function fieldsFromInputSchema(
  inputSchema: Record<string, unknown> | undefined,
): OfficialUiField[] {
  if (!isPlainObject(inputSchema)) return [];
  const properties = inputSchema.properties;
  if (!isPlainObject(properties)) return [];

  const required = new Set(
    Array.isArray(inputSchema.required)
      ? inputSchema.required.filter((item): item is string => typeof item === 'string')
      : [],
  );

  const fields: OfficialUiField[] = [];
  for (const [name, propRaw] of Object.entries(properties)) {
    if (!isPlainObject(propRaw)) continue;
    const type = asTrimmedString(propRaw.type) || 'string';
    const description = asTrimmedString(propRaw.description);
    let widget: OfficialFieldWidget = 'input';

    if (type === 'array') {
      const items = isPlainObject(propRaw.items) ? propRaw.items : undefined;
      const itemType = asTrimmedString(items?.type);
      if (itemType === 'string' && /image/i.test(name)) widget = 'image-url-list';
      else if (itemType === 'string') widget = 'array';
      else widget = 'json-editor';
    } else if (type === 'object') {
      widget = 'json-editor';
    } else if (type === 'number' || type === 'integer') {
      widget = 'number';
    } else if (/prompt|描述|诉求/i.test(name) || description.length > 24) {
      widget = 'textarea';
    }

    fields.push({
      name,
      label: description || name,
      description: description || undefined,
      type,
      widget,
      required: required.has(name),
      schema: propRaw,
    });
  }
  return fields;
}

/** 从目录项或 capabilities schema 响应中取出并清洗字段列表 */
export function pickOfficialUiFields(item: OfficialUiFieldsSource): OfficialUiField[] {
  try {
    // 优先 fields（与 inputSchema.required 顺序更一致），uiSchema.fields 作兜底
    if (Array.isArray(item?.fields) && item.fields.length > 0) {
      return normalizeOfficialUiFields(item.fields);
    }
    const fromUi = item?.uiSchema?.fields;
    if (Array.isArray(fromUi) && fromUi.length > 0) return normalizeOfficialUiFields(fromUi);

    if (item && 'inputSchema' in item) {
      return fieldsFromInputSchema(item.inputSchema);
    }
  } catch {
    return [];
  }
  return [];
}
