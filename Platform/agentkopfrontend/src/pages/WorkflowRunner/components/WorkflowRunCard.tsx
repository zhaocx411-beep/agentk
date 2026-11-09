/**
 * PipelineRunCard - 对话区中展示 Pipeline 运行状态的卡片
 * 智能渲染输出：视频用 <video>、图片用 <img>、其余 JSON
 */
import { Tag } from 'antd';
import {
  ApartmentOutlined,
  PlayCircleOutlined,
  FileImageOutlined,
  RightOutlined,
  DownOutlined,
} from '@ant-design/icons';
import type { WorkflowRun } from '@/pages/AgentPlayground/types';
import { NODE_STATUS_META, PIPELINE_RUN_STATUS_META } from '@/constants/workflowNodeStatus';
import { dedupeRunNodes, extractMediaUrls } from '../utils';
import styles from './WorkflowRunCard.module.less';

/** 渲染媒体输出区域 */
function RenderMediaOutput({ output }: { output: unknown }) {
  const { videos, images, textFields } = extractMediaUrls(output);

  return (
    <>
      {/* 视频列表 */}
      {videos.length > 0 && (
        <div className={styles.mediaSection}>
          <div className={styles.mediaLabel}>
            <PlayCircleOutlined className={styles.mediaIcon} />
            生成视频
            <span className={styles.mediaCount}>{videos.length}</span>
          </div>
          <div className={styles.videoGrid}>
            {videos.map((url, i) => (
              <div key={i} className={styles.videoItem}>
                <video src={url} controls preload="metadata" className={styles.videoPlayer} />
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 图片列表 */}
      {images.length > 0 && (
        <div className={styles.mediaSection}>
          <div className={styles.mediaLabel}>
            <FileImageOutlined className={styles.mediaIcon} />
            生成图片
            <span className={styles.mediaCount}>{images.length}</span>
          </div>
          <div className={styles.imageGrid}>
            {images.map((url, i) => (
              <div key={i} className={styles.imageItem}>
                <img src={url} alt={`output-${i}`} className={styles.imagePreview} />
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 剩余文本字段 */}
      {Object.keys(textFields).length > 0 && (
        <pre className={styles.outputPre}>{JSON.stringify(textFields, null, 2)}</pre>
      )}
    </>
  );
}

interface WorkflowRunCardProps {
  run: WorkflowRun;
}

function WorkflowRunCard({ run }: WorkflowRunCardProps) {
  const overallStatus = run.status;
  const statusMeta = PIPELINE_RUN_STATUS_META[overallStatus];
  const dedupedNodes = dedupeRunNodes(run.nodes);

  return (
    <div className={styles.card}>
      {/* 头部：Pipeline 名称 + run ID + 整体状态 */}
      <div className={styles.head}>
        <ApartmentOutlined className={styles.headIcon} />
        <span className={styles.wfName}>{run.workflowName}</span>
        <span className={styles.runId}>{run.runId.slice(-8)}</span>
        <Tag color={statusMeta.color} className={styles.statusTag}>
          {statusMeta.text}
        </Tag>
      </div>

      {/* 触发参数 */}
      <div className={styles.params}>
        <div className={styles.paramsSummary}>触发参数</div>
        <pre className={styles.paramsPre}>{JSON.stringify(run.params, null, 2)}</pre>
      </div>

      {/* 节点状态列表 — 每个节点可独立展开查看详情 */}
      {dedupedNodes.length > 0 && (
        <div className={styles.nodes}>
          {dedupedNodes.map((node) => {
            const cfg = NODE_STATUS_META[node.status];
            const hasOutput = node.output != null;
            return (
              <details key={`${node.name}-${node.label}`} className={styles.nodeRow}>
                <summary className={styles.nodeSummary}>
                  <span className={styles.nodeToggleIcon}>
                    <RightOutlined className={styles.iconCollapsed} />
                    <DownOutlined className={styles.iconExpanded} />
                  </span>
                  <span
                    className={styles.nodeIcon}
                    style={{
                      color:
                        cfg.color === 'default'
                          ? '#999'
                          : cfg.color === 'processing'
                            ? '#1677ff'
                            : cfg.color === 'success'
                              ? '#10b981'
                              : '#ef4444',
                    }}
                  >
                    {cfg.icon}
                  </span>
                  <span className={styles.nodeName}>{node.label || node.name}</span>
                  <span className={`${styles.nodeStatus} ${styles[`nodeStatus_${node.status}`]}`}>
                    {cfg.label}
                  </span>
                  {node.duration != null && (
                    <span className={styles.nodeDuration}>{node.duration}ms</span>
                  )}
                  {node.error && <span className={styles.nodeError}>{node.error}</span>}
                </summary>
                {hasOutput && (
                  <div className={styles.nodeDetailContent}>
                    <RenderMediaOutput output={node.output} />
                  </div>
                )}
              </details>
            );
          })}
        </div>
      )}

      {/* 顶层输出：视频/图片/文本 智能渲染 */}
      {run.status === 'completed' && run.output != null && (
        <div className={styles.outputSection}>
          <div className={styles.outputTitle}>输出结果</div>
          <RenderMediaOutput output={run.output} />
        </div>
      )}

      {/* 错误信息 */}
      {run.error && <div className={styles.errorBar}>{run.error}</div>}
    </div>
  );
}

export default WorkflowRunCard;
