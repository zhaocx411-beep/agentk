/**
 * 我的生成：支持按 Prompt 搜索与状态筛选。
 * 列表均来自 sessions/query（自主规划 / 工作流），历史主要用于切换会话。
 * 两种模式都只展示「当前打开会话」的实时状态；非当前条不展示，避免 sessions/query 误导。
 */
import type { RefObject } from 'react';
import { useMemo, useState } from 'react';
import { Empty, Input, Select, Spin, Tag } from 'antd';
import dayjs from 'dayjs';
import type { GenerationRecord } from '../lib/types';
import styles from '../CreationStudio.module.less';

type KnownStatus = Exclude<GenerationRecord['status'], null>;
type StatusFilter = 'all' | KnownStatus;

const STATUS_TEXT: Record<KnownStatus, string> = {
  running: '生成中',
  waiting: '待确认',
  done: '成功',
  failed: '失败',
  cancelled: '已停止',
};

const STATUS_CLASS: Record<KnownStatus, string> = {
  running: styles.statusRunning,
  waiting: styles.statusWaiting,
  done: styles.statusDone,
  failed: styles.statusFailed,
  cancelled: styles.statusCancelled,
};

const FILTER_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: '全部状态' },
  ...Object.entries(STATUS_TEXT).map(([value, label]) => ({
    value: value as KnownStatus,
    label,
  })),
];

interface GenerationHistoryProps {
  records: GenerationRecord[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  /**
   * 传入时只给这一条展示状态（当前打开的会话）。
   * sessions/query 无法可靠给出每条对话内的成败，避免误导。
   */
  statusSessionId?: string | null;
  /** 是否启用 sessions/query 滚动翻页（自主规划） */
  enableScrollPaging?: boolean;
  sessionsLoading?: boolean;
  isLoadingMoreSessions?: boolean;
  sessionsHasMore?: boolean;
  historyListRef?: RefObject<HTMLDivElement>;
  onHistoryScroll?: () => void;
  showHeader?: boolean;
}

function GenerationHistory({
  records,
  selectedId,
  onSelect,
  statusSessionId = null,
  enableScrollPaging = false,
  sessionsLoading = false,
  isLoadingMoreSessions = false,
  sessionsHasMore = false,
  historyListRef,
  onHistoryScroll,
  showHeader = true,
}: GenerationHistoryProps) {
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const statusOnlyCurrent = !!statusSessionId;

  const visible = useMemo(() => {
    const text = keyword.trim().toLowerCase();
    return records.filter((record) => {
      // 仅当前会话有可信状态时，不做全表状态筛选
      if (!statusOnlyCurrent && status !== 'all' && record.status !== status) return false;
      if (!text) return true;
      return record.prompt.toLowerCase().includes(text);
    });
  }, [records, keyword, status, statusOnlyCurrent]);

  return (
    <section className={`${styles.card} ${styles.historyCard}`}>
      {showHeader ? (
        <div className={styles.cardHead}>
          <h2 className={styles.cardTitle}>我的生成</h2>
          <span className={styles.cardHint}>{records.length} 条</span>
        </div>
      ) : null}

      {records.length > 0 && (
        <div className={styles.historyFilter}>
          <Input.Search
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索 Prompt"
            allowClear
          />
          {!statusOnlyCurrent && (
            <Select<StatusFilter>
              value={status}
              onChange={setStatus}
              options={FILTER_OPTIONS}
              className={styles.historyStatusSelect}
            />
          )}
        </div>
      )}

      <div
        ref={enableScrollPaging ? historyListRef : undefined}
        className={styles.recordScroll}
        onScroll={enableScrollPaging ? onHistoryScroll : undefined}
      >
        {sessionsLoading && records.length === 0 ? (
          <div className={styles.recordScrollStatus}>
            <Spin size="small" />
          </div>
        ) : visible.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={records.length === 0 ? '还没有生成记录' : '没有匹配的记录'}
          />
        ) : (
          <ul className={styles.recordList}>
            {visible.map((record) => {
              const selected = record.id === selectedId;
              const knownStatus = record.status;
              // 仅当前会话展示状态，且账本未回时 status 为 null，不显示默认值
              const showStatus =
                knownStatus != null && (!statusOnlyCurrent || record.id === statusSessionId);
              return (
                <li
                  key={record.id}
                  className={`${styles.recordItem} ${selected ? styles.recordSelected : ''}`}
                  aria-current={selected}
                  onClick={() => onSelect(record.id)}
                >
                  <div className={styles.recordHead}>
                    {showStatus && knownStatus && (
                      <>
                        <span className={`${styles.statusDot} ${STATUS_CLASS[knownStatus]}`} />
                        <span className={styles.recordStatus}>{STATUS_TEXT[knownStatus]}</span>
                      </>
                    )}
                    <Tag className={styles.softTag}>{record.scene}</Tag>
                    <span className={styles.recordTime}>
                      {dayjs(record.createdAt).format('HH:mm:ss')}
                    </span>
                  </div>
                  <p className={styles.recordPrompt}>{record.prompt}</p>
                  {showStatus && record.error && (
                    <p className={styles.recordError}>{record.error}</p>
                  )}
                </li>
              );
            })}
          </ul>
        )}

        {enableScrollPaging && isLoadingMoreSessions && (
          <div className={styles.recordScrollStatus}>加载中…</div>
        )}
        {enableScrollPaging &&
          !sessionsLoading &&
          !isLoadingMoreSessions &&
          !sessionsHasMore &&
          records.length > 0 && <div className={styles.recordScrollStatus}>已加载全部</div>}
      </div>
    </section>
  );
}

export default GenerationHistory;
