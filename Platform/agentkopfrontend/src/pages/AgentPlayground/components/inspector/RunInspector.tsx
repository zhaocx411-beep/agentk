/**
 * 右栏：Run Inspector — 运行状态 + 事件记录 + Job 进度
 */
// import { useMemo } from 'react'; // TODO: Job 暂时不用
// import { useCallback } from 'react'; // TODO: Job 暂时不用
import { Badge, Divider } from 'antd';
import type { SSEState } from '../../hooks/useSSE';
import styles from '../../AgentPlayground.module.less';

interface RunInspectorProps {
  sseState: SSEState;
  width: number;
}

function inspectorRunStatus(state: SSEState) {
  if (state.isRunning) return { badge: 'processing' as const, text: '运行中' };
  if (state.error) return { badge: 'error' as const, text: '运行失败' };
  return { badge: 'success' as const, text: '已完成' };
}

function RunInspector({ sseState, width }: RunInspectorProps) {
  // TODO: Job 相关逻辑暂时注释，后续恢复
  // // Job 状态图标
  // const getJobStatusIcon = useCallback((status: string) => {
  //   switch (status) {
  //     case 'submitted':
  //       return <ClockCircleOutlined className={styles.jobIconPending} />;
  //     case 'completed':
  //       return <CheckCircleOutlined className={styles.jobIconOk} />;
  //     case 'failed':
  //       return <ExclamationCircleOutlined className={styles.jobIconErr} />;
  //     default:
  //       return null;
  //   }
  // }, []);
  //
  // // 按 kind 分组统计 Job 进度
  // const jobProgress = useMemo(() => {
  //   const groups = new Map<
  //     string,
  //     { total: number; completed: number; failed: number; submitted: number }
  //   >();
  //   for (const job of sseState.jobs) {
  //     const kind = job.kind || 'async';
  //     const g = groups.get(kind) ?? { total: 0, completed: 0, failed: 0, submitted: 0 };
  //     g.total += 1;
  //     if (job.status === 'completed') g.completed += 1;
  //     else if (job.status === 'failed') g.failed += 1;
  //     else if (job.status === 'submitted') g.submitted += 1;
  //     groups.set(kind, g);
  //   }
  //   return Array.from(groups.entries()).map(([kind, stat]) => ({ kind, ...stat }));
  // }, [sseState.jobs]);
  //
  // // Job 总体摘要
  // const jobSummary = useMemo(() => {
  //   const total = sseState.jobs.length;
  //   const completed = sseState.jobs.filter((j) => j.status === 'completed').length;
  //   const failed = sseState.jobs.filter((j) => j.status === 'failed').length;
  //   return { total, completed, failed, pending: total - completed - failed };
  // }, [sseState.jobs]);

  const runStatus = inspectorRunStatus(sseState);

  return (
    <aside className={styles.runPanel} style={{ width }}>
      <div className={styles.runPanelHeader}>
        <div className={styles.runPanelTitle}>本次运行</div>
      </div>
      <div className={styles.runPanelContent}>
        {sseState.currentRunId ? (
          <>
            <div className={styles.runStatus}>
              <Badge status={runStatus.badge} />
              <span>{runStatus.text}</span>
            </div>
            <div className={styles.runId}>Run {sseState.currentRunId.slice(-6)}</div>
          </>
        ) : (
          <div className={styles.runStatus}>
            <Badge status="default" />
            <span>等待发送消息</span>
          </div>
        )}
      </div>

      <Divider className={styles.runDivider} />

      <div className={styles.runPanelHeader}>
        <div className={styles.runPanelTitle}>实时执行记录</div>
      </div>
      <div className={styles.runPanelContent}>
        {/* Metric Cards */}
        <div className={styles.metricCards}>
          <div className={styles.metricCard}>
            <div className={styles.metricLabel}>Tokens</div>
            <div className={styles.metricValue}>{sseState.stats.inTok + sseState.stats.outTok}</div>
            <div className={styles.metricSub}>
              In {sseState.stats.inTok} / Out {sseState.stats.outTok}
            </div>
          </div>
          <div className={styles.metricCard}>
            <div className={styles.metricLabel}>Latency</div>
            <div className={styles.metricValue}>{sseState.stats.modelMs}ms</div>
            <div className={styles.metricSub}>模型总耗时</div>
          </div>
          <div className={styles.metricCard}>
            <div className={styles.metricLabel}>Tools</div>
            <div className={styles.metricValue}>{sseState.stats.tools}</div>
            <div className={styles.metricSub}>工具调用次数</div>
          </div>
        </div>

        {/* 事件记录 */}
        <div className={styles.ledgerSection}>
          <div className={styles.ledgerTitle}>事件记录</div>
          <div className={styles.ledger}>
            {sseState.ledger.length === 0 && <div className={styles.ledgerEmpty}>暂无记录</div>}
            {sseState.ledger.map((ev, idx) => (
              <div key={`${ev.seq}-${ev.kind}`} className={styles.ledgerRow}>
                <span className={styles.ledgerNum}>{idx + 1}</span>
                <span className={styles.ledgerKind}>{ev.kind}</span>
                <span className={styles.ledgerSum}>{ev.summary}</span>
              </div>
            ))}
          </div>
        </div>

        {/* 异步 Job（暂时隐藏，后续恢复） */}
        {/* <div className={styles.ledgerSection}>
          <div className={styles.ledgerTitle}>
            异步 Job
            {jobSummary.total > 0 && (
              <span className={styles.jobSummary}>
                {jobSummary.completed}/{jobSummary.total} 就绪
                {jobSummary.failed > 0 && (
                  <span className={styles.err}> · {jobSummary.failed} 失败</span>
                )}
              </span>
            )}
          </div>

          {jobProgress.length > 0 && (
            <div className={styles.jobProgressList}>
              {jobProgress.map((g) => {
                const pct = g.total > 0 ? (g.completed / g.total) * 100 : 0;
                const isClip = g.kind === 'generate_clip';
                return (
                  <div key={g.kind} className={styles.jobProgressItem}>
                    <div className={styles.jobProgressHead}>
                      <span className={styles.jobProgressKind}>
                        {isClip && <span className={styles.jobProgressIcon}>🎬</span>}
                        {g.kind}
                      </span>
                      <span
                        className={`${styles.jobProgressCount} ${
                          g.failed > 0 ? styles.err : g.completed === g.total ? styles.ok : ''
                        }`}
                      >
                        {g.completed}/{g.total}
                      </span>
                    </div>
                    <div className={styles.jobProgressBar}>
                      <div
                        className={`${styles.jobProgressFill} ${
                          g.failed > 0 ? styles.fillErr : styles.fillOk
                        }`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          <div className={styles.jobList}>
            {sseState.jobs.length === 0 && <div className={styles.jobEmpty}>当前无异步任务</div>}
            {sseState.jobs.map((job) => (
              <div key={job.jobId} className={styles.jobRow}>
                <span className={styles.jobStatus}>{getJobStatusIcon(job.status)}</span>
                <span className={styles.jobId}>{job.jobId.slice(-6)}</span>
                <span className={`${styles.jobKind} ${job.status === 'failed' ? styles.err : ''}`}>
                  {job.kind}
                </span>
              </div>
            ))}
          </div>
        </div> */}
      </div>
    </aside>
  );
}

export default RunInspector;
