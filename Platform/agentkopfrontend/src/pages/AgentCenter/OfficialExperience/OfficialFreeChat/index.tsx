/**
 * 官方自主 Agent 自由对话页
 * 入口从 CreationStudio 切出，交互对齐 Playground：左历史 + 右对话 + 快捷文案模版
 * 工作流仍走 studio/CreationStudio，本文件不替代那套逻辑
 */
import { useMemo, useCallback, useRef } from 'react';
import { Button, Empty, Spin, Tooltip } from 'antd';
import { ArrowLeftOutlined, FormOutlined, MessageOutlined } from '@ant-design/icons';
import {
  SessionHistoryRow,
  SessionTitleRenameButton,
  useSessionTitleRename,
} from '@/components';
import OfficialLoopWorkbench from './workbench/OfficialLoopWorkbench';
import type { ChatAttachmentBarHandle } from '@/pages/AgentPlayground/components/chat/ChatAttachmentBar';
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import { useGoBack } from '@/hooks';
import { formatUpdatedTime } from '@/utils/format';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { isOfficialMangaLoop } from '../../utils';
import type { useOfficialExperienceChat } from '../useOfficialExperienceChat';
import OfficialOutputStage from './output/OfficialOutputStage';
import { MangaWorkspace } from './manga/MangaWorkspace';
import { getOfficialFreeChatGuide } from './templates/promptTemplates';
import OfficialLoopTemplateLauncher from './templates/OfficialLoopTemplateLauncher';
import OfficialLoopPromptStrip from './templates/OfficialLoopPromptStrip';
import { useOfficialLoopPromptTemplates } from './templates/useOfficialLoopPromptTemplates';
import { applyOfficialLoopTemplatePick } from './templates/applyOfficialLoopTemplatePick';
import { applyVirtualTryOnTemplatePick } from './virtualTryOn/applyVirtualTryOnTemplatePick';
import {
  AGENT_RUN_STATUS_LABEL,
  resolveAgentRunStatus,
} from '../runStatus';
import styles from './OfficialFreeChat.module.less';

type ChatModel = ReturnType<typeof useOfficialExperienceChat>;

const CONVERSATION_RUN_STATUS_CLASS = {
  running: styles.conversationRunning,
  processing: styles.conversationProcessing,
  failed: styles.conversationFailed,
  idle: styles.conversationIdle,
} as const;

interface OfficialFreeChatProps {
  sessionId: string;
  item: OfficialCatalogCardData | null;
  chat: ChatModel;
  usingTemplate: boolean;
  onCustomize: () => void;
}

