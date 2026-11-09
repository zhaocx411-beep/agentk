/**
 * Pipeline 执行过程：POST 启动后轮询 GET /pipeline-runs/{execId} 的详情
 */
import { Empty, Spin, Tag } from 'antd';
import dayjs from 'dayjs';
import { formatElapsed } from '@/utils/format';
import type { WorkflowRunDetail } from '@/pages/AgentPlayground/types';
import { mapWorkflowRunStatus } from '../lib/pipeline';
import {
  PIPELINE_NODE_STATUS_CLASS,
  PIPELINE_NODE_STATUS_TEXT,
  PIPELINE_NODE_TAG_COLOR,
} from './pipelineNodeMeta';
import type { PipelineNode } from '../lib/types';
import styles from '../CreationStudio.module.less';

interface PipelineRunPanelProps {
  detail: WorkflowRunDetail | null;
  nodes: PipelineNode[];
  progress: { done: number; total: number };
  loading?: boolean;
}

function formatTime(value?: string): string {
  if (!value) return '—';
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed.format('MM-DD HH:mm:ss') : value;
}

function PipelineRunPanel({ detail, nodes, progress, loading = false }: PipelineRunPanelProps) {
  const status = mapWorkflowRunStatus(detail?.status);

  return (
    <section className={styles.runPanel}>
      <div className={styles.columnHead}>
        <span className={styles.columnEyebrow}>PIPELINE</span>
        <h2 className={styles.columnTitle}>执行过程</h2>
        {detail ? (
          <Tag color={PIPELINE_NODE_TAG_COLOR[status]} className={styles.runStatusTag}>
            {PIPELINE_NODE_STATUS_TEXT[status]}
          </Tag>
        ) : (
          <span className={styles.columnMeta}>
            {progress.done}/{progress.total}
          </span>
        )}
      </div>

      {loading && !detail ? (
        <div className={styles.runEmpty}>
          <Spin size="small" />
        </div>
      ) : !detail ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="点击生成后，将在此展示本次 Workflow 执行过程"
        />
      ) : (
        <>
          <dl className={styles.runMeta}>
            <div>
              <dt>Pipeline</dt>
              <dd>{detail.name || '—'}</dd>
            </div>
            <div>
              <dt>execId</dt>
              <dd className={styles.runMono}>{detail.id}</dd>
            </div>
            <div>
              <dt>开始</dt>
              <dd>{formatTime(detail.createdAt)}</dd>
            </div>
            <div>
              <dt>结束</dt>
              <dd>{formatTime(detail.finishedAt)}</dd>
            </div>
          </dl>

          {detail.error ? <pre className={styles.runError}>{detail.error}</pre> : null}

          <ol className={styles.nodeList}>
            {nodes.map((node) => (
              <li key={node.key}>
                <div className={styles.node}>
                  <span className={`${styles.statusDot} ${PIPELINE_NODE_STATUS_CLASS[node.status]}`} />
                  <span className={styles.nodeIndex}>{node.index}</span>
                  <span className={styles.nodeMain}>
                    <span className={styles.nodeTitleRow}>
                      <span className={styles.nodeTitle}>{node.title}</span>
                      <Tag className={styles.nodeBadge}>{node.badge}</Tag>
                    </span>
                    {node.detail ? <span className={styles.nodeDetail}>{node.detail}</span> : null}
                  </span>
                  <span className={styles.nodeStatus}>
                    {PIPELINE_NODE_STATUS_TEXT[node.status]}
                    {node.durationMs != null ? ` · ${formatElapsed(node.durationMs)}` : ''}
                  </span>
                </div>
              </li>
            ))}
          </ol>
        </>
      )}
    </section>
  );
}

export default PipelineRunPanel;
