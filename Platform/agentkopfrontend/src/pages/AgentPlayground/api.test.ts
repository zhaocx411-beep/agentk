import { beforeEach, describe, expect, it, vi } from 'vitest';

const requestMocks = vi.hoisted(() => ({
  get: vi.fn(),
  patch: vi.fn(),
  post: vi.fn(),
}));

vi.mock('@/utils/request', () => requestMocks);

import {
  cancelSession,
  getSessionEvents,
  getSessions,
  getTtsVoices,
  sendMessage,
  submitAnswers,
  updateSessionTitle,
} from './api';

beforeEach(() => {
  vi.clearAllMocks();
});

describe('agent playground API contracts', () => {
  it('posts the V3 message body with structured attachments', async () => {
    requestMocks.post.mockResolvedValue({ executionId: 'exe_1' });

    await sendMessage('ses_1', '开始生成', {
      tts: { engine: 'minimax', voiceId: 'voice_1' },
      attachments: [
        {
          type: 'image',
          uri: 'https://example.com/product.png',
          mimeType: 'image/png',
        },
      ],
      userInput: { productImageUrls: ['https://example.com/product.png'] },
    });

    expect(requestMocks.post).toHaveBeenCalledWith(
      '/agent/operation/backend/agentk/sessions/ses_1/messages',
      {
        text: '开始生成',
        tts: { engine: 'minimax', voiceId: 'voice_1' },
        attachments: [
          {
            type: 'image',
            uri: 'https://example.com/product.png',
            mimeType: 'image/png',
          },
        ],
        userInput: { productImageUrls: ['https://example.com/product.png'] },
      },
    );
  });

  it('uses the expected endpoints for voices, cancel and answers', async () => {
    requestMocks.get.mockResolvedValue({ engines: [] });
    requestMocks.post.mockResolvedValue(undefined);

    await getTtsVoices();
    await cancelSession('ses_1', '用户停止');
    await submitAnswers('ses_1', {
      toolCallId: 'tool_1',
      answers: [{ questionId: 'q1', selected: ['继续'] }],
    });

    expect(requestMocks.get).toHaveBeenCalledWith(
      '/agent/operation/backend/agentk/tts/voices',
      undefined,
      { headers: { 'X-No-Loading': '1' } },
    );
    expect(requestMocks.post).toHaveBeenCalledWith(
      '/agent/operation/backend/agentk/sessions/ses_1/cancel',
      { reason: '用户停止' },
    );
    expect(requestMocks.post).toHaveBeenCalledWith(
      '/agent/operation/backend/agentk/sessions/ses_1/answers',
      {
        toolCallId: 'tool_1',
        answers: [{ questionId: 'q1', selected: ['继续'] }],
      },
    );
  });

  it('maps session records and falls back to publicId', async () => {
    requestMocks.post.mockResolvedValue({
      page: 1,
      pageSize: 1,
      total: 2,
      records: [
        {
          publicId: 'pcs_1',
          agentkSessionId: '',
          title: '异常会话',
          status: 'failed',
          createdAt: '2026-09-02 10:00:00',
        },
      ],
    });

    await expect(getSessions({ page: 1, pageSize: 1 })).resolves.toEqual({
      sessions: [
        expect.objectContaining({
          id: 'pcs_1',
          title: '异常会话',
          status: 'failed',
        }),
      ],
      hasMore: true,
      page: 1,
      pageSize: 1,
    });
  });

  it('deduplicates concurrent identical session queries', async () => {
    let resolveQuery: ((value: unknown) => void) | undefined;
    requestMocks.post.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveQuery = resolve;
        }),
    );

    const first = getSessions({ page: 2, pageSize: 20 });
    const second = getSessions({ page: 2, pageSize: 20 });
    expect(first).toBe(second);
    expect(requestMocks.post).toHaveBeenCalledTimes(1);

    resolveQuery?.({ page: 2, pageSize: 20, total: 0, records: [] });
    await expect(first).resolves.toEqual({
      sessions: [],
      hasMore: false,
      page: 2,
      pageSize: 20,
    });
  });

  it('maps platform ledger records and safely preserves invalid JSON', async () => {
    requestMocks.get.mockResolvedValue({
      page: 1,
      pageSize: 20,
      total: 2,
      records: [
        {
          seq: 2,
          kind: 'user.input',
          roundId: 'round_1',
          agentkRunId: 'exe_1',
          agentkEventId: 'evt_1',
          payloadJson: '{"text":"你好"}',
          createdAt: '2026-09-02 10:00:00',
          agentkCreatedAt: '2026-09-02T02:00:00.000Z',
        },
        {
          seq: 1,
          kind: 'execution.started',
          payloadJson: 'invalid-json',
          createdAt: '2026-09-02 09:59:59',
        },
      ],
    });

    const result = await getSessionEvents('ses/encoded', {
      page: 1,
      pageSize: 20,
    });

    expect(requestMocks.get).toHaveBeenCalledWith(
      '/agent/operation/backend/platform/admin/chat/sessions/by-agentk/ses%2Fencoded/events',
      { page: 1, pageSize: 20 },
    );
    expect(result.events).toEqual([
      expect.objectContaining({
        seq: 2,
        executionId: 'exe_1',
        data: { text: '你好' },
        createdAt: '2026-09-02T02:00:00.000Z',
      }),
      expect.objectContaining({
        seq: 1,
        data: 'invalid-json',
        createdAt: '2026-09-02 09:59:59',
      }),
    ]);
  });

  it('maps the updated session title response', async () => {
    requestMocks.patch.mockResolvedValue({
      publicId: 'pcs_1',
      agentkSessionId: 'ses_1',
      title: '新标题',
    });

    await expect(updateSessionTitle('ses/1', '新标题')).resolves.toEqual(
      expect.objectContaining({
        id: 'ses_1',
        title: '新标题',
      }),
    );
    expect(requestMocks.patch).toHaveBeenCalledWith(
      '/agent/operation/backend/platform/chat/sessions/by-agentk/ses%2F1/title',
      { title: '新标题' },
      { headers: { 'X-No-Loading': '1' } },
    );
  });
});
