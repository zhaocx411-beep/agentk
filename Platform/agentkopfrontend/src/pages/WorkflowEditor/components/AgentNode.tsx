import { memo, useCallback } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Tag, Tooltip } from 'antd';
import { DeleteOutlined } from '@ant-design/icons';
import type { AgentNodeData, AgentNodeType } from '../types';
import { AGENT_NODE_TYPE_CONFIG, RUN_STATUS_COLOR, RUN_STATUS_LABEL } from '../constants';
import { useWorkflowStore } from '../store';
import styles from './AgentNode.module.less';

function AgentNodeComponent({ id, data, selected }: NodeProps & { data: AgentNodeData }) {
  const config = AGENT_NODE_TYPE_CONFIG[data.type as AgentNodeType] ?? {
    label: data.type,
    color: '#999',
    icon: '?',
  };

  // 读取画布节点运行状态（由 WorkflowPanel 调试运行时写入 store）
  const runStatus = useWorkflowStore((s) => s.nodeRunStatuses[id]) ?? 'idle';
  // 合并：优先使用运行状态，其次使用 data.status
  const status = runStatus !== 'idle' ? runStatus : data.status || 'idle';

  const handleDelete = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      // 动态导入避免循环依赖
      import('../store').then(({ useWorkflowStore }) => {
        useWorkflowStore.getState().deleteNode(id);
      });
    },
    [id],
  );

  return (
    <div
      className={`${styles.agentNode} ${selected ? styles.selected : ''} ${
        styles[`status_${status}`] || ''
      }`}
      style={{ borderColor: config.color }}
    >
      {/* 顶部 Handle（输入） */}
      {data.type !== 'start' && (
        <Handle type="target" position={Position.Top} className={styles.handle} />
      )}

      {/* 节点头部 */}
      <div className={styles.header} style={{ background: config.color }}>
        <span className={styles.icon}>{config.icon}</span>
        <span className={styles.typeLabel}>{config.label}</span>
        {status !== 'idle' && (
          <Tag color={RUN_STATUS_COLOR[status]} className={styles.statusTag}>
            {RUN_STATUS_LABEL[status]}
          </Tag>
        )}
      </div>

      {/* 节点内容 */}
      <div className={styles.body}>
        <div className={styles.label}>{data.label}</div>
        {data.description && (
          <Tooltip title={data.description}>
            <div className={styles.description}>{data.description}</div>
          </Tooltip>
        )}
        {data.type === 'llm' && data.model && (
          <Tag color="blue" className={styles.metaTag}>
            {data.model}
          </Tag>
        )}
        {data.type === 'tool' && data.toolNames && data.toolNames.length > 0 && (
          <div className={styles.toolTags}>
            {data.toolNames.map((t) => (
              <Tag key={t} color="orange" className={styles.metaTag}>
                {t}
              </Tag>
            ))}
          </div>
        )}
        {data.type === 'condition' && data.conditionExpr && (
          <code className={styles.conditionCode}>{data.conditionExpr}</code>
        )}
        {data.outputVar && data.type !== 'start' && data.type !== 'end' && (
          <div className={styles.outputVar}>
            <span className={styles.outputVarLabel}>→</span>
            <code>{data.outputVar}</code>
          </div>
        )}
      </div>

      {/* 删除按钮 */}
      {data.type !== 'start' && data.type !== 'end' && (
        <button className={styles.deleteBtn} onClick={handleDelete} type="button">
          <DeleteOutlined />
        </button>
      )}

      {/* 底部 Handle（输出） */}
      {data.type !== 'end' && (
        <>
          <Handle type="source" position={Position.Bottom} className={styles.handle} id="default" />
          {/* 条件节点额外输出 */}
          {data.type === 'condition' && (
            <>
              <Handle
                type="source"
                position={Position.Bottom}
                id="yes"
                className={`${styles.handle} ${styles.handleYes}`}
                style={{ left: '30%' }}
              />
              <Handle
                type="source"
                position={Position.Bottom}
                id="no"
                className={`${styles.handle} ${styles.handleNo}`}
                style={{ left: '70%' }}
              />
            </>
          )}
        </>
      )}
    </div>
  );
}

export const AgentNodeComponentMemo = memo(AgentNodeComponent);
