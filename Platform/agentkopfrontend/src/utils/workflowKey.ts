/**
 * 官方 / 平台 workflowKey 归一化
 * 目录与 URL 可能是 generate_brand_ad / agentk.xxx / pipeline:agentk.xxx
 */

/** 去掉 pipeline: 前缀 */
export function stripPipelinePrefix(key: string): string {
  const trimmed = key.trim();
  return trimmed.startsWith('pipeline:') ? trimmed.slice('pipeline:'.length) : trimmed;
}

/**
 * 取短名：pipeline:agentk.generate_brand_ad → generate_brand_ad
 * 用于内置增强、模版 specs、品牌广告默认值等
 */
export function bareWorkflowKey(key: string): string {
  const stripped = stripPipelinePrefix(key);
  if (!stripped) return '';
  const dot = stripped.lastIndexOf('.');
  return dot >= 0 ? stripped.slice(dot + 1) : stripped;
}

/** 两种写法是否指向同一工作流 */
export function workflowKeyMatches(left: string, right: string): boolean {
  const a = left.trim();
  const b = right.trim();
  if (!a || !b) return false;
  if (a === b) return true;
  if (stripPipelinePrefix(a) === stripPipelinePrefix(b)) return true;
  return bareWorkflowKey(a) === bareWorkflowKey(b);
}
