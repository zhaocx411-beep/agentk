/** @vitest-environment jsdom */
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { INITIAL_SSE_STATE, type SSEActions } from '@/pages/AgentPlayground/hooks/sse/types';
import OfficialOutputStage from './OfficialOutputStage';

const artifactState = vi.hoisted(() => ({
  artifacts: [] as Array<{
    kind: 'image';
    url: string;
    from: string;
  }>,
  loadingArtifacts: false,
  loadingMoreArtifacts: false,
}));

vi.mock('./useOfficialSessionArtifacts', () => ({
  useOfficialSessionArtifacts: () => artifactState,
}));

vi.mock('./OfficialConfigPanel', () => ({
  default: () => <div>官方配置内容</div>,
}));

vi.mock('./OfficialLoopStreamPanel', () => ({
  default: () => <div>生成过程内容</div>,
}));

const actions = {} as SSEActions;

describe('OfficialOutputStage tabs', () => {
  beforeEach(() => {
    artifactState.artifacts = [];
    artifactState.loadingArtifacts = false;
    artifactState.loadingMoreArtifacts = false;
  });

  it('keeps the manually selected tab while generation updates arrive', () => {
    const { rerender } = render(
      <OfficialOutputStage
        item={null}
        sessionId="ses_1"
        sseState={INITIAL_SSE_STATE}
        sseActions={actions}
      />,
    );

    fireEvent.click(screen.getByText('官方配置'));
    expect(screen.getByText('官方配置内容')).toBeTruthy();

    rerender(
      <OfficialOutputStage
        item={null}
        sessionId="ses_1"
        sseState={{
          ...INITIAL_SSE_STATE,
          isRunning: true,
          streamingText: '正在生成',
        }}
        sseActions={actions}
      />,
    );

    expect(screen.getByText('官方配置内容')).toBeTruthy();
    expect(screen.getByText('生成过程 · 进行中')).toBeTruthy();
    expect(screen.queryByText('生成过程内容')).toBeNull();
  });

  it('does not label active generation as historical artifact syncing', () => {
    artifactState.artifacts = [
      {
        kind: 'image',
        url: 'https://example.com/image.png',
        from: 'generate_image',
      },
    ];
    artifactState.loadingMoreArtifacts = true;
    const { rerender } = render(
      <OfficialOutputStage
        item={null}
        sessionId="ses_1"
        sseState={{ ...INITIAL_SSE_STATE, isRunning: true }}
        sseActions={actions}
      />,
    );

    fireEvent.click(screen.getByText('当前产物 1'));
    expect(screen.queryByText('正在补全历史产物…')).toBeNull();
    expect(screen.getByText('正在生成新的产物…')).toBeTruthy();

    rerender(
      <OfficialOutputStage
        item={null}
        sessionId="ses_1"
        sseState={INITIAL_SSE_STATE}
        sseActions={actions}
      />,
    );
    expect(screen.queryByText('正在生成新的产物…')).toBeNull();
    expect(screen.getByText('正在补全历史产物…')).toBeTruthy();
  });

  it('returns to the conversation stream when ask_user arrives', async () => {
    const { container, rerender } = render(
      <OfficialOutputStage
        item={null}
        sessionId="ses_1"
        sseState={INITIAL_SSE_STATE}
        sseActions={actions}
      />,
    );
    const stage = within(container);

    fireEvent.click(stage.getByText('官方配置'));
    expect(stage.getByText('官方配置内容')).toBeTruthy();

    rerender(
      <OfficialOutputStage
        item={null}
        sessionId="ses_1"
        sseState={{
          ...INITIAL_SSE_STATE,
          pendingAskUser: {
            toolCallId: 'ask_1',
            questions: [{ id: 'q1', question: '是否继续？', options: ['继续'] }],
          },
        }}
        sseActions={actions}
      />,
    );

    await waitFor(() => {
      expect(stage.getByText('生成过程内容')).toBeTruthy();
    });
    expect(stage.queryByText('官方配置内容')).toBeNull();
  });
});
