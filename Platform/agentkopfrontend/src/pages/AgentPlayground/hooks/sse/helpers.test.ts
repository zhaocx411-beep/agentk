import { describe, expect, it } from 'vitest';
import {
  attachmentsToMessageParts,
  extractUserAttachments,
  resolveMessagePartMediaKind,
} from './helpers';

describe('SSE user attachment parsing', () => {
  it('parses V3 attachment fields from user.input events', () => {
    expect(
      extractUserAttachments({
        text: '看看这张图',
        attachments: [
          {
            type: 'image',
            uri: 'https://example.com/reference.png',
            mimeType: 'image/png',
          },
        ],
      }),
    ).toEqual([
      {
        url: 'https://example.com/reference.png',
        mediaType: 'image',
        contentType: 'image/png',
      },
    ]);
  });

  it('preserves audio metadata for URLs without a file extension', () => {
    const parts = attachmentsToMessageParts([
      {
        url: 'https://example.com/media/download?id=audio_1',
        mediaType: 'audio',
        contentType: 'audio/mpeg',
      },
    ]);

    expect(parts).toEqual([
      {
        type: 'file',
        url: 'https://example.com/media/download?id=audio_1',
        name: undefined,
        mediaType: 'audio',
        contentType: 'audio/mpeg',
      },
    ]);
    expect(
      resolveMessagePartMediaKind(parts[0] as Extract<(typeof parts)[number], { type: 'file' }>),
    ).toBe('audio');
  });
});
