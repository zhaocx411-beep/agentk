import { describe, expect, it } from 'vitest';
import type { ModelCatalog } from '@/pages/AgentPlayground/types';
import { structuredAttachmentTypesForModel } from './models';

const catalog: ModelCatalog = {
  kconfKey: 'klingai.agentk.models',
  names: ['text-model', 'image-model', 'all-model'],
  chatModels: ['text-model', 'image-model', 'all-model'],
  toolModels: {},
  models: [
    {
      name: 'text-model',
      kind: 'chat',
      attachments: { mode: 'none', image: false, video: false, audio: false },
    },
    {
      name: 'image-model',
      kind: 'chat',
      attachments: { mode: 'image', image: true, video: false, audio: false },
    },
    {
      name: 'all-model',
      kind: 'chat',
      attachments: { mode: 'all', image: true, video: true, audio: true },
    },
  ],
};

describe('structuredAttachmentTypesForModel', () => {
  it('derives V3 attachment types from the selected chat model', () => {
    expect(structuredAttachmentTypesForModel(catalog, 'text-model')).toEqual([]);
    expect(structuredAttachmentTypesForModel(catalog, 'image-model')).toEqual(['image']);
    expect(structuredAttachmentTypesForModel(catalog, 'all-model')).toEqual([
      'image',
      'video',
      'audio',
    ]);
  });

  it('defaults unknown or metadata-free models to the legacy text path', () => {
    expect(structuredAttachmentTypesForModel(catalog, 'unknown-model')).toEqual([]);
    expect(structuredAttachmentTypesForModel(null, 'all-model')).toEqual([]);
  });
});
