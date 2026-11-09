/**
 * 执行链路：把一次生成拆成可点选的步骤，点开在右侧看输入输出
 */
import { Tag } from 'antd';
import { formatElapsed } from '@/utils/format';
import type { PipelineNode } from '../lib/types';
import { PIPELINE_NODE_STATUS_CLASS, PIPELINE_NODE_STATUS_TEXT } from './pipelineNodeMeta';
import styles from '../CreationStudio.module.less';

interface PipelinePanelProps {
  nodes: PipelineNode[];
  activeKey: string | null;
  onSelect: (key: string) => void;
  progress: { done: number; total: number };
}

function PipelinePanel({ nodes, activeKey, onSelect, progress }: PipelinePanelProps) {
  return (
    <section className={styles.column}>
      <div className={styles.columnHead}>
        <span className={styles.columnEyebrow}>PIPELINE</span>
        <h2 className={styles.columnTitle}>执行链路</h2>
        <span className={styles.columnMeta}>
          {progress.done}/{progress.total}
        </span>
      </div>
      <ol className={styles.nodeList}>
        {nodes.map((node) => (
          <li key={node.key}>
            <button
              type="button"
              className={`${styles.node} ${activeKey === node.key ? styles.nodeActive : ''}`}
              onClick={() => onSelect(node.key)}
            >
              <span className={`${styles.statusDot} ${PIPELINE_NODE_STATUS_CLASS[node.status]}`} />
              <span className={styles.nodeIndex}>{node.index}</span>
              <span className={styles.nodeMain}>
                <span className={styles.nodeTitleRow}>
                  <span className={styles.nodeTitle}>{node.title}</span>
                  <Tag className={styles.nodeBadge}>{node.badge}</Tag>
                </span>
                {node.detail && <span className={styles.nodeDetail}>{node.detail}</span>}
              </span>
              <span className={styles.nodeStatus}>
                {PIPELINE_NODE_STATUS_TEXT[node.status]}
                {node.durationMs != null && ` · ${formatElapsed(node.durationMs)}`}
              </span>
            </button>
          </li>
        ))}
      </ol>
    </section>
  );
}

export default PipelinePanel;
