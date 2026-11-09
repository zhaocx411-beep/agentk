/**
 * 官方工具 parameters → 表单值 / profile.toolDefaults 互转
 */
import type { OfficialToolParameter } from '@/types/officialTools';
import type { BaseProfile } from '@/pages/AgentPlayground/types';
import { DOCUMENT_UPLOAD_ACCEPT, PLATFORM_MEDIA_UPLOAD_ACCEPT } from '@/services/media';
import { inferModelOptionsFrom } from '@/utils/modelField';

export type ToolDefaultsMap = NonNullable<BaseProfile['toolDefaults']>;
export type ParamFormValues = Record<string, unknown>;
export type ToolMediaKind = 'image' | 'video' | 'audio';

function isComplexType(type: string): boolean {
  return type === 'object' || (type.startsWith('array<') && type !== 'array<string>');
}

function isArrayString(type: string): boolean {
  return type === 'array<string>' || type === 'array';
}

export function initParamFormValues(
  parameters: OfficialToolParameter[] | undefined,
  existing?: Record<string, unknown>,
): ParamFormValues {
  const values: ParamFormValues = {};
  for (const param of parameters ?? []) {
    if (existing && param.name in existing) {
      values[param.name] = existing[param.name];
      continue;
    }
    if (param.defaultValue !== undefined && param.defaultValue !== null) {
      values[param.name] = param.defaultValue;
      continue;
    }
    if (param.type === 'boolean') {
      values[param.name] = undefined;
      continue;
    }
    if (isSchemaObjectArrayParam(param)) {
      values[param.name] = [];
      continue;
    }
    if (isSchemaObjectParam(param)) {
      values[param.name] = createEmptyObjectItem(param.children ?? []);
      continue;
    }
    if (isArrayString(param.type) || isComplexType(param.type)) {
      values[param.name] = '';
      continue;
    }
    values[param.name] = undefined;
  }
  return values;
}

function tryParseJson(raw: string): unknown | undefined {
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return undefined;
  }
}

/**
 * array / array<string>：先整段当 JSON 解析。
 * process_video.operations 等字段 schema 标成 array<string>，实际要对象数组；
 * 按行/逗号拆会把 pretty JSON 拆碎。
 */
function parseArrayField(raw: unknown): unknown[] | undefined {
  if (Array.isArray(raw)) return raw.length > 0 ? raw : undefined;
  if (typeof raw !== 'string') return undefined;
  const trimmed = raw.trim();
  if (!trimmed) return undefined;

  const looksJson = trimmed.startsWith('{') || trimmed.startsWith('[');
  const parsed = tryParseJson(trimmed);
  if (parsed !== undefined) {
    if (Array.isArray(parsed)) return parsed.length > 0 ? parsed : undefined;
    if (parsed && typeof parsed === 'object') return [parsed];
    if (typeof parsed === 'string' && parsed.trim()) return [parsed];
    if (typeof parsed === 'number' || typeof parsed === 'boolean') return [parsed];
  }
  if (looksJson) return undefined;

  const list = trimmed
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
  return list.length > 0 ? list : undefined;
}

function formatArrayFieldForInput(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value === 'string') return value;
  if (!Array.isArray(value)) return String(value);
  if (value.every((item) => typeof item === 'string')) return value.join('\n');
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}

function arrayFieldAsStrings(raw: unknown): string[] {
  return (parseArrayField(raw) ?? []).filter(
    (item): item is string => typeof item === 'string' && !!item.trim(),
  );
}

function parseComplex(raw: unknown): unknown {
  if (raw === undefined || raw === null || raw === '') return undefined;
  if (typeof raw !== 'string') return raw;
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return undefined;
  }
}

function coerceScalar(type: string, raw: unknown): unknown {
  if (raw === undefined || raw === null || raw === '') return undefined;
  if (type === 'boolean') return Boolean(raw);
  if (type === 'integer') {
    const n = typeof raw === 'number' ? raw : Number(raw);
    return Number.isFinite(n) ? Math.trunc(n) : undefined;
  }
  if (type === 'number') {
    const n = typeof raw === 'number' ? raw : Number(raw);
    return Number.isFinite(n) ? n : undefined;
  }
  if (typeof raw === 'string' || typeof raw === 'number' || typeof raw === 'boolean') {
    return raw;
  }
  return undefined;
}

