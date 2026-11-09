/** @vitest-environment jsdom */
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  copyTextToClipboard: vi.fn(),
  success: vi.fn(),
}));

vi.mock('@/utils/clipboard', () => ({
  copyTextToClipboard: mocks.copyTextToClipboard,
}));

vi.mock('@/utils/feedback', () => ({
  message: {
    success: mocks.success,
    error: vi.fn(),
  },
}));

import { ConversationMessages } from './ConversationMessages';

describe('ConversationMessages', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.copyTextToClipboard.mockResolvedValue(undefined);
  });

  afterEach(cleanup);

  it('renders assistant markdown links as clickable external links', () => {
    render(
      <ConversationMessages
        messages={[
          {
            key: 'assistant-1',
            role: 'assistant',
            kind: '回复',
            title: '助手',
            content: '[点击观看](http://example.com/video.mp4)',
            seq: 1,
          },
        ]}
        loadingOlder={false}
        isWorkflow={false}
        threadRef={{ current: null }}
        onScroll={vi.fn()}
      />,
    );

    const link = screen.getByRole('link', { name: '点击观看' });
    expect(link.getAttribute('href')).toBe('http://example.com/video.mp4');
    expect(link.getAttribute('target')).toBe('_blank');
  });

  it('copies each message from its hover action', async () => {
    render(
      <ConversationMessages
        messages={[
          {
            key: 'assistant-1',
            role: 'assistant',
            kind: '回复',
            title: '助手',
            content: '可以复制的回复',
            seq: 1,
          },
        ]}
        loadingOlder={false}
        isWorkflow={false}
        threadRef={{ current: null }}
        onScroll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: '复制助手' }));
    await waitFor(() => {
      expect(mocks.copyTextToClipboard).toHaveBeenCalledWith('可以复制的回复');
      expect(mocks.success).toHaveBeenCalledWith('已复制');
    });
  });

  it('keeps assistant reasoning collapsed by default', () => {
    render(
      <ConversationMessages
        messages={[
          {
            key: 'reasoning-1',
            role: 'assistant',
            kind: '思考',
            title: '思考',
            content: '完整思考内容',
            seq: 1,
          },
        ]}
        loadingOlder={false}
        isWorkflow={false}
        threadRef={{ current: null }}
        onScroll={vi.fn()}
      />,
    );

    const details = screen.getByText('查看思考过程').closest('details');
    expect(details?.open).toBe(false);
  });
});
