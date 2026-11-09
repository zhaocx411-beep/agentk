import { describe, expect, it } from 'vitest';
import type { SessionLedgerEvent } from '@/pages/AgentPlayground/types';
import { sessionEventsToReviewMessages } from './conversationUtils';

function event(
  seq: number,
  kind: string,
  payload: unknown,
): SessionLedgerEvent {
  return {
    seq,
    kind,
    payload,
    createdAt: '2026-09-02T11:30:00.000Z',
  };
}

describe('sessionEventsToReviewMessages tool calls', () => {
  it('shows the tool name and selected model prominently', () => {
    const messages = sessionEventsToReviewMessages([
      event(1, 'assistant.tool_call', {
        name: 'generate_image',
        toolCallId: 'call_1',
        args: {
          model: 'gemini-3.1-flash-lite',
          prompt: '生成产品图',
        },
      }),
    ]);

    expect(messages[0]).toMatchObject({
      title: '工具调用',
      kind: 'generate_image',
    });
    expect(messages[0]?.content).toContain('模型：gemini-3.1-flash-lite');
    expect(messages[0]?.content).toContain('"prompt": "生成产品图"');
  });

  it('formats model groups from JSON-string arguments', () => {
    const messages = sessionEventsToReviewMessages([
      event(1, 'assistant.tool_call', {
        tool_name: 'documentary_video',
        arguments: JSON.stringify({
          models: {
            vision: 'gemini-3.1-flash-lite',
            story: 'deepseek-v4-pro',
          },
        }),
      }),
    ]);

    expect(messages[0]?.content).toContain(
      '模型：vision: gemini-3.1-flash-lite · story: deepseek-v4-pro',
    );
  });

  it('reuses the call tool name for its completion event', () => {
    const messages = sessionEventsToReviewMessages([
      event(1, 'assistant.tool_call', {
        name: 'kling_text_to_video',
        toolCallId: 'call_1',
        args: { model: 'kling-v3' },
      }),
      event(2, 'tool.exec_succeeded', {
        toolCallId: 'call_1',
        duration: '36.37s',
      }),
    ]);

    expect(messages[1]).toMatchObject({
      title: '工具完成',
      kind: 'kling_text_to_video',
    });
    expect(messages[1]?.content).toContain('"duration": "36.37s"');
    expect(messages[1]?.content).toContain('"toolCallId": "call_1"');
  });
});

describe('sessionEventsToReviewMessages execution result', () => {
  it('keeps the execution failure reason visible', () => {
    const messages = sessionEventsToReviewMessages([
      event(140, 'execution.finished', {
        status: 'EXECUTION_STATUS_FAILED',
        error:
          'session: exceeded max tool rounds 20 (type: MaxToolRoundsExceeded, retryable: false)',
      }),
    ]);

    expect(messages[0]).toMatchObject({
      title: '执行失败',
      content:
        'session: exceeded max tool rounds 20 (type: MaxToolRoundsExceeded, retryable: false)',
    });
  });
});
