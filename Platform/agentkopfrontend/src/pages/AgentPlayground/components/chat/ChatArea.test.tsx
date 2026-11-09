/** @vitest-environment jsdom */
import { createRef } from 'react';
import { act, fireEvent, render } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { INITIAL_SSE_STATE, type SSEActions } from '../../hooks/sse/types';
import ChatArea from './ChatArea';

function createSseActions(): SSEActions {
  return {
    send: vi.fn().mockResolvedValue(true),
    cancel: vi.fn().mockResolvedValue(true),
    startSession: vi.fn(),
    resumeStream: vi.fn(),
    appendReplayEvents: vi.fn(),
    prependReplayEvents: vi.fn().mockReturnValue(true),
    stageLedgerEvents: vi.fn(),
    reset: vi.fn(),
    loadHistory: vi.fn(),
    prependHistory: vi.fn(),
    answer: vi.fn().mockResolvedValue(true),
  };
}

describe('ChatArea user attachments', () => {
  it('renders an uploaded audio attachment as a player', () => {
    const { container } = render(
      <ChatArea
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
        sseActions={createSseActions()}
        inputText=""
        setInputText={vi.fn()}
        onSend={vi.fn()}
        chatRef={createRef<HTMLDivElement>()}
        onChatScroll={vi.fn()}
        isLoadingMoreItems={false}
      />,
    );

    const audio = container.querySelector('audio');
    expect(audio).toBeTruthy();
    expect(audio?.getAttribute('src')).toBe('https://example.com/media/download?id=audio_1');
    expect(audio?.getAttribute('aria-label')).toBe('测试音频');
  });
});

describe('ChatArea session switching', () => {
  it('keeps the latest history visible while replay and media finish layout', () => {
    let frameId = 0;
    const frames = new Map<number, FrameRequestCallback>();
    const requestFrame = vi
      .spyOn(window, 'requestAnimationFrame')
      .mockImplementation((callback) => {
        frameId += 1;
        frames.set(frameId, callback);
        return frameId;
      });
    const cancelFrame = vi
      .spyOn(window, 'cancelAnimationFrame')
      .mockImplementation((id) => {
        frames.delete(id);
      });
    const chatRef = createRef<HTMLDivElement>();
    const actions = createSseActions();
    const baseProps = {
      sseActions: actions,
      inputText: '',
      setInputText: vi.fn(),
      onSend: vi.fn(),
      chatRef,
      onChatScroll: vi.fn(),
    };
    const oldState = {
      ...INITIAL_SSE_STATE,
      messages: [{ role: 'assistant' as const, parts: [{ type: 'text' as const, text: '旧回复' }] }],
    };
    const { container, rerender } = render(
      <ChatArea
        {...baseProps}
        sseState={oldState}
        stickBottomToken="ses_old"
        isLoadingMoreItems={false}
      />,
    );
    let contentHeight = 120;
    Object.defineProperty(chatRef.current, 'scrollHeight', {
      configurable: true,
      get: () => contentHeight,
    });

    for (let count = 0; count < 2; count += 1) {
      const initialFrame = [...frames.entries()][0];
      act(() => {
        if (!initialFrame) return;
        frames.delete(initialFrame[0]);
        initialFrame[1](count);
      });
    }

    // 会话 ID 先变化，此时 DOM 中仍是旧会话且历史正在加载。
    rerender(
      <ChatArea
        {...baseProps}
        sseState={oldState}
        stickBottomToken="ses_new"
        isLoadingMoreItems
      />,
    );

    // 随后新历史提交，但浏览器还未在当前帧更新出最终高度。
    rerender(
      <ChatArea
        {...baseProps}
        sseState={{
          ...INITIAL_SSE_STATE,
          messages: [
            {
              role: 'assistant',
              parts: [
                { type: 'text', text: '历史回复' },
                { type: 'image', url: 'https://example.com/history.png' },
              ],
            },
          ],
        }}
        stickBottomToken="ses_new"
        isLoadingMoreItems={false}
      />,
    );
    expect(chatRef.current?.scrollTop).toBe(120);

    contentHeight = 640;
    // 内容撑高产生 scroll 事件，不应被误判成用户主动上翻。
    if (chatRef.current) fireEvent.scroll(chatRef.current);
    const pendingFrames = [...frames.entries()];
    const nextFrame = pendingFrames[pendingFrames.length - 1];
    act(() => {
      if (!nextFrame) return;
      frames.delete(nextFrame[0]);
      nextFrame[1](16);
    });

    expect(chatRef.current?.scrollTop).toBe(640);

    contentHeight = 900;
    const historyImage = container.querySelector('img');
    expect(historyImage).toBeTruthy();
    if (historyImage) fireEvent.load(historyImage);
    expect(chatRef.current?.scrollTop).toBe(900);
    requestFrame.mockRestore();
    cancelFrame.mockRestore();
  });
});
