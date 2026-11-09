/** @vitest-environment jsdom */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { INITIAL_SSE_STATE } from '@/pages/AgentPlayground/hooks/sse/types';

const mocks = vi.hoisted(() => ({
  copyTextToClipboard: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('@/utils/clipboard', () => ({
  copyTextToClipboard: mocks.copyTextToClipboard,
}));

vi.mock('@/utils/feedback', () => ({
  message: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import OfficialLoopStreamPanel from './OfficialLoopStreamPanel';

class ResizeObserverMock {
  observe() {}
  disconnect() {}
  unobserve() {}
}

beforeAll(() => {
  vi.stubGlobal('ResizeObserver', ResizeObserverMock);
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
    configurable: true,
    value: vi.fn(),
  });
});

afterAll(() => {
  vi.unstubAllGlobals();
});

describe('OfficialLoopStreamPanel tool calls', () => {
  it('shows tool name, selected model and execution status', async () => {
    render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'assistant',
              parts: [
                {
                  type: 'tool-call',
                  toolCallId: 'call_1',
                  toolName: 'understand_media',
                  input: {
                    model: 'gemini-3.1-flash-lite',
                    prompt: '检查视频画面',
                  },
                },
              ],
            },
          ],
          toolCards: [
            {
              toolCallId: 'call_1',
              toolName: 'understand_media',
              status: 'ok',
              output: {
                result: { text: '质检通过', model: 'gemini-3.1-flash-lite' },
                duration: '9.105s',
                artifacts: [],
                toolCallId: 'call_1',
              },
            },
          ],
        }}
      />,
    );

    expect(screen.getByText('工具调用')).toBeTruthy();
    expect(screen.getByText('understand_media')).toBeTruthy();
    expect(screen.getByText('gemini-3.1-flash-lite')).toBeTruthy();
    expect(screen.getByText('已完成')).toBeTruthy();

    fireEvent.click(
      screen.getByRole('button', {
        name: '复制工具调用 understand_media',
      }),
    );
    await waitFor(() => {
      expect(mocks.copyTextToClipboard).toHaveBeenCalledWith(
        expect.stringContaining('"toolCallId": "call_1"'),
      );
    });

    fireEvent.click(screen.getByText('understand_media'));
    expect(await screen.findByText('完整调用参数')).toBeTruthy();
    expect(screen.getByText('完整执行结果')).toBeTruthy();
    expect(screen.getByText(/"duration": "9.105s"/)).toBeTruthy();
    expect(screen.getAllByText(/"toolCallId": "call_1"/)).toHaveLength(2);
  });

  it('scrolls to the latest content when switching sessions', async () => {
    const scrollToMock = vi.mocked(HTMLElement.prototype.scrollTo);
    const sseState = {
      ...INITIAL_SSE_STATE,
      messages: [
        {
          role: 'assistant' as const,
          parts: [{ type: 'text' as const, text: '历史回复' }],
        },
      ],
    };
    const { rerender } = render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={sseState}
        historyScrollToken="ses_one"
      />,
    );
    await waitFor(() => expect(scrollToMock).toHaveBeenCalled());
    scrollToMock.mockClear();

    rerender(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={sseState}
        historyScrollToken="ses_two"
        isLoadingHistory
      />,
    );
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(scrollToMock).not.toHaveBeenCalled();

    rerender(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...sseState,
          messages: [
            {
              role: 'assistant',
              parts: [{ type: 'text', text: '新会话历史' }],
            },
          ],
        }}
        historyScrollToken="ses_two"
        isLoadingHistory={false}
      />,
    );

    await waitFor(() => {
      expect(scrollToMock).toHaveBeenCalledWith({ top: expect.any(Number), behavior: 'auto' });
    });
  });

  it('keeps following delayed media unless the user explicitly scrolls upward', async () => {
    const scrollToMock = vi.mocked(HTMLElement.prototype.scrollTo);
    const sseState = {
      ...INITIAL_SSE_STATE,
      messages: [
        {
          role: 'assistant' as const,
          parts: [
            {
              type: 'text' as const,
              text: '![延迟图片](https://example.com/delayed.png)',
            },
          ],
        },
      ],
    };
    const { container, rerender } = render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={sseState}
        historyScrollToken="ses_media"
      />,
    );
    const image = container.querySelector('img');
    const scrollBody = container.querySelector('[class*="_body_"]');
    expect(image).toBeTruthy();
    expect(scrollBody).toBeTruthy();

    await waitFor(() => expect(scrollToMock).toHaveBeenCalled());
    scrollToMock.mockClear();
    fireEvent.load(image!);
    await waitFor(() =>
      expect(scrollToMock).toHaveBeenCalledWith({ top: expect.any(Number), behavior: 'auto' }),
    );

    fireEvent.wheel(scrollBody!, { deltaY: -100 });
    scrollToMock.mockClear();
    fireEvent.load(image!);
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(scrollToMock).not.toHaveBeenCalled();

    rerender(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...sseState,
          messages: [
            {
              role: 'assistant',
              parts: [
                {
                  type: 'text',
                  text: '![新会话延迟图片](https://example.com/new-delayed.png)',
                },
              ],
            },
          ],
        }}
        historyScrollToken="ses_media_again"
      />,
    );
    await waitFor(() => expect(scrollToMock).toHaveBeenCalled());
    scrollToMock.mockClear();
    fireEvent.load(container.querySelector('img')!);
    await waitFor(() => expect(scrollToMock).toHaveBeenCalled());
  });

  it('appends ask_user after the current conversation content', async () => {
    const onAnswer = vi.fn().mockResolvedValue(true);
    render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={onAnswer}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'assistant',
              parts: [{ type: 'text', text: '回答前内容' }],
            },
          ],
          pendingAskUser: {
            toolCallId: 'ask_1',
            questions: [
              {
                id: 'question_1',
                question: '是否继续？',
                options: ['继续', '停止'],
              },
            ],
          },
        }}
      />,
    );

    const content = screen.getByText('回答前内容');
    const askHeader = screen.getByText('agent-k 想先确认几个细节');
    expect(
      content.compareDocumentPosition(askHeader) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '继续' }));
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }));
    await waitFor(() => {
      expect(onAnswer).toHaveBeenCalledWith('ask_1', [
        { questionId: 'question_1', selected: ['继续'] },
      ]);
    });
  });

  it('keeps an answered ask_user in the conversation as read-only', () => {
    const { container } = render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'assistant',
              parts: [
                {
                  type: 'ask-user',
                  toolCallId: 'ask_answered',
                  questions: [
                    {
                      id: 'question_1',
                      question: '是否继续？',
                      options: ['继续', '停止'],
                    },
                  ],
                  answers: [{ questionId: 'question_1', selected: ['继续'] }],
                  status: 'submitted',
                },
              ],
            },
          ],
        }}
      />,
    );

    expect(screen.getByText('已提交')).toBeTruthy();
    const buttons = [...container.querySelectorAll('button')];
    expect(buttons.find((button) => button.textContent?.includes('继续'))?.disabled).toBe(true);
    expect(buttons.some((button) => button.textContent === '提交回答')).toBe(false);
  });

  it('renders a dismissed ask_user without entering an update loop', () => {
    const { container } = render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'assistant',
              parts: [
                {
                  type: 'ask-user',
                  toolCallId: 'ask_dismissed',
                  questions: [
                    {
                      id: 'question_1',
                      question: '是否继续？',
                      options: ['继续', '停止'],
                    },
                  ],
                  status: 'dismissed',
                },
              ],
            },
          ],
        }}
      />,
    );

    expect(container.textContent).toContain('已通过文字回复');
  });

  it('renders an uploaded audio attachment without relying on the URL extension', () => {
    const { container } = render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'user',
              parts: [
                {
                  type: 'file',
                  url: 'https://example.com/media/download?id=audio_1',
                  name: '测试音频',
                  mediaType: 'audio',
                  contentType: 'audio/mpeg',
                },
              ],
            },
          ],
        }}
      />,
    );

    const audio = container.querySelector('audio');
    expect(audio).toBeTruthy();
    expect(audio?.getAttribute('src')).toBe('https://example.com/media/download?id=audio_1');
    expect(audio?.getAttribute('aria-label')).toBe('测试音频');
  });

  it('does not place the Ant Design image wrapper inside a paragraph', () => {
    const { container } = render(
      <OfficialLoopStreamPanel
        variant="stage"
        onAnswer={vi.fn().mockResolvedValue(true)}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'assistant',
              parts: [{ type: 'text', text: '![预览图](https://example.com/preview.png)' }],
            },
          ],
        }}
      />,
    );

    const imageWrapper = container.querySelector('.ant-image');
    expect(imageWrapper).toBeTruthy();
    expect(imageWrapper?.parentElement?.tagName).not.toBe('P');
  });
});
