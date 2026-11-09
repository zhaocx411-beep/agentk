/**
 * 模型字段名推断（无业务类型依赖，可供 adaptWorkflowInfo 等安全引用）
 */

/** 模型下拉动态选项种类（对齐 /agentk/models） */
export type ModelOptionsKind = 'chat' | 'image' | 'video';

/**
 * 从字段名推断是否为模型选择，以及该走目录里的哪一类
 * 覆盖 chatModel / imageModel / videoModel / model / *Model 等命名
 */
export function inferModelOptionsFrom(fieldName: string): ModelOptionsKind | undefined {
  const key = fieldName.trim();
  if (!key) return undefined;
  const lower = key.toLowerCase().replace(/[_-]/g, '');

  if (lower === 'imagemodel' || lower.endsWith('imagemodel')) return 'image';
  if (lower === 'editmodel' || lower.endsWith('editmodel')) return 'image';
  if (lower === 'videomodel' || lower.endsWith('videomodel')) return 'video';
  if (
    lower === 'model' ||
    lower === 'chatmodel' ||
    lower === 'defaultmodel' ||
    lower.endsWith('chatmodel') ||
    lower.endsWith('defaultmodel')
  ) {
    return 'chat';
  }
  if (lower.endsWith('model')) {
    if (lower.includes('image')) return 'image';
    if (lower.includes('video') || lower.includes('kling')) return 'video';
    return 'chat';
  }
  return undefined;
}
