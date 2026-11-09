/**
 * Pipeline 版本详情弹窗
 * definitionJson 可视化展示（复用 WorkflowDetailCard），另附格式化原始 JSON
 */
import { useMemo } from 'react';
import { Alert, Modal, Tabs, Tag, Empty } from 'antd';
import type { WorkflowVersion } from '@/types/workflowPersistence';
import { prettyDefinitionJson, workflowDetailsFromJson } from '@/utils/workflowDefinition';
import WorkflowDetailCard from '@/pages/AgentPlayground/components/workflow/WorkflowDetailCard';
import styles from '../WorkflowRunner.module.less';

interface WorkflowVersionDetailModalProps {
  /** 当前查看的版本；null 表示关闭 */
  version: WorkflowVersion | null;
  onClose: () => void;
}

function WorkflowVersionDetailModal({ version, onClose }: WorkflowVersionDetailModalProps) {
  const details = useMemo(
    () => (version ? workflowDetailsFromJson(version.definitionJson) : []),
    [version],
  );

  if (!version) return null;

  // 存在未声明 nodes 的 Pipeline：后端会拒绝运行，详情里明确提示
  const noNodeNames = details.filter((d) => d.nodes.length === 0).map((d) => d.name);

  return (
    <Modal
      title={
        <span>
          版本详情 v{version.version}{' '}
          {version.status === 'published' ? <Tag color="success">已发布</Tag> : <Tag>草稿</Tag>}
        </span>
      }
      open
      onCancel={onClose}
      footer={null}
      width={720}
      classNames={{ body: styles.detailModalBody }}
    >
      <Tabs
        items={[
          {
            key: 'overview',
            label: '配置总览',
            children:
              details.length > 0 ? (
                <div className={styles.detailWorkflows}>
                  {noNodeNames.length > 0 && (
                    <Alert
                      type="warning"
                      showIcon
                      message={`pipeline「${noNodeNames.join('」、「')}」未定义 nodes，该版本不可运行，请补全定义后另存新草稿`}
                    />
                  )}
                  {details.map((d) => (
                    <WorkflowDetailCard key={d.name} workflow={d} />
                  ))}
                </div>
              ) : (
                <Empty description="定义为空或无法解析，请查看原始 JSON" />
              ),
          },
          {
            key: 'json',
            label: '原始 JSON',
            children: (
              <pre className={styles.jsonPreview}>
                {prettyDefinitionJson(version.definitionJson)}
              </pre>
            ),
          },
        ]}
      />
    </Modal>
  );
}

export default WorkflowVersionDetailModal;
