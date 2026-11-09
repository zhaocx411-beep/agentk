/**
 * SessionHistoryList - 详情页右侧「历史会话」列表
 * 点击后在本页对话区加载账本；滚到底加载下一页
 */
import type { RefObject } from 'react';
import { Empty, Spin } from 'antd';
import {
  RunStatusTag,
  SessionHistoryRow,
  SessionTitleRenameButton,
  useSessionTitleRename,
} from '@/components';
import type { Session } from '@/pages/AgentPlayground/types';
import { formatUpdatedTime } from '@/utils/format';
import styles from '../AgentDetail.module.less';

interface SessionHistoryListProps {
  versionMap: Record<number, number>;
  currentVersionId?: number;
  currentSessionId: string | null;
  sessions: Session[];
  loading: boolean;
  isLoadingMoreSessions?: boolean;
  historyListRef?: RefObject<HTMLDivElement>;
  onHistoryScroll?: () => void;
  onOpenSession: (sessionId: string, meta?: Session) => void;
  onSessionTitleUpdated?: (sessionId: string, title: string) => void;
}

function SessionHistoryList({
  versionMap,
  currentVersionId,
  currentSessionId,
  sessions,
  loading,
  isLoadingMoreSessions = false,
  historyListRef,
  onHistoryScroll,
  onOpenSession,
  onSessionTitleUpdated,
}: SessionHistoryListProps) {
  const { openRename, renameModal } = useSessionTitleRename(onSessionTitleUpdated);

  if (loading) {
    return (
      <div className={styles.historyPane}>
        <Spin />
      </div>
    );
  }

  if (sessions.length === 0) {
    return (
      <div className={styles.historyPane}>
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="暂无历史会话，在「对话」中发送第一条消息即可"
        />
      </div>
    );
  }

  return (
    <div className={styles.historyPane}>
      <div className={styles.historyHint}>点击会话将在右侧对话区加载历史记录</div>
      <div ref={historyListRef} className={styles.historyList} onScroll={onHistoryScroll}>
        {sessions.map((s) => {
          const versionNo = s.agentVersionId != null ? versionMap[s.agentVersionId] : undefined;
          const isCurrentVersion =
            currentVersionId != null && s.agentVersionId === currentVersionId;
          const isActive = s.id === currentSessionId;
          const timeText = formatUpdatedTime(s.createdAt);
          return (
            <SessionHistoryRow
              key={s.id}
              className={`${styles.historyItem} ${isActive ? styles.historyItemActive : ''}`}
            >
              <div
                className={styles.historyItemBody}
                role="button"
                tabIndex={0}
                title={s.title || s.id}
                onClick={() => onOpenSession(s.id, s)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') onOpenSession(s.id, s);
                }}
              >
                <div className={styles.historyItemMain}>
                  <div className={styles.historyTitle}>{s.title || s.id}</div>
                  <div className={styles.historyMeta}>
                    {versionNo != null && (
                      <span
                        className={
                          isCurrentVersion ? styles.historyVersionCurrent : styles.historyVersion
                        }
                      >
                        v{versionNo}
                        {isCurrentVersion ? ' · 本版本' : ''}
                      </span>
                    )}
                    {timeText && <span className={styles.historyTime}>{timeText}</span>}
                  </div>
                </div>
                <RunStatusTag status={s.activeRunStatus} />
              </div>
              {onSessionTitleUpdated ? (
                <SessionTitleRenameButton session={s} onRename={openRename} />
              ) : null}
            </SessionHistoryRow>
          );
        })}
        {isLoadingMoreSessions && <div className={styles.historyLoading}>加载中...</div>}
      </div>
      {renameModal}
    </div>
  );
}

export default SessionHistoryList;
