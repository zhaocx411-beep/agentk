import type { OfficialCatalogCardData } from '@/types/officialCatalog';

export const GEMINI_MULTIMODAL_AGENT_NAME = 'Gemini 多模态创作助手';

/** 目前仅官方 Gemini 多模态创作助手支持消息 V3 attachments。 */
export function supportsStructuredMessageAttachments(
  item: OfficialCatalogCardData | null,
): boolean {
  return item?.variant === 'planning' && item.name.trim() === GEMINI_MULTIMODAL_AGENT_NAME;
}
