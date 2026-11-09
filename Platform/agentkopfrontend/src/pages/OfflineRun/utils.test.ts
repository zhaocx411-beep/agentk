import { describe, expect, it } from 'vitest';
import { buildTraceItems } from './utils';

describe('buildTraceItems', () => {
  it('keeps original payloads as media sources for messages and tool results', () => {
    const assistantPayload = {
      text: '图片：https://example.com/result.png',
    };
    const toolPayload = {
      artifacts: [{ uri: 'https://example.com/result.mp4' }],
    };

    const items = buildTraceItems([
      {
        seq: 1,
        kind: 'assistant.text',
        data: assistantPayload,
        createdAt: '2026-09-07T08:00:00.000Z',
      },
      {
        seq: 2,
        kind: 'tool.exec_succeeded',
        data: toolPayload,
        createdAt: '2026-09-07T08:00:01.000Z',
      },
    ]);

    expect(items[0].parts[0].mediaSource).toBe(assistantPayload);
    expect(items[1].parts[0].mediaSource).toBe(toolPayload);
  });
});
