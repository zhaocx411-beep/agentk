import { describe, expect, it } from 'vitest';
import {
  messageAttachmentFromUrl,
  partitionSendMessageAttachments,
  pickHistoryUploadThumbUrl,
  pickUploadMediaUrl,
  SEND_MESSAGE_MAX_ATTACHMENTS,
  validateSendMessageAttachments,
} from './media';

describe('message V3 attachments', () => {
  it('maps supported media and leaves documents on the compatibility path', () => {
    expect(
      partitionSendMessageAttachments([
        {
          url: 'https://example.com/reference.png',
          mediaType: 'image',
          contentType: 'image/png',
        },
        {
          url: 'https://example.com/demo.mp4',
          mediaType: 'video',
        },
        {
          url: 'https://example.com/spec.pdf',
          mediaType: 'doc',
        },
      ]),
    ).toEqual({
      attachments: [
        {
          type: 'image',
          uri: 'https://example.com/reference.png',
          mimeType: 'image/png',
        },
        {
          type: 'video',
          uri: 'https://example.com/demo.mp4',
          mimeType: 'video/mp4',
        },
      ],
      unsupported: [
        {
          url: 'https://example.com/spec.pdf',
          mediaType: 'doc',
        },
      ],
    });
  });

  it('rejects more than ten attachments or more than one video', () => {
    expect(
      validateSendMessageAttachments(
        Array.from({ length: SEND_MESSAGE_MAX_ATTACHMENTS + 1 }, (_, index) => ({
          type: 'image' as const,
          uri: `https://example.com/${index}.png`,
          mimeType: 'image/png',
        })),
      ),
    ).toContain('最多发送 10 个附件');

    expect(
      validateSendMessageAttachments([
        { type: 'video', uri: 'https://example.com/a.mp4', mimeType: 'video/mp4' },
        { type: 'video', uri: 'https://example.com/b.mp4', mimeType: 'video/mp4' },
      ]),
    ).toContain('最多发送 1 个视频');
  });

  it('recognizes audio extensions carried in URL query parameters', () => {
    const url = 'https://down.ear0.com:3321/index/preview?soundid=44034&type=mp3&audio=sound.mp3';
    const attachment = messageAttachmentFromUrl(url, '治愈短视频BGM');

    expect(attachment.mediaType).toBe('audio');
    expect(partitionSendMessageAttachments([attachment])).toEqual({
      attachments: [
        {
          type: 'audio',
          uri: url,
          mimeType: 'audio/mpeg',
        },
      ],
      unsupported: [],
    });
  });
});

describe('uploaded media URL selection', () => {
  it('always prefers blobUrl over public and compatibility URLs', () => {
    expect(
      pickUploadMediaUrl({
        blobUrl: 'https://blob.example.com/file.png',
        cdnUrl: 'https://cdn.example.com/file.png',
        fileUrl: 'https://file.example.com/file.png',
        url: 'https://legacy.example.com/file.png',
      }),
    ).toBe('https://blob.example.com/file.png');
  });

  it('uses blobUrl for history thumbnails too', () => {
    expect(
      pickHistoryUploadThumbUrl({
        blobUrl: 'https://blob.example.com/file.png',
        cdnUrl: 'https://cdn.example.com/file.png',
      }),
    ).toBe('https://blob.example.com/file.png');
  });
});
