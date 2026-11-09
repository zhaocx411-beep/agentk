/**
 * 工具广场视图映射
 */
import { matchToolName, normalizeKeyword, uniqueByName } from '@/utils/text';
import { parseEnumParam } from '@/utils/url';
import { TOOL_GALLERY_VIEWS } from './constants';
import type {
  OfficialToolCardData,
  OfficialToolItem,
  OfficialToolParameter,
  ToolGalleryView,
} from './types';

export type { OfficialToolParameter };

/** URL 的 view 参数 → 工具广场视图，缺省为推荐 */
export function parseToolGalleryView(value: string | null): ToolGalleryView {
  return parseEnumParam(value, TOOL_GALLERY_VIEWS, 'recommend');
}

/** 清洗 parameters，剔除脏字段 */
function normalizeParameters(raw: OfficialToolItem['parameters']): OfficialToolParameter[] {
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((item): item is OfficialToolParameter => !!item && typeof item.name === 'string')
    .map((item) => ({
      name: item.name,
      type: typeof item.type === 'string' ? item.type : 'string',
      required: Boolean(item.required),
      description: typeof item.description === 'string' ? item.description : undefined,
      defaultValue: item.defaultValue,
      enumValues: Array.isArray(item.enumValues) ? item.enumValues : undefined,
      children: normalizeParameters(item.children),
    }));
}

/** 接口工具项 → 广场卡片（不做前端 title/category 映射） */
export function toToolCard(item: OfficialToolItem): OfficialToolCardData {
  return {
    key: item.name,
    name: item.name,
    kind: item.kind?.trim() || 'unknown',
    capability: resolveCapabilityId(item),
    description: item.description?.trim() || '',
    modelSelectable: Boolean(item.modelSelectable),
    parameters: normalizeParameters(item.parameters),
  };
}

/** tool ref / capability → capabilityId */
export function resolveCapabilityId(
  tool: Pick<OfficialToolItem, 'name' | 'ref' | 'capability'>,
): string {
  const capability = tool.capability?.trim();
  if (capability) return capability;
  const ref = tool.ref?.trim();
  if (ref?.startsWith('tool:')) return ref.slice('tool:'.length);
  if (ref) return ref;
  return `agentk.${tool.name}`;
}

/** 按工具名去重后，再按名称前缀过滤 */
export function filterToolCards(
  items: OfficialToolCardData[],
  keyword: string,
): OfficialToolCardData[] {
  const unique = uniqueByName(items);
  const q = normalizeKeyword(keyword);
  if (!q) return unique;
  return unique.filter((item) => matchToolName(item.name, q));
}

/** 按 kind 分组，保持首次出现顺序 */
export function groupToolsByKind(items: OfficialToolCardData[]) {
  const groups: Array<{ kind: string; items: OfficialToolCardData[] }> = [];
  const indexMap = new Map<string, number>();
  for (const item of items) {
    const existing = indexMap.get(item.kind);
    if (existing == null) {
      indexMap.set(item.kind, groups.length);
      groups.push({ kind: item.kind, items: [item] });
      continue;
    }
    groups[existing].items.push(item);
  }
  return groups;
}

/** 工具详情页路径 */
export function buildToolDetailPath(toolName: string): string {
  return `/tools/${encodeURIComponent(toolName)}`;
}