/** 表单值 → 写入 toolDefaults 的对象（空值不写入） */
function pruneObjectFields(item: Record<string, unknown>): Record<string, unknown> {
  const next: Record<string, unknown> = {};
  for (const [key, val] of Object.entries(item)) {
    if (val === undefined || val === null) continue;
    if (typeof val === 'string' && !val.trim()) continue;
    next[key] = val;
  }
  return next;
}

function isObjectItemComplete(
  item: Record<string, unknown>,
  children: OfficialToolParameter[],
): boolean {
  return children.every(
    (child) => !child.required || !isChildValueEmpty(child, item[child.name]),
  );
}

export function collectToolDefaults(
  parameters: OfficialToolParameter[] | undefined,
  values: ParamFormValues,
): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const param of parameters ?? []) {
    const raw = values[param.name];
    let parsed: unknown;
    if (isSchemaObjectArrayParam(param)) {
      parsed = parseObjectArrayValue(raw)
        .map((item) => pruneObjectFields(item))
        .filter((item) => isObjectItemComplete(item, param.children ?? []));
    } else if (isSchemaObjectParam(param)) {
      const objectValue = pruneObjectFields(parseObjectValue(raw));
      parsed = isObjectItemComplete(objectValue, param.children ?? [])
        ? objectValue
        : undefined;
    } else if (isArrayString(param.type)) {
      parsed = parseArrayField(raw);
    } else if (isComplexType(param.type)) {
      parsed = parseComplex(raw);
    } else {
      parsed = coerceScalar(param.type, raw);
    }
    if (parsed === undefined) continue;
    if (typeof parsed === 'string' && !parsed.trim()) continue;
    if (Array.isArray(parsed) && parsed.length === 0) continue;
    result[param.name] = parsed;
  }
  return result;
}

/** 接口 args → 表单控件值（历史记录回显） */
export function argsToParamFormValues(
  parameters: OfficialToolParameter[] | undefined,
  args: Record<string, unknown>,
): ParamFormValues {
  const values = initParamFormValues(parameters);
  for (const param of parameters ?? []) {
    if (!(param.name in args)) continue;
    const raw = args[param.name];
    if (raw === undefined || raw === null) continue;

    if (isSchemaObjectArrayParam(param)) {
      values[param.name] = parseObjectArrayValue(raw);
      continue;
    }
    if (isSchemaObjectParam(param)) {
      values[param.name] = parseObjectValue(raw);
      continue;
    }
    if (isArrayString(param.type)) {
      values[param.name] = formatArrayFieldForInput(raw);
      continue;
    }
    if (isComplexType(param.type)) {
      values[param.name] = typeof raw === 'string' ? raw : JSON.stringify(raw, null, 2);
      continue;
    }
    values[param.name] = raw;
  }
  return values;
}

/** 展示用：复杂类型序列化为文本 */
export function formatParamForInput(param: OfficialToolParameter, value: unknown): string {
  if (value === undefined || value === null) return '';
  if (isArrayString(param.type) && Array.isArray(value)) {
    return formatArrayFieldForInput(value);
  }
  if (isComplexType(param.type) && typeof value !== 'string') {
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return '';
    }
  }
  return String(value);
}

export function pruneToolDefaults(
  defaults: ToolDefaultsMap,
  tools: readonly string[],
): ToolDefaultsMap | undefined {
  const next: ToolDefaultsMap = {};
  for (const name of tools) {
    const row = defaults[name];
    if (row && Object.keys(row).length > 0) next[name] = row;
  }
  return Object.keys(next).length > 0 ? next : undefined;
}

export function isComplexParam(param: OfficialToolParameter): boolean {
  return isComplexType(param.type);
}

export function isArrayStringParam(param: OfficialToolParameter): boolean {
  return isArrayString(param.type);
}

