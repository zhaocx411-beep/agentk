/**
 * 工作流 Agent 详情顶栏：返回 / 名称 / 版本 / 保存与发布
 */
import { Button, Select, Tag } from 'antd';
import { ArrowLeftOutlined, CloudUploadOutlined, SaveOutlined } from '@ant-design/icons';
import { useGoBack } from '@/hooks';
import type { WorkflowVersion } from '@/types/workflowPersistence';
import { formatClock } from '@/utils/format';
import AgentAvatar from '../components/AgentAvatar';
import styles from '../AgentDetail/AgentDetail.module.less';

interface WorkflowDetailHeaderProps {
  name: string;
  description: string;
  status: 'published' | 'draft';
  versions: WorkflowVersion[];
  selectedVersionId?: number;
  dirty: boolean;
  saving: boolean;
  publishing: boolean;
  autoSavedAt?: Date | null;
  onVersionChange: (versionId: number) => void;
  onSaveDraft: () => void;
  onPublish: () => void;
}

function WorkflowDetailHeader({
  name,
  description,
  status,
  versions,
  selectedVersionId,
  dirty,
  saving,
  publishing,
  autoSavedAt,
  onVersionChange,
  onSaveDraft,
  onPublish,
}: WorkflowDetailHeaderProps) {
  const goBack = useGoBack('/agents/mine');
  const versionOptions = versions.map((v) => ({
    value: v.id,
    label: `v${v.version}${v.status === 'draft' ? '（草稿）' : ''}`,
  }));

  return (
    <div className={styles.workflowHeaderContent}>
      <div className={styles.workflowHeaderIdentity}>
        <Button type="text" icon={<ArrowLeftOutlined />} onClick={goBack} />
        <AgentAvatar size={36} kind="workflow" />
        <div className={styles.headerTitleBox}>
          <div className={styles.headerNameRow}>
            <span className={styles.headerName}>{name}</span>
            <Tag
              color={status === 'published' ? 'success' : 'default'}
              className={styles.headerTag}
            >
              {status === 'published' ? '已发布' : '草稿'}
            </Tag>
            <Tag className={styles.headerTag}>工作流Agent</Tag>
            <Select
              size="small"
              className={styles.versionSelect}
              value={selectedVersionId}
              placeholder="选择版本"
              options={versionOptions}
              onChange={onVersionChange}
              popupMatchSelectWidth={false}
              disabled={versionOptions.length === 0}
              aria-label="切换工作流版本"
            />
          </div>
          <div className={styles.headerSub}>
            {description || '暂无描述'}
            {autoSavedAt && (
              <span className={styles.autoSavedInline}>
                {' · '}
                自动保存于 {formatClock(autoSavedAt)}
              </span>
            )}
          </div>
        </div>
      </div>
      <div className={styles.headerRight}>
        <Button icon={<SaveOutlined />} disabled={!dirty} loading={saving} onClick={onSaveDraft}>
          保存草稿
        </Button>
        <Button
          type="primary"
          icon={<CloudUploadOutlined />}
          loading={publishing}
          onClick={onPublish}
        >
          更新发布
        </Button>
      </div>
    </div>
  );
}

export default WorkflowDetailHeader;
