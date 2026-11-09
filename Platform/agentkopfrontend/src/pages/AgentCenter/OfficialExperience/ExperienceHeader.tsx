/**
 * 官方体验页顶栏：模版信息 + 运行状态
 * 自主规划可「基于此创建」；官方工作流仅试用，不提供该入口
 */
import { Button } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { useGoBack } from '@/hooks';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import AgentAvatar from '../components/AgentAvatar';
import { AGENT_RUN_STATUS_LABEL, resolveAgentRunStatus } from './runStatus';
import styles from './OfficialExperience.module.less';

const EXPERIENCE_RUN_STATUS_CLASS = {
  running: styles.runRunning,
  processing: styles.runProcessing,
  failed: styles.runFailed,
  idle: styles.runIdle,
} as const;

interface ExperienceHeaderProps {
  item: OfficialCatalogCardData | null;
  isRunning: boolean;
  /** 有未完成的后台任务（视频生成等），即使 round 已结束也算处理中 */
  hasPendingJobs?: boolean;
  hasError: boolean;
  usingTemplate: boolean;
  onUseTemplate: () => void;
}

function ExperienceHeader({
  item,
  isRunning,
  hasPendingJobs = false,
  hasError,
  usingTemplate,
  onUseTemplate,
}: ExperienceHeaderProps) {
  // 有应用内历史则回上一页；直接打开/刷新时兜底首页
  const goBack = useGoBack('/');
  const typeLabel = item?.variant === 'workflow' ? '工作流 Agent' : 'Agent-Loop Agent';
  const versionLabel = item?.skills.find((skill) => /^v\d+/i.test(skill));
  const title = item?.name ?? '官方 Agent';
  const description = item?.description ?? '官方示范能力，可直接体验效果';
  // 「待运行」容易被理解成卡着不动；空闲就是空闲，有后台任务单独标出来
  const runStatus = resolveAgentRunStatus(isRunning, hasPendingJobs, hasError);
  const runLabel = AGENT_RUN_STATUS_LABEL[runStatus];
  const runClass = EXPERIENCE_RUN_STATUS_CLASS[runStatus];

  return (
    <header className={styles.header}>
      <div className={styles.headerLeft}>
        <button type="button" className={styles.backBtn} aria-label="返回上一页" onClick={goBack}>
          <ArrowLeftOutlined />
        </button>
        <AgentAvatar
          size={40}
          kind={item?.variant === 'workflow' ? 'workflow' : 'planning'}
          className={styles.headerAvatar}
        />
        <div className={styles.headerText}>
          <div className={styles.headerNameRow}>
            <h1 className={styles.headerName} title={title}>
              {title}
            </h1>
            <span className={styles.headerMeta}>
              {typeLabel}
              {versionLabel ? ` · ${versionLabel}` : ''}
            </span>
          </div>
          <p className={styles.headerDesc} title={description}>
            {description}
          </p>
        </div>
      </div>
      <div className={styles.headerActions}>
        <span className={`${styles.runTag} ${runClass}`}>{runLabel}</span>
        {item?.variant === 'planning' ? (
          <Button
            type="primary"
            className={styles.createButton}
            loading={usingTemplate}
            disabled={!item}
            onClick={onUseTemplate}
          >
            基于此创建
          </Button>
        ) : null}
      </div>
    </header>
  );
}

export default ExperienceHeader;
