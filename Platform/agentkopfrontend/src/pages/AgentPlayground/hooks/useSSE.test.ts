/** @vitest-environment jsdom */
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { SessionLedgerEvent } from '../types';

const apiMocks = vi.hoisted(() => ({
  cancelSession: vi.fn(),
  getSessionEvents: vi.fn(),
  sendMessage: vi.fn(),
  submitAnswers: vi.fn(),
}));

vi.mock('../api', () => apiMocks);

import { useSSE } from './useSSE';

function ledgerEvent(
  seq: number,
  kind: string,
  payload: unknown,
  executionId = 'exe_test',
): SessionLedgerEvent {
  return {
    seq,
    kind,
    payload,
    executionId,
    createdAt: '2026-09-02T08:00:00.000Z',
  };
}

function neverResolvingFetch() {
  return vi.fn(() => new Promise<Response>(() => undefined));
}

function streamEvent(
  kind: string,
  id: number,
  data: unknown,
  options?: { createdAt?: string; eventId?: string; executionId?: string; roundId?: string },
): string {
  return [
    `event: ${kind}`,
    `data: ${JSON.stringify({
      id,
      eventId: options?.eventId ?? `evt_${id}`,
      executionId: options?.executionId ?? 'exe_overlap',
      roundId: options?.roundId,
      kind,
      data,
      createdAt: options?.createdAt ?? `2026-09-03T06:18:${String(id).padStart(2, '0')}.000Z`,
    })}`,
    '',
    '',
  ].join('\n');
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('useSSE lifecycle', () => {
  it('starts the execution stream returned by sendMessage', async () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    apiMocks.sendMessage.mockResolvedValue({
      executionId: 'exe_sent',
      events: [],
    });
    const { result } = renderHook(() => useSSE());

    let sent = false;
    await act(async () => {
      sent = await result.current[1].send('ses_chat', '生成一个视频');
    });

    expect(sent).toBe(true);
    expect(apiMocks.sendMessage).toHaveBeenCalledWith('ses_chat', '生成一个视频', undefined);
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(
        '/agent/operation/backend/agentk/sessions/ses_chat/stream?executionId=exe_sent',
      ),
      expect.objectContaining({
        method: 'GET',
        credentials: 'include',
      }),
    );

    act(() => result.current[1].reset());
  });

  it('defaults to the legacy text-only attachment payload', async () => {
    vi.stubGlobal('fetch', neverResolvingFetch());
    apiMocks.sendMessage.mockResolvedValue({
      executionId: 'exe_legacy_attachment',
      events: [],
    });
    const { result } = renderHook(() => useSSE());

    await act(async () => {
      await result.current[1].send('ses_chat', '看看这张图', [
        {
          url: 'https://example.com/reference.png',
          mediaType: 'image',
          contentType: 'image/png',
          size: 1024,
        },
      ]);
    });

    expect(apiMocks.sendMessage).toHaveBeenCalledWith(
      'ses_chat',
      ['看看这张图', '', '- reference.png（image）：https://example.com/reference.png'].join('\n'),
      undefined,
    );

    act(() => result.current[1].reset());
  });

  it('sends V3 attachments for an explicitly enabled Agent while retaining the text list', async () => {
    vi.stubGlobal('fetch', neverResolvingFetch());
    apiMocks.sendMessage.mockResolvedValue({
      executionId: 'exe_attachment',
      events: [],
    });
    const { result } = renderHook(() => useSSE({ enableStructuredAttachments: true }));

    await act(async () => {
      await result.current[1].send('ses_chat', '看看这些媒体', [
        {
          url: 'https://example.com/reference.png',
          mediaType: 'image',
          contentType: 'image/png',
          size: 1024,
        },
        {
          url: 'https://down.ear0.com:3321/index/preview?soundid=44034&type=mp3&audio=sound.mp3',
          name: '治愈短视频BGM',
          mediaType: 'audio',
          size: 1024,
        },
      ]);
    });

    expect(apiMocks.sendMessage).toHaveBeenCalledWith(
      'ses_chat',
      [
        '看看这些媒体',
        '',
        '- reference.png（image）：https://example.com/reference.png',
        '- 治愈短视频BGM（audio）：https://down.ear0.com:3321/index/preview?soundid=44034&type=mp3&audio=sound.mp3',
      ].join('\n'),
      {
        attachments: [
          {
            type: 'image',
            uri: 'https://example.com/reference.png',
            mimeType: 'image/png',
          },
          {
            type: 'audio',
            uri: 'https://down.ear0.com:3321/index/preview?soundid=44034&type=mp3&audio=sound.mp3',
            mimeType: 'audio/mpeg',
          },
        ],
      },
    );
    const optimisticText = result.current[0].messages
      .flatMap((item) => item.parts)
      .find((part) => part.type === 'text');
    expect(optimisticText).toEqual({ type: 'text', text: '看看这些媒体' });

    act(() => result.current[1].reset());
  });

  it('only structures attachment types supported by the selected model', async () => {
    vi.stubGlobal('fetch', neverResolvingFetch());
    apiMocks.sendMessage.mockResolvedValue({
      executionId: 'exe_image_capability',
      events: [],
    });
    const imageOnlyAttachmentTypes = ['image'] as const;
    const { result } = renderHook(() =>
      useSSE({ structuredAttachmentTypes: imageOnlyAttachmentTypes }),
    );

    await act(async () => {
      await result.current[1].send('ses_chat', '分析这些素材', [
        {
          url: 'https://example.com/reference.png',
          mediaType: 'image',
          contentType: 'image/png',
          size: 1024,
        },
        {
          url: 'https://example.com/reference.wav',
          mediaType: 'audio',
          contentType: 'audio/wav',
          size: 1024,
        },
      ]);
    });

    expect(apiMocks.sendMessage).toHaveBeenCalledWith(
      'ses_chat',
      [
        '分析这些素材',
        '',
        '- reference.png（image）：https://example.com/reference.png',
        '- reference.wav（audio）：https://example.com/reference.wav',
      ].join('\n'),
      {
        attachments: [
          {
            type: 'image',
            uri: 'https://example.com/reference.png',
            mimeType: 'image/png',
          },
        ],
      },
    );

    act(() => result.current[1].reset());
  });

  it('accepts ask_user events when a new execution restarts its sequence', async () => {
    apiMocks.sendMessage
      .mockResolvedValueOnce({ executionId: 'exe_first', events: [] })
      .mockResolvedValueOnce({ executionId: 'exe_second', events: [] });
    const firstStream = streamEvent(
      'execution.finished',
      10,
      { status: 'EXECUTION_STATUS_COMPLETED' },
      { eventId: 'first_finished', executionId: 'exe_first' },
    );
    const secondStream = [
      streamEvent(
        'ask_user.requested',
        1,
        {
          toolCallId: 'tool_ask',
          questions: [{ id: 'q1', question: '是否继续？', options: ['继续'] }],
        },
        { eventId: 'second_ask', executionId: 'exe_second' },
      ),
      streamEvent(
        'execution.finished',
        2,
        { status: 'EXECUTION_STATUS_COMPLETED' },
        { eventId: 'second_finished', executionId: 'exe_second' },
      ),
    ].join('');
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(new Response(firstStream, { status: 200 }))
        .mockResolvedValueOnce(new Response(secondStream, { status: 200 })),
    );
    const { result } = renderHook(() => useSSE());

    await act(async () => {
      await result.current[1].send('ses_chat', '第一轮');
    });
    await waitFor(() => expect(result.current[0].isRunning).toBe(false));

    await act(async () => {
      await result.current[1].send('ses_chat', '第二轮');
    });
    await waitFor(() => {
      expect(result.current[0].pendingAskUser?.toolCallId).toBe('tool_ask');
    });

    act(() => result.current[1].reset());
  });

  it('restores local input and ask_user when platform history is still stale', async () => {
    const executionId = 'exe_latest_local';
    apiMocks.sendMessage.mockResolvedValue({
      executionId,
      events: [],
    });
    const responseBody = [
      streamEvent('execution.started', 1, {}, { eventId: 'latest_started', executionId }),
      streamEvent('user.input', 2, { text: '还有吗' }, { eventId: 'latest_input', executionId }),
      streamEvent(
        'ask_user.requested',
        38,
        {
          toolCallId: 'latest_ask',
          questions: [{ id: 'q1', question: '这批呢？', options: ['继续'] }],
        },
        { eventId: 'latest_ask_event', executionId },
      ),
      streamEvent(
        'execution.finished',
        39,
        { status: 'EXECUTION_STATUS_COMPLETED' },
        { eventId: 'latest_finished', executionId },
      ),
    ].join('');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() =>
        Promise.resolve(
          new Response(responseBody, {
            status: 200,
            headers: { 'Content-Type': 'text/event-stream' },
          }),
        ),
      ),
    );
    const { result } = renderHook(() => useSSE());

    await act(async () => {
      await result.current[1].send('ses_current', '还有吗');
    });
    await waitFor(() => {
      expect(result.current[0].pendingAskUser?.toolCallId).toBe('latest_ask');
    });

    act(() => result.current[1].startSession('ses_other'));
    act(() =>
      result.current[1].startSession('ses_current', [
        {
          ...ledgerEvent(287, 'execution.finished', {}, 'exe_stale'),
          eventId: 'stale_finished',
          createdAt: '2026-09-03T05:00:00.000Z',
        },
      ]),
    );

    await waitFor(() => {
      expect(result.current[0].messages).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            role: 'user',
            parts: [expect.objectContaining({ type: 'text', text: '还有吗' })],
          }),
          expect.objectContaining({
            role: 'assistant',
            parts: [expect.objectContaining({ type: 'ask-user', toolCallId: 'latest_ask' })],
          }),
        ]),
      );
      expect(result.current[0].pendingAskUser?.toolCallId).toBe('latest_ask');
    });

    act(() => result.current[1].reset());
  });

  it('restores an optimistic user input before the first stream event arrives', async () => {
    apiMocks.sendMessage.mockResolvedValue({
      executionId: 'exe_waiting_for_stream',
      events: [],
    });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() =>
        Promise.resolve(
          new Response('', {
            status: 200,
            headers: { 'Content-Type': 'text/event-stream' },
          }),
        ),
      ),
    );
    const { result } = renderHook(() => useSSE());

    await act(async () => {
      await result.current[1].send('ses_current', '刚发出的消息');
    });
    act(() => result.current[1].startSession('ses_other'));
    act(() => result.current[1].startSession('ses_current'));

    expect(result.current[0].messages).toEqual([
      expect.objectContaining({
        role: 'user',
        parts: [expect.objectContaining({ type: 'text', text: '刚发出的消息' })],
      }),
    ]);

    act(() => result.current[1].reset());
  });

  it('does not cache the previous conversation under a rapidly switched session', () => {
    const { result } = renderHook(() => useSSE());
    const firstConversation: SessionLedgerEvent[] = [
      {
        ...ledgerEvent(1, 'user.input', { text: '只属于会话 A' }, 'exe_a'),
        createdAt: '2026-09-03T08:40:00.000Z',
      },
    ];

    act(() => result.current[1].startSession('ses_a', firstConversation));
    expect(result.current[0].messages).toHaveLength(1);

    act(() => {
      result.current[1].startSession('ses_b');
      result.current[1].startSession('ses_c');
    });
    act(() => result.current[1].startSession('ses_b'));

    expect(result.current[0].messages).toEqual([]);

    act(() => result.current[1].reset());
  });

  it('replays platform history by global seq instead of createdAt', () => {
    const { result } = renderHook(() => useSSE());
    const history: SessionLedgerEvent[] = [
      {
        ...ledgerEvent(281, 'user.input', { text: '第二条' }, 'exe_second'),
        createdAt: '2026-09-03T08:39:00.000Z',
      },
      {
        ...ledgerEvent(280, 'user.input', { text: '第一条' }, 'exe_first'),
        createdAt: '2026-09-03T08:40:00.000Z',
      },
    ];

    act(() => result.current[1].startSession('ses_ordered', history));

    expect(result.current[0].messages.map((item) => item.parts[0])).toEqual([
      expect.objectContaining({ type: 'text', text: '第一条' }),
      expect.objectContaining({ type: 'text', text: '第二条' }),
    ]);

    act(() => result.current[1].reset());
  });

  it('does not apply a late send response to the conversation opened meanwhile', async () => {
    let resolveSend: (value: { executionId: string; events: SessionLedgerEvent[] }) => void = () =>
      undefined;
    apiMocks.sendMessage.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveSend = resolve;
        }),
    );
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useSSE());

    let sendPromise: Promise<boolean>;
    act(() => {
      sendPromise = result.current[1].send('ses_a', '只属于会话 A');
    });
    await waitFor(() => {
      expect(apiMocks.sendMessage).toHaveBeenCalledTimes(1);
    });
    act(() =>
      result.current[1].startSession('ses_b', [
        {
          ...ledgerEvent(1, 'user.input', { text: '只属于会话 B' }, 'exe_b'),
          createdAt: '2026-09-03T08:40:00.000Z',
        },
      ]),
    );
    await act(async () => {
      resolveSend({
        executionId: 'exe_a',
        events: [
          {
            ...ledgerEvent(1, 'user.input', { text: '只属于会话 A' }, 'exe_a'),
            eventId: 'late_a_input',
            createdAt: '2026-09-03T08:40:01.000Z',
          },
        ],
      });
      await sendPromise!;
    });

    expect(result.current[0].messages).toEqual([
      expect.objectContaining({
        parts: [expect.objectContaining({ type: 'text', text: '只属于会话 B' })],
      }),
    ]);
    expect(fetchMock).not.toHaveBeenCalled();

    act(() => result.current[1].reset());
  });

  it('keeps a replayed ask_user when its earlier user.input is filled from the stream', async () => {
    const executionId = 'exe_waiting_user';
    const askEvent = {
      ...ledgerEvent(
        5,
        'ask_user.requested',
        {
          toolCallId: 'tool_ask',
          questions: [{ id: 'q1', question: '是否继续？', options: ['继续'] }],
        },
        executionId,
      ),
      eventId: 'waiting_ask',
    };
    const responseBody = [
      streamEvent(
        'user.input',
        1,
        { text: '刚刚的问题' },
        { eventId: 'waiting_input', executionId },
      ),
      streamEvent('ask_user.requested', 5, askEvent.payload, {
        eventId: askEvent.eventId,
        executionId,
      }),
      streamEvent(
        'execution.finished',
        6,
        { status: 'EXECUTION_STATUS_COMPLETED' },
        { eventId: 'waiting_finished', executionId },
      ),
    ].join('');
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(responseBody, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_waiting_user', [askEvent], {
        resumeExecutionId: executionId,
      });
    });

    await waitFor(() => {
      expect(result.current[0].messages.map((messageItem) => messageItem.role)).toEqual([
        'user',
        'assistant',
      ]);
      expect(result.current[0].pendingAskUser?.toolCallId).toBe('tool_ask');
    });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.not.stringContaining('after='),
      expect.any(Object),
    );

    act(() => result.current[1].reset());
  });

  it('places a repeated low-id stream input after older platform history', async () => {
    const executionId = 'exe_new_batch';
    const history: SessionLedgerEvent[] = [
      {
        ...ledgerEvent(280, 'user.input', { text: '换一批' }, 'exe_old_batch'),
        createdAt: '2026-09-03T08:30:00.000Z',
      },
      {
        ...ledgerEvent(
          284,
          'assistant.tool_call',
          {
            toolCallId: 'old_batch_ask',
            toolName: 'ask_user',
            args: { questions: [] },
          },
          'exe_old_batch',
        ),
        roundId: 'rnd_old_batch',
        createdAt: '2026-09-03T08:30:05.000Z',
      },
    ];
    const responseBody = [
      streamEvent(
        'user.input',
        2,
        { text: '换一批' },
        {
          createdAt: '2026-09-03T08:35:00.000Z',
          eventId: 'new_batch_input',
          executionId,
        },
      ),
      streamEvent(
        'execution.finished',
        3,
        { status: 'EXECUTION_STATUS_COMPLETED' },
        { eventId: 'new_batch_finished', executionId },
      ),
    ].join('');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(responseBody, {
          status: 200,
          headers: { 'Content-Type': 'text/event-stream' },
        }),
      ),
    );
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_repeat_batch', history, {
        resumeExecutionId: executionId,
      });
    });

    await waitFor(() => {
      const messages = result.current[0].messages;
      expect(messages[messages.length - 1]).toMatchObject({
        role: 'user',
        parts: [expect.objectContaining({ type: 'text', text: '换一批' })],
      });
    });
    expect(result.current[0].messages.filter((item) => item.role === 'user')).toHaveLength(2);

    act(() => result.current[1].reset());
  });

  it('switches to the new execution returned after answering ask_user', async () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    apiMocks.submitAnswers.mockResolvedValue({
      executionId: 'exe_after_ask',
    });
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_ask', [], {
        resumeExecutionId: 'exe_before_ask',
      });
    });
    await act(async () => {
      await result.current[1].answer('tool_ask', [{ questionId: 'q1', selected: ['继续'] }]);
    });

    expect(apiMocks.submitAnswers).toHaveBeenCalledWith('ses_ask', {
      toolCallId: 'tool_ask',
      answers: [{ questionId: 'q1', selected: ['继续'] }],
    });
    expect(fetchMock).toHaveBeenLastCalledWith(
      expect.stringContaining(
        '/agent/operation/backend/agentk/sessions/ses_ask/stream?executionId=exe_after_ask',
      ),
      expect.objectContaining({
        method: 'GET',
        credentials: 'include',
      }),
    );

    act(() => result.current[1].reset());
  });

  it('replays an answered ask_user as a submitted history card', () => {
    const { result } = renderHook(() => useSSE());
    const history = [
      ledgerEvent(5, 'ask_user.requested', {
        toolCallId: 'tool_answered',
        questions: [{ id: 'q1', question: '选择画幅', options: ['横屏', '竖屏'] }],
      }),
      ledgerEvent(6, 'ask_user.responded', {
        toolCallId: 'tool_answered',
        answers: [{ questionId: 'q1', selected: ['横屏'] }],
      }),
    ];

    act(() => result.current[1].startSession('ses_answered', history));

    expect(result.current[0].pendingAskUser).toBeNull();
    expect(result.current[0].messages[0]?.parts[0]).toMatchObject({
      type: 'ask-user',
      toolCallId: 'tool_answered',
      status: 'submitted',
      answers: [{ questionId: 'q1', selected: ['横屏'] }],
    });

    act(() => result.current[1].reset());
  });

  it('resumes the answered execution after switching away before its first event', async () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    apiMocks.submitAnswers.mockResolvedValue({
      executionId: 'exe_after_ask',
    });
    const staleHistory = [
      ledgerEvent(
        1,
        'ask_user.requested',
        {
          toolCallId: 'tool_ask',
          questions: [{ id: 'q1', question: '是否继续？', options: ['继续'] }],
        },
        'exe_before_ask',
      ),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => result.current[1].startSession('ses_ask', staleHistory));
    expect(result.current[0].pendingAskUser?.toolCallId).toBe('tool_ask');

    await act(async () => {
      await result.current[1].answer('tool_ask', [{ questionId: 'q1', selected: ['继续'] }]);
    });
    act(() => result.current[1].startSession('ses_other'));
    act(() => result.current[1].startSession('ses_ask', staleHistory));

    expect(fetchMock).toHaveBeenLastCalledWith(
      expect.stringContaining(
        '/agent/operation/backend/agentk/sessions/ses_ask/stream?executionId=exe_after_ask',
      ),
      expect.objectContaining({
        method: 'GET',
        credentials: 'include',
      }),
    );
    expect(result.current[0].pendingAskUser).toBeNull();
    expect(
      result.current[0].messages
        .flatMap((messageItem) => messageItem.parts)
        .find((part) => part.type === 'ask-user' && part.toolCallId === 'tool_ask'),
    ).toMatchObject({
      type: 'ask-user',
      status: 'submitted',
      answers: [{ questionId: 'q1', selected: ['继续'] }],
    });

    act(() => result.current[1].reset());
  });

  it('keeps the old stream alive for the worker binding grace period', async () => {
    vi.useFakeTimers();
    const signals: AbortSignal[] = [];
    const fetchMock = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>(() => {
          if (init?.signal) signals.push(init.signal);
        }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_one', [], {
        resumeExecutionId: 'exe_one',
      });
    });
    act(() => {
      result.current[1].startSession('ses_two', [], {
        resumeExecutionId: 'exe_two',
      });
    });

    expect(signals).toHaveLength(2);
    expect(signals[0]?.aborted).toBe(false);
    expect(signals[1]?.aborted).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(999);
    });
    expect(signals[0]?.aborted).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(signals[0]?.aborted).toBe(true);

    act(() => result.current[1].reset());
  });

  it('replays a terminal ledger without opening another stream', () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const history = [
      ledgerEvent(1, 'execution.started', {}),
      ledgerEvent(2, 'user.input', { text: '历史问题' }),
      ledgerEvent(3, 'execution.finished', {
        status: 'EXECUTION_STATUS_COMPLETED',
      }),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => result.current[1].startSession('ses_history', history));

    expect(fetchMock).not.toHaveBeenCalled();
    expect(result.current[0].isRunning).toBe(false);
    expect(result.current[0].messages).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          role: 'user',
          parts: [expect.objectContaining({ type: 'text', text: '历史问题' })],
        }),
      ]),
    );
  });

  it('shows the backend failure reason from execution.finished', () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const history = [
      ledgerEvent(1, 'execution.started', {}),
      ledgerEvent(2, 'execution.finished', {
        status: 'EXECUTION_STATUS_FAILED',
        error:
          'session: exceeded max tool rounds 20 (type: MaxToolRoundsExceeded, retryable: false)',
      }),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => result.current[1].startSession('ses_failed', history));

    expect(result.current[0].isRunning).toBe(false);
    expect(result.current[0].error).toBe('执行失败：工具调用轮数已达到上限（20 轮），执行已停止');
  });

  it('preserves the complete tool execution payload', () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const history = [
      ledgerEvent(1, 'assistant.tool_call', {
        toolName: 'understand_media',
        toolCallId: 'call_1',
        args: { model: 'gemini-3.1-pro-preview' },
      }),
      ledgerEvent(2, 'tool.exec_succeeded', {
        result: { text: '质检通过', model: 'gemini-3.1-pro-preview' },
        duration: '38.690s',
        artifacts: [],
        toolCallId: 'call_1',
      }),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => result.current[1].startSession('ses_tool', history));

    expect(result.current[0].toolCards[0]?.output).toEqual({
      result: { text: '质检通过', model: 'gemini-3.1-pro-preview' },
      duration: '38.690s',
      artifacts: [],
      toolCallId: 'call_1',
    });
  });

  it('settles residual running tools when the execution is finished', () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const history = [
      ledgerEvent(1, 'assistant.tool_call', {
        toolName: 'generate_image',
        toolCallId: 'call_without_terminal_event',
        args: { prompt: '一只猫' },
      }),
      ledgerEvent(2, 'execution.finished', {
        status: 'EXECUTION_STATUS_COMPLETED',
      }),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => result.current[1].startSession('ses_finished_tool', history));

    expect(result.current[0].isRunning).toBe(false);
    expect(result.current[0].toolCards).toEqual([
      expect.objectContaining({
        toolCallId: 'call_without_terminal_event',
        status: 'ok',
      }),
    ]);
  });

  it('does not use the platform history seq as a stream cursor', () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const history = [
      ledgerEvent(7, 'execution.started', {}, 'exe_resume'),
      ledgerEvent(8, 'user.input', { text: '继续执行' }, 'exe_resume'),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_resume', history, {
        resumeExecutionId: 'exe_resume',
      });
    });

    expect(fetchMock).toHaveBeenCalledWith(
      expect.not.stringContaining('after='),
      expect.any(Object),
    );

    act(() => result.current[1].reset());
  });

  it('starts from zero when stream binding has only platform history', () => {
    const fetchMock = neverResolvingFetch();
    vi.stubGlobal('fetch', fetchMock);
    const history = [
      ledgerEvent(79, 'execution.finished', {}, 'exe_old'),
      ledgerEvent(1, 'execution.started', {}, 'exe_current'),
      ledgerEvent(2, 'user.input', { text: '当前问题' }, 'exe_current'),
    ];
    const { result } = renderHook(() => useSSE());

    act(() => result.current[1].startSession('ses_late_resume', history));
    act(() => result.current[1].resumeStream('ses_late_resume', 'exe_current'));

    expect(fetchMock).toHaveBeenCalledWith(
      expect.not.stringContaining('after='),
      expect.any(Object),
    );

    act(() => result.current[1].reset());
  });

  it('deduplicates overlapping history and stream events using data.id as cursor', async () => {
    const history = [
      {
        ...ledgerEvent(1, 'execution.started', {}, 'exe_overlap'),
        eventId: 'evt_1',
      },
      {
        ...ledgerEvent(2, 'user.input', { text: '生成一张图片' }, 'exe_overlap'),
        eventId: 'evt_2',
      },
    ];
    const responseBody = [
      streamEvent('user.input', 2, { text: '生成一张图片' }),
      streamEvent('round.started', 3, {}, { roundId: 'rnd_overlap' }),
      streamEvent(
        'assistant.text',
        4,
        { block: 0, text: '已生成图片' },
        { roundId: 'rnd_overlap' },
      ),
      streamEvent('round.completed', 5, {}, { roundId: 'rnd_overlap' }),
      streamEvent('execution.finished', 6, {
        status: 'EXECUTION_STATUS_COMPLETED',
      }),
    ].join('');
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(responseBody, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_overlap', history, {
        resumeExecutionId: 'exe_overlap',
      });
    });

    await waitFor(() => {
      expect(result.current[0].isRunning).toBe(false);
      expect(result.current[0].messages).toHaveLength(2);
    });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.not.stringContaining('after='),
      expect.any(Object),
    );
    expect(result.current[0].messages[0]).toMatchObject({
      role: 'user',
      parts: [expect.objectContaining({ type: 'text', text: '生成一张图片' })],
    });
    expect(result.current[0].messages[1]).toMatchObject({
      role: 'assistant',
      parts: [expect.objectContaining({ type: 'text', text: '已生成图片' })],
    });

    act(() => {
      result.current[1].appendReplayEvents(history);
    });
    expect(result.current[0].messages).toHaveLength(2);

    act(() => result.current[1].reset());
  });

  it('retries a stream that is briefly unavailable', async () => {
    vi.useFakeTimers();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 404 }))
      .mockImplementation(() => new Promise<Response>(() => undefined));
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useSSE());

    act(() => {
      result.current[1].startSession('ses_retry', [], {
        resumeExecutionId: 'exe_retry',
      });
    });
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });

    expect(fetchMock).toHaveBeenCalledTimes(2);

    act(() => result.current[1].reset());
  });
});
