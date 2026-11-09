import { describe, expect, it } from 'vitest';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { supportsStructuredMessageAttachments } from './structuredAttachments';

function officialItem(
  name: string,
  variant: OfficialCatalogCardData['variant'] = 'planning',
): OfficialCatalogCardData {
  return {
    key: 'test',
    name,
    description: '',
    subtitle: '官方示范',
    skills: [],
    variant,
  };
}

describe('supportsStructuredMessageAttachments', () => {
  it('enables V3 attachments only for the official Gemini multimodal Agent', () => {
    expect(supportsStructuredMessageAttachments(officialItem('Gemini 多模态创作助手'))).toBe(true);
    expect(supportsStructuredMessageAttachments(officialItem('其他自主规划 Agent'))).toBe(false);
    expect(
      supportsStructuredMessageAttachments(officialItem('Gemini 多模态创作助手', 'workflow')),
    ).toBe(false);
    expect(supportsStructuredMessageAttachments(null)).toBe(false);
  });
});
