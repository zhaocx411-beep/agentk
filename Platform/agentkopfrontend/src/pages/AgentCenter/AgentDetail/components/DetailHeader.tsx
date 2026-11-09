/**
 * DetailHeader - 详情页顶部栏
 * 返回 + 名称 / 状态 / 版本切换 / 副标题展示「自动保存于」/ 保存与发布
 */
import { Button, Select, Tag, Tooltip } from 'antd';
import { ArrowLeftOutlined, CloudUploadOutlined, SaveOutlined } from '@ant-design/icons';
import { useGoBack } from '@/hooks';
import type { AgentLoopVersion } from '@/types/agentLoop';
import { formatClock } from '@/utils/format';
import type { AgentDetailData } from '../../types';
import { AGENT_STATUS_META } from '../../constants';
import styles from '../AgentDetail.module.less';

interface DetailHeaderProps {
  detail: AgentDetailData;
  versions: AgentLoopVersion[];
  selectedVersionId?: number;
  dirty: boolean;
  /** 是否允许点「保存草稿」（含官方模版首次未落库） */
  canSaveDraft?: boolean;
  saving: boolean;
  publishing: boolean;
  /** 官方模版未创建前禁用发布 */
  publishDisabled?: boolean;
  /** 最近一次保存时间；副标题展示「自动保存于 HH:mm:ss」 */
  autoSavedAt?: Date | null;
  onVersionChange: (versionId: number) => void;
  onSaveDraft: () => void;
  onPublish: () => void;
  /** 自定义返回（如未保存确认）；入参为实际执行返回 */
  onBack?: (goBack: () => void) => void;
}

type IdentityHeaderProps = Pick<DetailHeaderProps, 'detail' | 'autoSavedAt' | 'onBack'>;
type ConfigHeaderProps = Omit<DetailHeaderProps, 'autoSavedAt' | 'onBack'>;

export function AgentIdentityHeader({ detail, autoSavedAt, onBack }: IdentityHeaderProps) {
  const goBack = useGoBack('/agents/mine');
  const meta = AGENT_STATUS_META[detail.status];

  return (
    <div className={styles.identityHeaderContent}>
      <Button
        type="text"
        icon={<ArrowLeftOutlined />}
        aria-label="返回 Agent 列表"
        onClick={() => (onBack ? onBack(goBack) : goBack())}
      />
      <div className={styles.headerTitleBox}>
        <div className={styles.headerNameRow}>
          <span className={styles.headerName}>{detail.name}</span>
          <Tag color={meta.color} className={styles.headerTag}>
            {meta.text}
          </Tag>
        </div>
        <div className={styles.headerSub}>
          {detail.isBuiltin ? 'Base Profile 默认配置' : detail.description || '暂无描述'}
          {autoSavedAt && (
            <span className={styles.autoSavedInline}>
              {' · '}
              自动保存于 {formatClock(autoSavedAt)}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

export function AgentConfigHeader({
  detail,
  versions,
  selectedVersionId,
  dirty,
  canSaveDraft,
  saving,
  publishing,
  publishDisabled = false,
  onVersionChange,
  onSaveDraft,
  onPublish,
}: ConfigHeaderProps) {
  const versionOptions = versions.map((v) => ({
    value: v.id,
    label: `v${v.version}${v.status === 'draft' ? '（草稿）' : ''}`,
  }));
  const saveEnabled = canSaveDraft ?? dirty;

  return (
    <div className={styles.configHeaderContent}>
      <div className={styles.configHeaderInfo}>
        <span className={styles.configHeaderTitle}>Agent 配置</span>
        {!detail.isBuiltin &&
          (versionOptions.length > 0 ? (
            <span className={styles.versionField}>
              <span className={styles.versionLabel}>版本</span>
              <Select
                size="small"
                className={styles.versionSelect}
                value={selectedVersionId}
                placeholder="选择版本"
                options={versionOptions}
                onChange={onVersionChange}
                popupMatchSelectWidth={false}
                aria-label="切换 Agent 版本"
              />
            </span>
          ) : (
            <Tooltip title="保存草稿创建我的 Agent 后才会生成版本">
              <span className={styles.versionPendingTip}>版本尚未生成</span>
            </Tooltip>
          ))}
      </div>

      <div className={styles.headerRight}>
        {detail.isBuiltin ? (
          <Tooltip title="内置 Base Profile 不可保存，创建 Agent 后即可编辑与发布">
            <Button disabled>保存草稿</Button>
          </Tooltip>
        ) : (
          <>
            <Button
              icon={<SaveOutlined />}
              disabled={!saveEnabled}
              loading={saving}
              onClick={onSaveDraft}
            >
              保存草稿
            </Button>
            <Tooltip title={publishDisabled ? '请先保存草稿创建我的 Agent' : undefined}>
              <Button
                type="primary"
                icon={<CloudUploadOutlined />}
                loading={publishing}
                disabled={publishDisabled}
                onClick={onPublish}
              >
                更新发布
              </Button>
            </Tooltip>
          </>
        )}
      </div>
    </div>
  );
}
