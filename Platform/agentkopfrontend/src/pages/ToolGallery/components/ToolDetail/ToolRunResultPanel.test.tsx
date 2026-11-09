/** @vitest-environment jsdom */
import { describe, expect, it } from 'vitest';
import type { CapabilityExecution } from '@/types/capabilityRuns';
import { collectPreviewMedia } from './previewMedia';

describe('collectPreviewMedia', () => {
  it('hides .internal URLs while retaining browser-accessible alternatives', () => {
    const execution: CapabilityExecution = {
      id: 'exe_1',
      sessionId: 'ses_1',
      name: 'capability_run',
      status: 'completed',
      output: {
        artifacts: [
          {
            type: 'video',
            uri: 'http://bs3-hb1.internal/bucket/output.mp4',
          },
        ],
      },
      mediaArtifacts: [
        {
          type: 'video',
          url: 'https://bs3-hb1.corp.kuaishou.com/bucket/output.mp4',
        },
        {
          type: 'video',
          url: 'https://bs3-hb1.staging.kuaishou.com/bucket/input.mp4',
        },
      ],
      videoUrls: [
        'http://bs3-hb1.internal/bucket/output.mp4',
        'https://bs3-hb1.corp.kuaishou.com/bucket/output.mp4',
      ],
    };

    expect(collectPreviewMedia(execution)).toEqual([
      {
        url: 'https://bs3-hb1.corp.kuaishou.com/bucket/output.mp4',
        mediaType: 'video',
      },
      {
        url: 'https://bs3-hb1.staging.kuaishou.com/bucket/input.mp4',
        mediaType: 'video',
      },
    ]);
  });
});