/** array<object> 且声明了 children 字段 schema */
export function isSchemaObjectArrayParam(param: OfficialToolParameter): boolean {
  const type = param.type.trim().toLowerCase();
  return (
    type.startsWith('array<') &&
    type !== 'array<string>' &&
    type !== 'array' &&
    (param.children?.length ?? 0) > 0
  );
}

/** object 且声明了 children 字段 schema */
export function isSchemaObjectParam(param: OfficialToolParameter): boolean {
  return param.type === 'object' && (param.children?.length ?? 0) > 0;
}

export function createEmptyObjectItem(
  children: OfficialToolParameter[],
): Record<string, unknown> {
  const item: Record<string, unknown> = {};
  for (const child of children) {
    if (child.defaultValue !== undefined && child.defaultValue !== null) {
      item[child.name] = child.defaultValue;
    }
  }
  return item;
}

export function parseObjectArrayValue(raw: unknown): Record<string, unknown>[] {
  if (Array.isArray(raw)) {
    return raw.filter(
      (item): item is Record<string, unknown> =>
        !!item && typeof item === 'object' && !Array.isArray(item),
    );
  }
  if (typeof raw === 'string' && raw.trim()) {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (Array.isArray(parsed)) {
        return parsed.filter(
          (item): item is Record<string, unknown> =>
            !!item && typeof item === 'object' && !Array.isArray(item),
        );
      }
    } catch {
      return [];
    }
  }
  return [];
}

export function isChildValueEmpty(_param: OfficialToolParameter, raw: unknown): boolean {
  if (raw === undefined || raw === null) return true;
  if (typeof raw === 'string' && !raw.trim()) return true;
  if (Array.isArray(raw) && raw.length === 0) return true;
  return false;
}

/** 可选 array<object> 中尚未填写任何字段的空行 */
export function isObjectItemDraft(
  item: Record<string, unknown>,
  children: OfficialToolParameter[],
): boolean {
  return children.every((child) => isChildValueEmpty(child, item[child.name]));
}

export function parseObjectValue(raw: unknown): Record<string, unknown> {
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    return raw as Record<string, unknown>;
  }
  if (typeof raw === 'string' && raw.trim()) {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      return {};
    }
  }
  return {};
}

function isToolPromptField(name: string): boolean {
  const lower = name.toLowerCase();
  return lower.endsWith('_prompt') || lower.includes('prompt');
}

/** 参数名是否像素材 URL 字段（API 不单独标注 media 类型，靠命名约定推断） */
function isMediaParamName(name: string): boolean {
  if (inferModelOptionsFrom(name)) return false;
  const lower = name.toLowerCase();
  if (/url|image|video|audio|media|track|file|cover|refer|clip|asset|素材/.test(lower)) return true;
  if (lower === 'images' || /^(first|last)?frame$/.test(lower) || lower === 'featurevideo') return true;
  return false;
}

/** 参数描述是否表明需要素材 URL（如 firstFrame 的「首帧图片 URL」） */
function isMediaParamFromDescription(description: string): boolean {
  if (!description.trim()) return false;
  const desc = description.toLowerCase();
  if (/artifact\s*url|artifact\s*\/\s*bs|http\/https|公网.{0,16}url|可读.{0,16}url/.test(desc)) return true;
  if (/图片\s*url|视频\s*url|音频\s*url|素材\s*url|字幕\s*url|文件\s*url/.test(desc)) return true;
  if (/首帧|尾帧|参考图|参考视频|参考音频|受管.*(图片|视频|音频|artifact)/.test(desc)) return true;
  if (/可下载.{0,8}(视频|音频|图片)|视频\/图片\s*url/.test(desc)) return true;
  return false;
}

/** 网页链接、检索词等非素材 URL，不应展示上传组件 */
function isNonMediaUrlParam(name: string, description: string): boolean {
  const lower = name.toLowerCase();
  if (lower === 'producturl' || lower === 'product_url') return true;
  if (lower === 'query' || lower === 'sessionid' || lower === 'agent') return true;
  if (/商品详情页|商品页|检索词|网页标题|site:/.test(description)) return true;
  return false;
}

