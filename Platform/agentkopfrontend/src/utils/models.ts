/**
 * 模型目录辅助：从 GET /agentk/models 派生下拉选项与 Profile 字段
 */
import type {
  BaseProfile,
  CatalogModel,
  ModelCatalog,
  ModelKind,
  SendMessageAttachmentType,
} from '@/pages/AgentPlayground/types';
import { inferModelOptionsFrom, type ModelOptionsKind } from '@/utils/modelField';

export { inferModelOptionsFrom, type ModelOptionsKind };

/** 按 kind 取模型条目（保序，去重） */
export function modelsByKind(
  catalog: ModelCatalog | null | undefined,
  kind: ModelKind,
): CatalogModel[] {
  if (!catalog) return [];
  const seen = new Set<string>();
  const fromModels = catalog.models.filter((m) => {
    if (m.kind !== kind || seen.has(m.name)) return false;
    seen.add(m.name);
    return true;
  });
  if (fromModels.length > 0) return fromModels;
  // chat 可回退到 chatModels 列表（仅 name，无额外元数据）
  if (kind === 'chat') {
    return catalog.chatModels
      .filter((name) => {
        if (seen.has(name)) return false;
        seen.add(name);
        return true;
      })
      .map((name) => ({ name, kind: 'chat' as const }));
  }
  return [];
}

/** 对话模型条目（目录 ∪ 已选值，保证可回显） */
export function chatModelItems(
  catalog: ModelCatalog | null | undefined,
  pinned?: string,
): CatalogModel[] {
  const listed = modelsByKind(catalog, 'chat');
  if (!pinned || listed.some((m) => m.name === pinned)) return listed;
  return [{ name: pinned, kind: 'chat' }, ...listed];
}

/** 按 kind 取模型名（保序，去重） */
export function modelNamesByKind(
  catalog: ModelCatalog | null | undefined,
  kind: ModelKind,
): string[] {
  return modelsByKind(catalog, kind).map((m) => m.name);
}

/** 解析对话主模型默认值：优先 preferred，否则目录首个 chat 模型 */
export function pickDefaultChatModel(
  catalog: ModelCatalog | null | undefined,
  preferred?: string | null,
): string {
  const chat = catalog?.chatModels ?? [];
  if (preferred && chat.includes(preferred)) return preferred;
  if (preferred) return preferred;
  return chat[0] ?? '';
}

/** 对话模型选项（目录 ∪ 已选值，保证可回显） */
export function chatModelOptions(
  catalog: ModelCatalog | null | undefined,
  pinned?: string,
): string[] {
  return chatModelItems(catalog, pinned).map((m) => m.name);
}

/** 根据当前对话模型返回可走消息 V3 attachments 的媒体类型；未知模型默认旧链路。 */
export function structuredAttachmentTypesForModel(
  catalog: ModelCatalog | null | undefined,
  modelName: string | null | undefined,
): SendMessageAttachmentType[] {
  if (!catalog || !modelName) return [];
  const capabilities = catalog.models.find(
    (model) => model.kind === 'chat' && model.name === modelName,
  )?.attachments;
  if (!capabilities) return [];

  const types: SendMessageAttachmentType[] = [];
  if (capabilities.image) types.push('image');
  if (capabilities.video) types.push('video');
  if (capabilities.audio) types.push('audio');
  return types;
}

/** 某工具的候选模型列表（目录 ∪ 已选值，保证可回显） */
export function toolModelOptions(
  catalog: ModelCatalog | null | undefined,
  toolName: string,
  pinned?: string,
): string[] {
  const listed = catalog?.toolModels?.[toolName] ?? [];
  return Array.from(new Set([...listed, ...(pinned ? [pinned] : [])]));
}

/**
 * 把「工具 → 选中模型」写成 Profile.toolDefaults.<tool>.model
 * 只写入已启用且确实选了模型的工具
 */
export function toToolModelDefaults(
  selectedTools: readonly string[],
  selectedToolModels: Record<string, string>,
  base?: BaseProfile['toolDefaults'],
): BaseProfile['toolDefaults'] | undefined {
  const next: Record<string, Record<string, unknown>> = { ...(base ?? {}) };
  for (const tool of selectedTools) {
    const model = selectedToolModels[tool];
    if (!model) continue;
    next[tool] = { ...(next[tool] ?? {}), model };
  }
  return Object.keys(next).length > 0 ? next : undefined;
}

/** Select options 快捷构造 */
export function toSelectOptions(
  names: readonly string[],
  defaultName?: string,
): { label: string; value: string }[] {
  return names.map((name) => ({
    value: name,
    label: defaultName && name === defaultName ? `${name}（默认）` : name,
  }));
}

/**
 * 模型字段下拉：优先 /agentk/models；目录为空时回退 schema enum
 */
export function selectOptionsForModelField(
  catalog: ModelCatalog | null | undefined,
  optionsFrom: ModelOptionsKind,
  fallback?: readonly { label: string; value: string | number }[],
): { label: string; value: string }[] {
  const names = modelNamesByKind(catalog, optionsFrom);
  if (names.length > 0) return toSelectOptions(names);
  if (!fallback?.length) return [];
  return fallback.map((item) => ({ label: item.label, value: String(item.value) }));
}
