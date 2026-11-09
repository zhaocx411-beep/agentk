import { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Empty, Spin } from 'antd';
import { getPipelineExecsByAgentkSession } from '../api';
import {
  pipelineExecsToReviewMessages,
  sessionEventsToReviewMessages,
  summarizePipelineExecs,
  summarizeSessionEvents,
} from '../conversationUtils';
import { useReviewSessionEvents } from '../hooks/useReviewSessionEvents';
import type { PipelineExecRecord } from '../types';
import { ConversationMessages } from './ConversationMessages';
import styles from '../SessionReview.module.less';

export interface ConversationReviewRecord {
  publicId: string;
  agentkSessionId?: string;
  title?: string;
  sessionKind?: string;
  effectiveModel?: string;
}

interface ConversationReviewContentProps {
  record: ConversationReviewRecord;
}

export function ConversationReviewContent({ record }: ConversationReviewContentProps) {
  const threadRef = useRef<HTMLDivElement | null>(null);
  const shouldScrollToBottomRef = useRef(false);
  const [pipelineLoading, setPipelineLoading] = useState(false);
  const [pipelineError, setPipelineError] = useState('');
  const [pipelineExecs, setPipelineExecs] = useState<PipelineExecRecord[] | null>(null);
  const {
    events,
    loading: eventsLoading,
    loadingOlder,
    error: eventsError,
    total,
    loadInitial,
    reset: resetEvents,
    handleScroll,
  } = useReviewSessionEvents();
  const isWorkflow = record.sessionKind === 'workflow';

  useEffect(() => {
    const sessionId = record.agentkSessionId?.trim();
    if (!sessionId) {
      resetEvents();
      setPipelineExecs(null);
      setPipelineError('该会话没有 agentkSessionId，无法拉取历史记录');
      setPipelineLoading(false);
      return;
    }

    if (!isWorkflow) {
      setPipelineExecs(null);
      setPipelineError('');
      setPipelineLoading(false);
      shouldScrollToBottomRef.current = true;
      void loadInitial(sessionId);
      return () => resetEvents();
    }

    resetEvents();
    let cancelled = false;
    setPipelineLoading(true);
    setPipelineError('');
    setPipelineExecs(null);
    getPipelineExecsByAgentkSession(sessionId)
      .then((data) => {
        if (!cancelled) setPipelineExecs(Array.isArray(data) ? data : []);
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setPipelineError(error instanceof Error ? error.message : '拉取 Pipeline 账本失败');
        }
      })
      .finally(() => {
        if (!cancelled) setPipelineLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isWorkflow, loadInitial, record.agentkSessionId, resetEvents]);

  useEffect(() => {
    if (isWorkflow || eventsLoading || events.length === 0) return;
    if (!shouldScrollToBottomRef.current || !threadRef.current) return;
    shouldScrollToBottomRef.current = false;
    requestAnimationFrame(() => {
      if (threadRef.current) threadRef.current.scrollTop = threadRef.current.scrollHeight;
    });
  }, [events.length, eventsLoading, isWorkflow]);

  const messages = useMemo(
    () =>
      isWorkflow
        ? pipelineExecsToReviewMessages(pipelineExecs ?? [])
        : sessionEventsToReviewMessages(events),
    [events, isWorkflow, pipelineExecs],
  );
  const loading = isWorkflow ? pipelineLoading : eventsLoading;
  const error = isWorkflow ? pipelineError : eventsError;
  const runSummary = isWorkflow
    ? summarizePipelineExecs(pipelineExecs ?? [])
    : summarizeSessionEvents(events, total);
  const hasLoadedData = isWorkflow ? pipelineExecs !== null : events.length > 0;

  return (
    <section className={styles.conversationPagePanel} aria-label="对话内容">
      <header className={styles.chatHeader}>
        <h2 className={styles.chatTitle}>
          {record.title || record.publicId}
          <span className={styles.chatTitleSub}>{isWorkflow ? 'Pipeline 回看' : 'Agent 对话'}</span>
        </h2>
        <div className={styles.chatMetaChips}>
          {record.sessionKind ? <span className={styles.metaChip}>{record.sessionKind}</span> : null}
          {record.agentkSessionId ? (
            <span className={styles.metaChip} title={record.agentkSessionId}>
              {record.agentkSessionId}
            </span>
          ) : null}
          {record.effectiveModel ? (
            <span className={styles.metaChip}>{record.effectiveModel}</span>
          ) : null}
          {!loading && !error && hasLoadedData ? (
            <span className={styles.metaChip}>{runSummary}</span>
          ) : null}
          {!loading && !error && messages.length > 0 ? (
            <span className={styles.metaChipAccent}>{messages.length} 条消息</span>
          ) : null}
        </div>
      </header>

      {error ? <Alert type="warning" showIcon message={error} className={styles.conversationAlert} /> : null}
      <div className={styles.conversationPageBody}>
        {loading ? (
          <div className={styles.conversationLoading}>
            <Spin />
            <span className={styles.loadingText}>
              {isWorkflow ? '加载 Pipeline 账本…' : '加载对话历史…'}
            </span>
          </div>
        ) : !error && messages.length === 0 ? (
          <div className={styles.conversationEmpty}>
            <Empty description={isWorkflow ? '暂无 Pipeline 执行记录' : '暂无对话消息'} />
          </div>
        ) : (
          <ConversationMessages
            messages={messages}
            loadingOlder={loadingOlder}
            isWorkflow={isWorkflow}
            threadRef={threadRef}
            onScroll={handleScroll}
          />
        )}
      </div>
    </section>
  );
}
