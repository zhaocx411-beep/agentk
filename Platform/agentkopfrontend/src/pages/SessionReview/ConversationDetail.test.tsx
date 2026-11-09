/** @vitest-environment jsdom */
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import ConversationDetail from './ConversationDetail';

const mocks = vi.hoisted(() => ({
  getAdminSessionCase: vi.fn(),
  success: vi.fn(),
}));

vi.mock('./api', () => ({
  getAdminSessionCase: mocks.getAdminSessionCase,
}));

vi.mock('./components/ConversationReviewContent', () => ({
  ConversationReviewContent: ({ record }: { record: { title?: string } }) => (
    <div>{record.title}</div>
  ),
}));

vi.mock('@/utils/feedback', () => ({
  message: {
    success: mocks.success,
    error: vi.fn(),
  },
}));

function ListLocationProbe() {
  const location = useLocation();
  return <div>已返回筛选列表：{location.search}</div>;
}

describe('ConversationDetail', () => {
  it('returns to the list with its pagination and filters preserved', async () => {
    render(
      <MemoryRouter
        initialEntries={[
          {
            pathname: '/session-review/ses_1',
            state: {
              record: {
                id: 1,
                publicId: 'pcs_1',
                agentkSessionId: 'ses_1',
                title: '筛选后的会话',
              },
              returnTo: '/session-review?page=4&userId=wu_fuxingyi&status=active',
            },
          },
        ]}
        future={{ v7_startTransition: true, v7_relativeSplatPath: true }}
      >
        <Routes>
          <Route path="/session-review/:agentkSessionId" element={<ConversationDetail />} />
          <Route path="/session-review" element={<ListLocationProbe />} />
        </Routes>
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole('button', { name: /返回数据回看列表/ }));

    expect(
      await screen.findByText('已返回筛选列表：?page=4&userId=wu_fuxingyi&status=active'),
    ).toBeTruthy();
  });

  it('loads shared URLs directly and copies the current HTTP address', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });
    window.history.replaceState({}, '', '/session-review/ses_shared');
    mocks.getAdminSessionCase.mockResolvedValue({
      session: {
        publicId: 'pcs_1',
        agentkSessionId: 'ses_shared',
        title: '可分享的会话',
        sessionKind: 'agent_loop',
      },
    });

    render(
      <MemoryRouter
        initialEntries={['/session-review/ses_shared']}
        future={{ v7_startTransition: true, v7_relativeSplatPath: true }}
      >
        <Routes>
          <Route path="/session-review/:agentkSessionId" element={<ConversationDetail />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('可分享的会话')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /分享链接/ }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(window.location.href);
      expect(mocks.success).toHaveBeenCalledWith('分享链接已复制');
    });
  });
});
