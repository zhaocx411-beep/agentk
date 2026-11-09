/**
 * 工具详情页：执行历史（点击加载，样式对齐会话历史）
 */
import { Button, Empty, Pagination, Spin } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import type { CapabilityRunHistoryRecord, CapabilityRunResult } from '@/types/capabilityRuns';
import { formatUpdatedTime } from '@/utils/format';
import { CAPABILITY_RUN_HISTORY_KEY, getCapabilityRunHistory } from '../../api';
import { RunStatusTag } from '../../lib/runStatus';
import styles from '../../ToolGallery.module.less';

interface ToolRunHistoryProps {
  capabilityId: string;
  activeExecutionId?: string;
  loadingExecutionId?: string | null;
  /** 当前轮询中的执行结果，用于覆盖历史列表里的 stale 状态 */
  liveRunResult?: CapabilityRunResult | null;
  onSelect: (record: CapabilityRunHistoryRecord) => void;
  refreshKey?: number;
}

function ToolRunHistory({
  capabilityId,
  activeExecutionId,
  loadingExecutionId,
  liveRunResult,
  onSelect,
  refreshKey = 0,
}: ToolRunHistoryProps) {
  const [page, setPage] = useState(1);
  const pageSize = 10;

  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: [CAPABILITY_RUN_HISTORY_KEY, capabilityId, page, pageSize, refreshKey],
    queryFn: () =>
      getCapabilityRunHistory({
        capabilityId,
        page,
        pageSize,
      }),
  });

  return (
    <section className={styles.historySection}>
      <div className={styles.historyHead}>
        <div>
          <h2 className={styles.paramSectionTitle}>执行历史</h2>
          <p className={styles.paramSectionHint}>点击记录回显入参并加载结果</p>
        </div>
        <Button onClick={() => void refetch()} loading={isFetching}>
          刷新
        </Button>
      </div>

      {isLoading ? (
        <div className={styles.historyPaneLoading}>
          <Spin />
        </div>
      ) : !data?.records.length ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无执行记录" />
      ) : (
        <>
          <div className={styles.runHistoryList}>
            {data.records.map((record) => {
              const isActive = record.executionId === activeExecutionId;
              const isLoadingItem = record.executionId === loadingExecutionId;
              const status =
                liveRunResult && record.executionId === liveRunResult.executionId
                  ? liveRunResult.status
                  : record.status;
              const title = buildHistoryTitle(record);
              const timeText = formatUpdatedTime(record.createdAt);
              const summary = summarizeArgs(record.args);

              return (
                <div
                  key={record.executionId}
                  className={`${styles.runHistoryItem} ${isActive ? styles.runHistoryItemActive : ''}`}
                >
                  <div
                    className={styles.runHistoryItemBody}
                    role="button"
                    tabIndex={0}
                    title={`${timeText} · ${summary}`}
                    onClick={() => onSelect(record)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') onSelect(record);
                    }}
                  >
                    <div className={styles.runHistoryItemMain}>
                      <div className={styles.runHistoryTitle}>{timeText || title}</div>
                      {summary && summary !== '—' ? (
                        <div className={styles.runHistorySummary}>{summary}</div>
                      ) : null}
                    </div>
                    <div className={styles.runHistoryItemAside}>
                      {isLoadingItem ? <Spin size="small" /> : null}
                      <RunStatusTag status={status} bordered={false} />
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
          {data.total > pageSize ? (
            <Pagination
              simple
              current={page}
              pageSize={pageSize}
              total={data.total}
              onChange={setPage}
              className={styles.historyPagination}
            />
          ) : null}
        </>
      )}
    </section>
  );
}

function buildHistoryTitle(record: CapabilityRunHistoryRecord): string {
  const summary = summarizeArgs(record.args);
  if (summary && summary !== '—') return summary;
  return record.executionId;
}

function summarizeArgs(args: Record<string, unknown>): string {
  const parts = Object.entries(args).slice(0, 3).map(([key, value]) => {
    if (typeof value === 'string') {
      const text = value.length > 48 ? `${value.slice(0, 48)}…` : value;
      return `${key}: ${text}`;
    }
    if (Array.isArray(value)) return `${key}: [${value.length}]`;
    if (value && typeof value === 'object') return `${key}: {...}`;
    return `${key}: ${String(value)}`;
  });
  return parts.join(' · ') || '—';
}

export { ToolRunHistory };