function looksLikeMediaParam(param: OfficialToolParameter): boolean {
  if (isToolPromptField(param.name)) return false;
  if (inferModelOptionsFrom(param.name)) return false;
  const description = param.description || '';
  if (isNonMediaUrlParam(param.name, description)) return false;
  return isMediaParamName(param.name) || isMediaParamFromDescription(description);
}

/** 音频入参描述里写了视频/音轨时，允许选带音轨的视频 */
function audioParamAlsoAllowsVideo(description: string): boolean {
  return /视频|video|音轨|mp4|mov/.test(description.toLowerCase());
}

/** array<object> 内嵌的子字段是否为素材 URL（如 audioTracks[].url） */
export function isMediaUrlChildField(
  child: OfficialToolParameter,
  parentParam?: OfficialToolParameter,
): boolean {
  if (child.type !== 'string') return false;
  if (/^(url|uri|link|src|href)$/i.test(child.name)) {
    if (child.description && isMediaParamFromDescription(child.description)) return true;
    if (parentParam && looksLikeMediaParam(parentParam)) return true;
    return /^(url|uri)$/i.test(child.name);
  }
  const synthetic: OfficialToolParameter = {
    ...child,
    description: child.description ?? parentParam?.description,
  };
  return looksLikeMediaParam(synthetic);
}

/** 是否展示本地上传（URL 列表 / 含 url 的对象数组 / 单 URL 字符串） */
export function supportsLocalMediaUpload(param: OfficialToolParameter): boolean {
  if (isArrayStringParam(param)) return looksLikeMediaParam(param);
  if (param.type.trim().toLowerCase() === 'array<object>') return looksLikeMediaParam(param);
  if (param.type === 'string') return looksLikeMediaParam(param);
  return looksLikeMediaParam(param) && isComplexParam(param);
}

/** 按参数名推断 accept（仅平台允许的扩展名） */
export function acceptForMediaParam(param: OfficialToolParameter): string {
  const kinds = mediaKindsForParam(param);
  if (kinds.length === 0) {
    const name = param.name.toLowerCase();
    if (name.includes('file') || name.includes('document') || name.includes('doc')) {
      return DOCUMENT_UPLOAD_ACCEPT;
    }
    return PLATFORM_MEDIA_UPLOAD_ACCEPT;
  }
  const tokens: string[] = [];
  if (kinds.includes('image')) tokens.push('image/*');
  if (kinds.includes('video')) tokens.push('video/*');
  if (kinds.includes('audio')) tokens.push('audio/*');
  return tokens.join(',') || PLATFORM_MEDIA_UPLOAD_ACCEPT;
}

/** 参数适用的历史素材类型；文档参数当前仅支持本地上传 */
export function mediaKindsForParam(param: OfficialToolParameter): ToolMediaKind[] {
  if (inferModelOptionsFrom(param.name)) return [];

  const name = param.name.toLowerCase();
  const desc = (param.description || '').toLowerCase();
  const type = param.type.trim().toLowerCase();

  if (/array<video>|^video$/.test(type) || (type.includes('video') && !isToolPromptField(param.name))) {
    return ['video'];
  }
  if (/array<audio>|^audio$/.test(type) || type.includes('audio')) {
    return audioParamAlsoAllowsVideo(desc) ? ['audio', 'video'] : ['audio'];
  }
  if (/array<image>|^image$/.test(type) || type.includes('image')) {
    return ['image'];
  }

  // 首尾帧参数名一定是图；描述里写「图片 URL」但同时提视频时不能锁死为图
  if (/^(first|last)?frame$/.test(name)) {
    return ['image'];
  }
  if (/首帧|尾帧|参考图|图片 url|image url/.test(desc) && !/视频|video/.test(desc)) {
    return ['image'];
  }
  if (!isToolPromptField(param.name) && (name.includes('video') || name === 'featurevideo')) {
    return ['video'];
  }
  if (
    name.includes('audio') ||
    name.includes('sound') ||
    name.includes('voice') ||
    name.includes('track') ||
    name.includes('bgm')
  ) {
    return audioParamAlsoAllowsVideo(desc) ? ['audio', 'video'] : ['audio'];
  }
  if (
    name.includes('image') ||
    name.includes('img') ||
    name.includes('cover') ||
    name.includes('refer') ||
    name === 'images'
  ) {
    return ['image'];
  }
  if (name.includes('file') || name.includes('document') || name.includes('doc')) return [];

  const fromDesc: ToolMediaKind[] = [];
  if (/视频|video|mp4|mov|成片|素材视频/.test(desc)) fromDesc.push('video');
  if (/音频|audio|bgm|音乐|配音|配乐/.test(desc)) fromDesc.push('audio');
  if (/图片|图像|image|商品图|封面|参考图|photo|jpg|png/.test(desc)) fromDesc.push('image');
  if (fromDesc.length > 0) return [...new Set(fromDesc)];

  return ['image', 'video', 'audio'];
}

