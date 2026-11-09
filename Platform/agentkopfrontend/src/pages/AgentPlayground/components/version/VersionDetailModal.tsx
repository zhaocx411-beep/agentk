/**
 * 版本详情弹窗
 * 把版本 Profile object 可视化展示，另附格式化原始 JSON 供完整核对
 */
import { Modal, Tabs, Tag } from 'antd';
import type { AgentLoopVersion } from '@/types/agentLoop';
import VersionConfigOverview from './VersionConfigOverview';
import styles from '../../AgentPlayground.module.less';

interface VersionDetailModalProps {
  /** 当前查看的版本；null 表示关闭 */
  version: AgentLoopVersion | null;
  onClose: () => void;
}

function VersionDetailModal({ version, onClose }: VersionDetailModalProps) {
  if (!version) return null;

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
            children: <VersionConfigOverview profile={version.profileYaml} />,
          },
          {
            key: 'json',
            label: '原始 JSON',
            children: (
              <pre className={styles.versionYamlPreview}>
                {JSON.stringify(version.profileYaml, null, 2)}
              </pre>
            ),
          },
        ]}
      />
    </Modal>
  );
}

export default VersionDetailModal;
