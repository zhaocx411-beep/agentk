/**
 * AgentCard - Agent 列表卡片
 * 布局参考：头像 + 名称/类型 → ID → 描述 → 状态 + 更新时间
 */
import { Tag, Tooltip } from 'antd';
import type { AgentCardData } from '../types';
import { AGENT_STATUS_META } from '../constants';
import AgentAvatar from './AgentAvatar';
import styles from '../AgentCenter.module.less';

interface AgentCardProps {
  data: AgentCardData;
  onClick: (data: AgentCardData) => void;
}

function AgentCard({ data, onClick }: AgentCardProps) {
  const meta = AGENT_STATUS_META[data.status];
  return (
    <div
      className={styles.card}
      onClick={() => onClick(data)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onClick(data);
      }}
    >
      <div className={styles.cardHead}>
        <AgentAvatar size={48} kind={data.kind} />
        <div className={styles.cardTitleBox}>
          <div className={styles.cardName} title={data.name}>
            {data.name}
          </div>
          <div className={styles.cardType}>{data.typeLabel}</div>
        </div>
      </div>

      <Tooltip
        title={
          data.kind === 'builtin'
            ? 'Base Profile'
            : data.kind === 'workflow'
              ? `工作流 ID: ${data.workflowId}`
              : `ID: ${data.key}`
        }
      >
        <div className={styles.cardId}>
          {data.kind === 'builtin'
            ? `catalog: ${data.versionLabel || '-'}`
            : data.kind === 'workflow'
              ? `ID: ${data.workflowId}`
              : `ID: ${data.key}`}
        </div>
      </Tooltip>

      <div className={styles.cardDesc} title={data.description}>
        {data.description}
      </div>

      {data.toolCount !== undefined && (
        <div className={styles.cardMeta}>
          <Tag className={styles.cardMetaTag}>{data.toolCount} 个工具</Tag>
        </div>
      )}

      <div className={styles.cardFoot}>
        <Tag color={meta.color} className={styles.cardStatus}>
          {meta.text}
        </Tag>
        {data.versionLabel && data.status !== 'builtin' && (
          <span className={styles.cardVersion}>{data.versionLabel}</span>
        )}
        {data.updatedText && <span className={styles.cardTime}>{data.updatedText} 更新</span>}
      </div>
    </div>
  );
}

export default AgentCard;
