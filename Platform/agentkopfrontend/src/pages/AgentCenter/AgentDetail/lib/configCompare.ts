/**
 * 详情页配置对比：编辑态 vs 已保存 baseline / 历史会话快照
 */
import type { ModelCatalog, Session } from '@/pages/AgentPlayground/types';
import type { AgentDetailData } from '../../types';
import { pickDefaultChatModel } from '@/utils/models';
import { pruneToolDefaults, type ToolDefaultsMap } from './toolDefaultsUtils';

/** 详情页可编辑字段快照 */
export interface AgentDetailEditorSnapshot {
  system: string;
  defaultModel: string;
  tools: string[];
  toolDefaults: ToolDefaultsMap;
}

/** 从详情视图 + 模型目录推导 baseline（含 defaultModel 自动补齐规则） */
export function buildAgentDetailBaseline(
  detail: AgentDetailData,
  modelCatalog: ModelCatalog | null | undefined,
): AgentDetailEditorSnapshot {
  const tools = [...detail.tools];
  const rawDefaults = (detail.rawProfile.toolDefaults ?? {}) as ToolDefaultsMap;
  return {
    system: detail.system ?? '',
    defaultModel: detail.defaultModel || pickDefaultChatModel(modelCatalog) || '',
    tools,
    toolDefaults: pruneToolDefaults(rawDefaults, tools) ?? {},
  };
}

/** 当前编辑态是否与 baseline 存在实质差异 */
export function isAgentDetailDirty(
  editor: AgentDetailEditorSnapshot,
  baseline: AgentDetailEditorSnapshot,
): boolean {
  if (normalizeText(editor.system) !== normalizeText(baseline.system)) return true;
  if (editor.defaultModel.trim() !== baseline.defaultModel.trim()) return true;
  if (sortedKey(editor.tools) !== sortedKey(baseline.tools)) return true;
  const editorDefaults = pruneToolDefaults(editor.toolDefaults, editor.tools) ?? {};
  return stableJson(editorDefaults) !== stableJson(baseline.toolDefaults);
}

function stableJson(value: ToolDefaultsMap): string {
  const normalized: ToolDefaultsMap = {};
  for (const key of Object.keys(value).sort()) {
    normalized[key] = value[key];
  }
  return JSON.stringify(normalized);
}

/** 是否具备可拉取 events 的 agentk 会话 ID（ses_ 前缀） */
export function hasAgentkLedger(session: Session): boolean {
  return typeof session.id === 'string' && session.id.startsWith('ses_');
}

function normalizeText(value: string): string {
  return value.replace(/\r\n/g, '\n').trim();
}

function sortedKey(tools: string[]): string {
  return [...tools]
    .map((t) => t.trim())
    .filter(Boolean)
    .sort()
    .join('\0');
}
