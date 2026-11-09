/**
 * Pipeline 详情卡片
 * 可视化展示单个 Pipeline：描述 / 输入参数 / 输出 / 节点执行链路
 */
import { Fragment } from 'react';
import { Tag, Tooltip } from 'antd';
import { ArrowRightOutlined, BranchesOutlined } from '@ant-design/icons';
import type { WorkflowDetail } from '@/utils/profileYaml';
import { NODE_TYPE_COLOR } from '../../constants';
import styles from '../../AgentPlayground.module.less';

function WorkflowDetailCard({ workflow }: { workflow: WorkflowDetail }) {
  return (
    <div className={styles.workflowCard}>
      <div className={styles.workflowName}>
        <BranchesOutlined /> {workflow.name}
      </div>
      {workflow.description && <div className={styles.workflowDesc}>{workflow.description}</div>}

      {/* 输入参数 */}
      {workflow.inputs.length > 0 && (
        <div className={styles.workflowMeta}>
          <div className={styles.workflowMetaLabel}>输入参数</div>
          {workflow.inputs.map((p) => (
            <div key={p.name} className={styles.paramRow}>
              <span className={styles.paramName}>
                {p.name}
                {p.required && <span className={styles.paramRequired}>*</span>}
              </span>
              {p.type && <Tag className={styles.paramTypeTag}>{p.type}</Tag>}
              {p.description && <span className={styles.paramDesc}>{p.description}</span>}
              {p.constraints && <span className={styles.paramConstraints}>{p.constraints}</span>}
            </div>
          ))}
        </div>
      )}

      {/* 输出 */}
      {workflow.outputs.length > 0 && (
        <div className={styles.workflowMeta}>
          <div className={styles.workflowMetaLabel}>输出</div>
          {workflow.outputs.map((o) => (
            <div key={o.name} className={styles.outputRow}>
              <span className={styles.paramName}>{o.name}</span>
              <span className={styles.outputValue}>{o.value}</span>
            </div>
          ))}
        </div>
      )}

      {/* 节点执行链路（按 YAML 定义顺序） */}
      {workflow.nodes.length > 0 && (
        <div className={styles.workflowMeta}>
          <div className={styles.workflowMetaLabel}>节点链路</div>
          <div className={styles.nodeFlow}>
            {workflow.nodes.map((n, i) => {
              const chip = (
                <span className={styles.nodeChip}>
                  <span className={styles.paramName}>{n.name}</span>
                  {n.type && (
                    <Tag color={NODE_TYPE_COLOR[n.type]} className={styles.paramTypeTag}>
                      {n.type}
                    </Tag>
                  )}
                  {n.tool && <span className={styles.nodeTool}>{n.tool}</span>}
                </span>
              );
              return (
                <Fragment key={n.name}>
                  {i > 0 && <ArrowRightOutlined className={styles.nodeArrow} />}
                  {/* 悬停展示节点完整定义（args/prompt/retry 等，仅 JSON 来源有 raw） */}
                  {n.raw ? (
                    <Tooltip
                      title={
                        <pre className={styles.nodeRawTip}>{JSON.stringify(n.raw, null, 2)}</pre>
                      }
                      placement="top"
                    >
                      {chip}
                    </Tooltip>
                  ) : (
                    chip
                  )}
                </Fragment>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

export default WorkflowDetailCard;
