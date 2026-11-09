/**
 * PreviewPanel - 详情页右侧预览与调试
 * Tab「对话」嵌入 Playground ChatArea（发送/停止）；「历史会话」就地切换账本
 */
import { useCallback, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { Segmented } from 'antd';
import { WorkbenchPane } from '@/components';
import ChatArea from '@/pages/AgentPlayground/components/chat/ChatArea';
import type { ChatAttachmentBarHandle } from '@/pages/AgentPlayground/components/chat/ChatAttachmentBar';
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import type { MessageAttachment, Session } from '@/pages/AgentPlayground/types';
import type { SSEActions, SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import type { AgentDetailData } from '../../types';
import { applyOfficialLoopTemplatePick } from '../../OfficialExperience/OfficialFreeChat/templates/applyOfficialLoopTemplatePick';
import SessionHistoryList from './SessionHistoryList';
import { useAgentDetailPromptShell } from '../hooks/useAgentDetailPromptShell';
import styles from '../AgentDetail.module.less';

type PreviewTab = 'chat' | 'history';

interface PreviewPanelProps {
  detail: AgentDetailData;
  versionMap: Record<number, number>;
  sseState: SSEState;
  sseActions: SSEActions;
  inputText: string;
  setInputText: (v: string) => void;
  onSend: (attachments?: MessageAttachment[]) => boolean | void | Promise<boolean | void>;
  sendPending?: boolean;
  chatRef: RefObject<HTMLDivElement>;
  isLoadingHistory: boolean;
  /** 对话区上滚到顶时加载更早的对话 */
  onChatScroll: () => void;
  sessionId: string | null;
  sessions: Session[];
  sessionsLoading: boolean;
  isLoadingMoreSessions?: boolean;
  historyListRef?: RefObject<HTMLDivElement>;
  onHistoryScroll?: () => void;
  onSwitchSession: (sessionId: string, meta?: Session) => void;
  onSessionTitleUpdated?: (sessionId: string, title: string) => void;
  /** 基于官方模版暂存编辑时为 true，可接入官方 pipeline 灵感模版 */
  isFromOfficial?: boolean;
  officialAgentLoopId?: number;
}

function PreviewPanel({
  detail,
  versionMap,
  sseState,
  sseActions,
  inputText,
  setInputText,
  onSend,
  sendPending = false,
  chatRef,
  isLoadingHistory,
  onChatScroll,
  sessionId,
  sessions,
  sessionsLoading,
  isLoadingMoreSessions,
  historyListRef,
  onHistoryScroll,
  onSwitchSession,
  onSessionTitleUpdated,
  isFromOfficial = false,
  officialAgentLoopId,
}: PreviewPanelProps) {
  const [tab, setTab] = useState<PreviewTab>('chat');
  const attachmentBarRef = useRef<ChatAttachmentBarHandle | null>(null);
  const handleTemplatePick = useCallback(
    (item: ChatSuggestionItem) => {
      applyOfficialLoopTemplatePick(item, setInputText, attachmentBarRef.current);
    },
    [setInputText],
  );
  const handleAttachmentBarReady = useCallback((handle: ChatAttachmentBarHandle | null) => {
    attachmentBarRef.current = handle;
  }, []);
  const promptShell = useAgentDetailPromptShell(
    isFromOfficial,
    officialAgentLoopId,
    handleTemplatePick,
    sseState.isRunning,
  );

  const handleOpenSession = (sid: string, meta?: Session) => {
    void onSwitchSession(sid, meta);
    setTab('chat');
  };

  return (
    <WorkbenchPane
      tone="muted"
      ariaLabel="预览与调试"
      className={styles.previewPanel}
      contentClassName={styles.previewPaneContent}
      header={
        <div className={styles.previewHeaderContent}>
          <span className={styles.previewTitle}>预览与调试</span>
          <Segmented<PreviewTab>
            size="small"
            value={tab}
            onChange={setTab}
            options={[
              { value: 'chat', label: '对话' },
              { value: 'history', label: '历史会话' },
            ]}
          />
        </div>
      }
    >
      {tab === 'chat' ? (
        <div className={styles.previewChatWrap}>
          <ChatArea
            sseState={sseState}
            sseActions={sseActions}
            inputText={inputText}
            setInputText={setInputText}
            onSend={onSend}
            sendPending={sendPending}
            chatRef={chatRef}
            onChatScroll={onChatScroll}
            isLoadingMoreItems={isLoadingHistory}
            stickBottomToken={sessionId}
            emptyAgentName={detail.name}
            suggestions={[]}
            composerPromptStrip={promptShell.composerPromptStrip}
            composerToolbarExtra={promptShell.composerToolbarExtra}
            showEmptyIcon={promptShell.showEmptyIcon}
            emptyTitle={promptShell.emptyTitle}
            emptyDescription={promptShell.emptyDescription}
            enableAttachments
            variant="quiet"
            onAttachmentBarReady={handleAttachmentBarReady}
          />
        </div>
      ) : (
        <SessionHistoryList
          versionMap={versionMap}
          currentVersionId={detail.versionId}
          currentSessionId={sessionId}
          sessions={sessions}
          loading={sessionsLoading}
          isLoadingMoreSessions={isLoadingMoreSessions}
          historyListRef={historyListRef}
          onHistoryScroll={onHistoryScroll}
          onOpenSession={handleOpenSession}
          onSessionTitleUpdated={onSessionTitleUpdated}
        />
      )}
    </WorkbenchPane>
  );
}

export default PreviewPanel;
