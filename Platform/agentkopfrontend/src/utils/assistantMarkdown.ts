/**
 * 兼容历史数据：部分四字节 Emoji 在旧存储链路中被降级成 `?`，
 * 仅在 Markdown 标题、带冒号的语义列表等明确上下文中恢复，避免误改正文问号。
 */
const LEGACY_HEADING_ICONS: Record<string, string> = {
  介绍视频: '🎬',
  成片交付: '🎬',
  成片: '🎞️',
  分镜内容: '🎞️',
  成片评价: '✅',
  视频参数: '⚙️',
  内容概要: '📝',
  脚本回顾: '📋',
  质量细则: '✅',
  质检细则: '✅',
};

/** 未登记的历史标题也不能把损坏的 Emoji 显示成问号。 */
const LEGACY_HEADING_FALLBACK_ICON = '◆';

/** 列表型内容中的常见语义标签；按关键词匹配，覆盖「画幅比例」等组合标签。 */
const LEGACY_LIST_LABEL_ICONS: ReadonlyArray<readonly [string, string]> = [
  ['风格', '🎨'],
  ['机甲', '🤖'],
  ['场景', '🏞️'],
  ['画幅', '🖼️'],
  ['比例', '📐'],
  ['尺寸', '📐'],
  ['主题', '🎯'],
  ['角色', '👤'],
  ['构图', '🧭'],
  ['镜头', '🎥'],
  ['运镜', '🎥'],
  ['光影', '☀️'],
  ['色彩', '🎨'],
  ['氛围', '✨'],
  ['时长', '⏱️'],
  ['声音', '🔊'],
  ['音乐', '🎵'],
  ['输出', '📦'],
  ['质量', '✅'],
];

const CIRCLED_NUMBERS = [
  '',
  '①',
  '②',
  '③',
  '④',
  '⑤',
  '⑥',
  '⑦',
  '⑧',
  '⑨',
  '⑩',
  '⑪',
  '⑫',
  '⑬',
  '⑭',
  '⑮',
  '⑯',
  '⑰',
  '⑱',
  '⑲',
  '⑳',
] as const;

export const MAX_DEBUG_TEXT_LENGTH = 20_000;

export function truncateDebugText(text: string, maxLength = MAX_DEBUG_TEXT_LENGTH): string {
  if (text.length <= maxLength) return text;
  return `${text.slice(0, maxLength)}\n\n…内容过长，已截断 ${text.length - maxLength} 个字符`;
}

/** Markdown 外链仅允许网页协议和站内相对地址。 */
export function safeMarkdownUrl(url: string): string {
  const trimmed = url.trim();
  if (
    trimmed.startsWith('/') ||
    trimmed.startsWith('./') ||
    trimmed.startsWith('../') ||
    trimmed.startsWith('#')
  ) {
    return trimmed;
  }
  try {
    const protocol = new URL(trimmed).protocol;
    return protocol === 'http:' || protocol === 'https:' ? trimmed : '';
  } catch {
    return '';
  }
}

export function restoreLegacyHeadingIcons(text: string): string {
  const headings = text.replace(
    /^(#{1,6})\s+\?\s+([^\n]+?)\s*$/gm,
    (_original, heading: string, title: string) => {
      const normalizedTitle = title.trim();
      const icon = LEGACY_HEADING_ICONS[normalizedTitle] ?? LEGACY_HEADING_FALLBACK_ICON;
      return `${heading} ${icon} ${normalizedTitle}`;
    },
  );
  const listLabels = headings.replace(
    /^(\s*(?:[-*+]|\d+[.)])\s+)\?\s+([^\n]+)$/gm,
    (original, prefix: string, content: string) => {
      const plainContent = content.replace(/^(?:\*\*|__)\s*/, '');
      const colonIndex = plainContent.search(/[：:]/);
      if (colonIndex < 1) return original;

      const label = plainContent
        .slice(0, colonIndex)
        .replace(/(?:\*\*|__)\s*$/, '')
        .trim();
      if (!label || label.length > 12) return original;

      const icon =
        LEGACY_LIST_LABEL_ICONS.find(([keyword]) => label.includes(keyword))?.[1] ??
        LEGACY_HEADING_FALLBACK_ICON;
      return `${prefix}${icon} ${content}`;
    },
  );
  const tableSequences = listLabels.replace(
    /(\|\s*)\?(\d{1,2})(\s*\|)/g,
    (original, prefix: string, rawNumber: string, suffix: string) => {
      const marker = CIRCLED_NUMBERS[Number(rawNumber)];
      return marker ? `${prefix}${marker}${suffix}` : original;
    },
  );
  return tableSequences.replace(/←\s*\?\s*(决策点\s*\d+)/g, '← ◆ $1');
}
