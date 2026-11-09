/**
 * Agent-loop 生成过程：只读展示工具执行步骤
 */
import { useMemo } from 'react';
import { Empty, Tag } from 'antd';
import { formatElapsed } from '@/utils/format';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import {
  buildPipelineNodes,
  pipelineProgress,
} from '../../studio/lib/pipeline';
import { PIPELINE_NODE_STATUS_TEXT } from '../../studio/pipeline/pipelineNodeMeta';
import type { PipelineNode } from '../../studio/lib/types';
import styles from './OfficialGenerationProcess.module.less';

const DOT_CLASS: Record<PipelineNode['status'], string> = {
  done: styles.dotDone,
  running: styles.dotRunning,
  failed: styles.dotFailed,
  pending: styles.dotPending,
};

interface OfficialGenerationProcessProps {
  sseState: SSEState;
  compact?: boolean;
  /** 工作台空态由外层引导文案承担，不重复 Empty */
  hideEmpty?: boolean;
}

function OfficialGenerationProcess({
  sseState,
  compact = false,
  hideEmpty = false,
}: OfficialGenerationProcessProps) {
  const nodes = useMemo(() => buildPipelineNodes(sseState, null, ''), [sseState]);
  const progress = useMemo(() => pipelineProgress(nodes), [nodes]);
  const hasRun =
    sseState.messages.length > 0 ||
    sseState.isRunning ||
    sseState.toolCards.length > 0 ||
    sseState.jobs.length > 0;

  if (!hasRun || nodes.length === 0) {
    if (hideEmpty) return null;
    return (
      <div className={styles.empty}>
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="发送消息后，工具调用与执行进度会显示在这里"
        />
      </div>
    );
  }

  return (
    <section className={styles.panel} aria-label="生成过程">
      {!compact ? (
        <div className={styles.head}>
          <div>
            <div className={styles.eyebrow}>PIPELINE</div>
            <h3 className={styles.title}>生成过程</h3>
          </div>
          <span className={styles.meta}>
            {progress.done}/{progress.total}
          </span>
        </div>
      ) : null}
      <ol className={styles.list}>
        {nodes.map((node) => (
          <li key={node.key} className={styles.node}>
            <span className={`${styles.dot} ${DOT_CLASS[node.status]}`} aria-hidden />
            <span className={styles.index}>{node.index}</span>
            <span className={styles.main}>
              <span className={styles.nodeTitleRow}>
                <span className={styles.nodeTitle}>{node.title}</span>
                <Tag className={styles.badge}>{node.badge}</Tag>
              </span>
              {node.detail ? <span className={styles.detail}>{node.detail}</span> : null}
            </span>
            <span className={styles.status}>
              {PIPELINE_NODE_STATUS_TEXT[node.status]}
              {node.durationMs != null ? ` · ${formatElapsed(node.durationMs)}` : ''}
            </span>
          </li>
        ))}
      </ol>
    </section>
  );
}

export default OfficialGenerationProcess;