export function mediaKindsHint(kinds: readonly ToolMediaKind[]): string {
  if (kinds.length === 0) return '文件';
  const labels = kinds.map((kind) => {
    if (kind === 'video') return '视频';
    if (kind === 'audio') return '音频';
    return '图片';
  });
  if (labels.length === 2) return labels.join('或');
  return labels.join('、');
}

/** 从当前参数值提取 URL，用于素材选择时去重 */
export function extractMediaUrls(param: OfficialToolParameter, raw: unknown): string[] {
  if (param.type === 'string' && typeof raw === 'string' && raw.trim()) {
    return [raw.trim()];
  }
  if (isArrayStringParam(param)) return arrayFieldAsStrings(raw);
  const parsed = parseComplex(raw);
  if (!Array.isArray(parsed)) return [];
  return parsed.flatMap((item) => {
    if (typeof item === 'string' && item.trim()) return [item.trim()];
    if (!item || typeof item !== 'object' || Array.isArray(item)) return [];
    const url = (item as Record<string, unknown>).url;
    return typeof url === 'string' && url.trim() ? [url.trim()] : [];
  });
}

/** 把上传得到的 URL 追加进 array<string> 文本（每行一个） */
export function appendUrlsToArrayStringText(raw: unknown, urls: string[]): string {
  const existing = parseArrayField(raw) ?? [];
  if (existing.some((item) => typeof item !== 'string')) {
    return formatArrayFieldForInput(existing);
  }
  const merged = existing.filter((item): item is string => typeof item === 'string');
  for (const url of urls) {
    if (url && !merged.includes(url)) merged.push(url);
  }
  return merged.join('\n');
}

/** 素材 URL 列表 → 写入表单值（array<string> 每行一个，对象数组 JSON，string 取首个） */
export function urlsToParamFormValue(
  param: OfficialToolParameter,
  urls: readonly string[],
): unknown {
  const cleaned = urls.map((url) => url.trim()).filter(Boolean);
  if (param.type === 'string') return cleaned[0] ?? '';
  if (isArrayStringParam(param)) {
    return cleaned.join('\n');
  }
  return JSON.stringify(
    cleaned.map((url) => ({ url })),
    null,
    2,
  );
}

/** 把上传得到的 URL 追加进 JSON 数组文本（每项 {url}） */
export function appendUrlsToObjectArrayText(raw: unknown, urls: string[]): string {
  let list: unknown[] = [];
  if (typeof raw === 'string' && raw.trim()) {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (Array.isArray(parsed)) list = parsed;
    } catch {
      list = [];
    }
  } else if (Array.isArray(raw)) {
    list = [...raw];
  }
  const existingUrls = new Set(
    list
      .map((item) =>
        item && typeof item === 'object' && !Array.isArray(item)
          ? String((item as Record<string, unknown>).url ?? '')
          : '',
      )
      .filter(Boolean),
  );
  for (const url of urls) {
    if (!url || existingUrls.has(url)) continue;
    list.push({ url });
    existingUrls.add(url);
  }
  return JSON.stringify(list, null, 2);
}
