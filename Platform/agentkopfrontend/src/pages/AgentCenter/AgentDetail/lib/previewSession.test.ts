import { describe, expect, it } from 'vitest';
import type { BaseProfile } from '@/pages/AgentPlayground/types';
import {
  areProfilesEquivalent,
  shouldStartNewPreviewSession,
} from './previewSession';

const SYSTEM_V1 = '你是“虚拟试穿”助手。\n缺少商品图时，不要继续生成。';
const CURRENT_V1: BaseProfile = {
  system: SYSTEM_V1,
  defaultModel: 'deepseek-v4-pro',
  tools: [
    { name: 'understand_media', ref: 'tool:agentk.understand_media' },
    { name: 'generate_image', ref: 'tool:agentk.generate_image' },
  ],
  toolDefaults: {
    understand_media: { model: 'gemini-3.1-flash-lite' },
    generate_image: { aspectRatio: '9:16' },
  },
};
const HISTORY_V1 = {
  system: SYSTEM_V1,
  defaultModel: 'deepseek-v4-pro',
  tools: [
    { name: 'understand_media', capability: 'agentk.understand_media' },
    { name: 'generate_image', capability: 'agentk.generate_image' },
  ],
  toolDefaults: {
    generate_image: { aspectRatio: '9:16' },
    understand_media: { model: 'gemini-3.1-flash-lite' },
  },
};

describe('preview session reuse', () => {
  it('treats backend capability tools and frontend ref tools as the same profile', () => {
    expect(areProfilesEquivalent(CURRENT_V1, HISTORY_V1)).toBe(true);
    expect(
      shouldStartNewPreviewSession({
        sessionId: 'ses_history',
        dirty: false,
        currentProfile: CURRENT_V1,
        sessionProfile: HISTORY_V1,
        sessionVersionId: 41,
        currentVersionId: 42,
      }),
    ).toBe(false);
  });

  it('starts a new session when the current full profile differs', () => {
    const currentV2: BaseProfile = {
      ...CURRENT_V1,
      system: SYSTEM_V1.slice(0, -1),
    };

    expect(areProfilesEquivalent(currentV2, HISTORY_V1)).toBe(false);
    expect(
      shouldStartNewPreviewSession({
        sessionId: 'ses_v1',
        dirty: false,
        currentProfile: currentV2,
        sessionProfile: HISTORY_V1,
        sessionVersionId: 41,
        currentVersionId: 42,
      }),
    ).toBe(true);
  });

  it('reuses the original session after switching the page profile back', () => {
    expect(
      shouldStartNewPreviewSession({
        sessionId: 'ses_v1',
        dirty: false,
        currentProfile: CURRENT_V1,
        sessionProfile: HISTORY_V1,
        sessionVersionId: 41,
        currentVersionId: 41,
      }),
    ).toBe(false);
  });

  it('starts a new session without a session or after an actual editor change', () => {
    expect(
      shouldStartNewPreviewSession({
        sessionId: null,
        dirty: false,
        currentProfile: CURRENT_V1,
      }),
    ).toBe(true);
    expect(
      shouldStartNewPreviewSession({
        sessionId: 'ses_history',
        dirty: true,
        currentProfile: CURRENT_V1,
        sessionProfile: HISTORY_V1,
      }),
    ).toBe(true);
  });

  it('falls back to version IDs only when the historical profile is unavailable', () => {
    expect(
      shouldStartNewPreviewSession({
        sessionId: 'ses_history',
        dirty: false,
        currentProfile: CURRENT_V1,
        sessionVersionId: 41,
        currentVersionId: 42,
      }),
    ).toBe(true);
  });
});