function OfficialFreeChat({
  sessionId,
  item,
  chat,
  usingTemplate,
  onCustomize,
}: OfficialFreeChatProps) {
  const goBack = useGoBack('/');
  const attachmentBarRef = useRef<ChatAttachmentBarHandle | null>(null);
  const guide = useMemo(() => getOfficialFreeChatGuide(item), [item]);
  const promptTemplates = useOfficialLoopPromptTemplates(item);
  const handleTemplatePick = useCallback(
    (pick: ChatSuggestionItem) => {
      if (chat.needsVirtualTryOn) {
        applyVirtualTryOnTemplatePick(pick, chat.setInputText, chat.virtualTryOnComposer);
        return;
      }
      applyOfficialLoopTemplatePick(pick, chat.setInputText, attachmentBarRef.current);
    },
    [chat.setInputText, chat.needsVirtualTryOn, chat.virtualTryOnComposer],
  );
  const handleAttachmentBarReady = useCallback((handle: ChatAttachmentBarHandle | null) => {
    attachmentBarRef.current = handle;
  }, []);
  const showPromptTemplates = promptTemplates.loading || promptTemplates.fromApi;
  const templateLauncher =
    showPromptTemplates && promptTemplates.fromApi ? (
      <OfficialLoopTemplateLauncher
        catalog={promptTemplates}
        onPick={handleTemplatePick}
        disabled={chat.sseState.isRunning}
      />
    ) : null;
  const promptStrip = showPromptTemplates ? (
    <OfficialLoopPromptStrip
      items={promptTemplates.strip}
      loading={promptTemplates.loading}
      refreshing={promptTemplates.refreshingStrip}
      skeletonCount={6}
      onPick={handleTemplatePick}
      onRefresh={promptTemplates.fromApi ? promptTemplates.refreshStrip : undefined}
    />
  ) : null;
  const isManga = !!item && isOfficialMangaLoop(item);
  const currentSession = chat.sessions.find((session) => session.id === sessionId);
  const conversationTitle =
    currentSession?.title?.trim() ||
    (sessionId === 'new' ? '新对话' : `对话 ${sessionId.slice(-8)}`);
  const hasPendingJobs = chat.sseState.jobs.some((job) => job.status === 'submitted');
  const runStatus = resolveAgentRunStatus(
    chat.sseState.isRunning,
    hasPendingJobs,
    Boolean(chat.sseState.error),
  );
  const runLabel = AGENT_RUN_STATUS_LABEL[runStatus];
  const runClass = CONVERSATION_RUN_STATUS_CLASS[runStatus];
  const { openRename, renameModal } = useSessionTitleRename(chat.updateSessionTitle);

  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <button
          type="button"
          className={styles.homeLink}
          aria-label="返回上一页"
          onClick={goBack}
        >
          <ArrowLeftOutlined aria-hidden />
          <span>返回</span>
        </button>
        <div className={styles.agentBar}>
          <span className={styles.agentBarName}>{item?.name || '官方 Agent'}</span>
          <span className={styles.agentBarDesc}>图文 · 视频 · 创意策划</span>
        </div>
        <div className={styles.sidebarHead}>
          <button
            type="button"
            className={styles.newChatItem}
            onClick={() => void chat.handleNewChat()}
          >
            <span className={styles.newChatIcon} aria-hidden>
              <FormOutlined />
            </span>
            <span>开启新对话</span>
          </button>
          <div className={styles.sidebarTitleRow}>
            <h2 className={styles.sidebarTitle}>对话</h2>
            <span className={styles.historyCount}>{chat.sessions.length}</span>
          </div>
        </div>
        <div
          ref={chat.historyListRef}
          className={styles.historyScroll}
          onScroll={chat.handleHistoryScroll}
        >
          {chat.sessionsLoading && chat.sessions.length === 0 ? (
            <div className={styles.historyStatus}>
              <Spin size="small" />
            </div>
          ) : chat.sessions.length === 0 ? (
            <div className={styles.historyEmpty}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史记录" />
            </div>
          ) : (
            <ul className={styles.historyList}>
              {chat.sessions.map((session) => {
                const active = session.id === sessionId;
                const title = session.title?.trim() || `对话 ${session.id.slice(-8)}`;
                return (
                  <li key={session.id}>
                    <SessionHistoryRow
                      className={`${styles.historyItem} ${active ? styles.historyItemActive : ''}`}
                    >
                      <button
                        type="button"
                        className={styles.historyItemBody}
                        onClick={() => chat.handleSwitchSession(session.id, session)}
                      >
                        <span className={styles.historyItemIcon} aria-hidden>
                          <MessageOutlined />
                        </span>
                        <Tooltip title={title} placement="right">
                          <span className={styles.historyItemTitle}>{title}</span>
                        </Tooltip>
                        {session.createdAt ? (
                          <span className={styles.historyItemTime}>
                            {formatUpdatedTime(session.createdAt)}
                          </span>
                        ) : null}
                      </button>
                      <SessionTitleRenameButton session={session} onRename={openRename} />
                    </SessionHistoryRow>
                  </li>
                );
              })}
              {chat.isLoadingMoreSessions && <li className={styles.historyStatus}>加载中…</li>}
            </ul>
          )}
        </div>
      </aside>
      {renameModal}
      <section className={styles.mainStage}>
        <div className={styles.chatTopbar}>
          <div className={styles.conversationInfo}>
            <h2 className={styles.conversationTitle} title={conversationTitle}>
              {conversationTitle}
            </h2>
            <span className={`${styles.conversationStatus} ${runClass}`}>{runLabel}</span>
          </div>
          <div className={styles.chatTopbarActions}>
            <Button
              type="primary"
              className={styles.customizeAction}
              loading={usingTemplate}
              disabled={!item}
              onClick={onCustomize}
            >
              去自定义配置
            </Button>
          </div>
        </div>
        <div className={styles.stageBody}>
          <div className={styles.workColumnWrap}>
            {isManga ? (
              <MangaWorkspace
                item={item}
                chat={chat}
                composerPromptStrip={promptStrip}
                composerToolbarExtra={templateLauncher}
                emptyDescription={guide.emptyDescription}
                sessionId={sessionId}
                onAttachmentBarReady={handleAttachmentBarReady}
              />
            ) : (
              <OfficialLoopWorkbench
                chat={chat}
                sessionId={sessionId}
                emptyDescription={guide.emptyDescription}
                composerPromptStrip={promptStrip}
                composerToolbarExtra={templateLauncher}
                onAttachmentBarReady={handleAttachmentBarReady}
              />
            )}
          </div>
          <OfficialOutputStage
            item={item}
            sessionId={sessionId}
            sseState={chat.sseState}
            sseActions={chat.sseActions}
            streamScrollRef={chat.streamScrollRef}
            onHistoryScroll={chat.handleStreamScroll}
            isLoadingHistory={chat.isLoadingHistory}
            historyScrollToken={sessionId}
          />
        </div>
      </section>
    </div>
  );
}

export default OfficialFreeChat;
