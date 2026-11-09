/**
 * 创作模版工具：prompt 填充。
 * 可选列表一律来自 datasets/tasks/query?usage=TEMPLATE，无数据时不造假模版。
 */
import type { CreationTemplate, SampleRow } from './types';

/** 内部占位（不展示给用户；仅避免空引用） */
export const EMPTY_CREATION_TEMPLATE: CreationTemplate = {
  id: '',
  specs: [],
  name: '',
  description: '',
  scene: '效果广告',
  tags: [],
  preferredCategories: [],
  prompt: '',
  memory: [],
  refImageLimit: 12,
  stages: [],
};

/** 把样本数据填进模版 prompt，未选样本时保留占位符原文 */
export function fillPrompt(prompt: string, row: SampleRow | null): string {
  if (!row) return prompt;
  const values: Record<string, string> = {
    商品标题: row.title,
    商品价格: row.price,
    商品属性: row.attributes,
    商品属性信息: row.attributes,
    商品链接: row.link,
    商品首图: row.coverUrls[0] ?? '无',
  };
  return prompt.replace(/\{\{([^}]+)\}\}/g, (raw, key: string) => values[key.trim()] ?? raw);
}
