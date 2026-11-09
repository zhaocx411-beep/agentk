import { Button, Empty, Spin } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';
import { AGENT100_HISTORY_QUERY_KEY, listAgent100Executions } from '../api';
import { AGENT100_TEXT } from '../mock';
import { formatUpdatedTime } from '@/utils/format';
import { mergeLiveHistoryItem } from '../executionView';
import { useHistoryStatusSync } from '../useHistoryStatusSync';
import { Agent100RunStatusTag } from '../runStatus';
import type { Agent100Execution, Agent100HistoryItem } from '../types';
import styles from '../Agent100.module.less';

interface ExecutionHistoryProps {
  agentId: string;
  pipeline: string;
  activeExecutionId?: string;
  loadingExecutionId?: string | null;
  liveRunResult?: Agent100Execution | null;
  refreshKey?: number;
  onSelect: (record: Agent100HistoryItem) => void;
}

function historyTime(value?: string): string {
  if (!value) return '';
  return formatUpdatedTime(value.replace(' ', 'T')) || value;
}

function ExecutionHistory({
  agentId,
  pipeline,
  activeExecutionId,
  loadingExecutionId,
  liveRunResult,
  refreshKey = 0,
  onSelect,
}: ExecutionHistoryProps) {
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: [...AGENT100_HISTORY_QUERY_KEY, agentId],
    queryFn: () => listAgent100Executions(agentId),
    enabled: Boolean(agentId),
  });

  useEffect(() => {
    if (refreshKey > 0) void refetch();
  }, [refreshKey, refetch]);

  useHistoryStatusSync(agentId, data?.items ?? [], liveRunResult?.executionId);

  const records = mergeLiveHistoryItem(
    (data?.items ?? []).filter((item) => item.pipeline === pipeline),
    liveRunResult,
    pipeline,
  );

  return (
    <section className={styles.historySection}>
      <div className={styles.historyHead}>
        <div>
          <h2 className={styles.sectionTitle}>{AGENT100_TEXT.historyTitle}</h2>
          <p className={styles.sectionHint}>{AGENT100_TEXT.historyHint}</p>
        </div>
        <Button onClick={() => void refetch()} loading={isFetching}>
          刷新
        </Button>
      </div>

      {isLoading ? (
        <div className={styles.historyPaneLoading}>
          <Spin />
        </div>
      ) : records.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={AGENT100_TEXT.historyEmpty} />
      ) : (
        <div className={styles.runHistoryList}>
          {records.map((record) => {
            const isActive = record.executionId === activeExecutionId;
            const isLoadingItem = record.executionId === loadingExecutionId;
            const status =
              liveRunResult && record.executionId === liveRunResult.executionId
                ? liveRunResult.status
                : record.status;
            const timeText = historyTime(record.createdAt || record.updatedAt);
            const title = record.title?.trim() || timeText || record.executionId;

            return (
              <div
                key={record.executionId}
                className={`${styles.runHistoryItem} ${isActive ? styles.runHistoryItemActive : ''}`}
              >
                <div
                  className={styles.runHistoryItemBody}
                  role="button"
                  tabIndex={0}
                  onClick={() => onSelect(record)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') onSelect(record);
                  }}
                >
                  <div className={styles.runHistoryItemMain}>
                    <div className={styles.runHistoryTitle}>{title}</div>
                    {timeText && timeText !== title ? (
                      <div className={styles.runHistorySummary}>{timeText}</div>
                    ) : null}
                  </div>
                  <div className={styles.runHistoryItemAside}>
                    {isLoadingItem ? <Spin size="small" /> : null}
                    <Agent100RunStatusTag status={status} bordered={false} />
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

export { ExecutionHistory };
