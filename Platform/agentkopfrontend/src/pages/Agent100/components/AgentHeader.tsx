import { Button } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { AGENT100_TEXT } from '../mock';
import { isIconUrl, isUsableIcon } from '../utils';
import type { Agent100Summary } from '../types';
import styles from '../Agent100.module.less';

interface AgentHeaderProps {
  agentId: string;
  summary?: Agent100Summary;
  pipelineCount: number;
  onBack: () => void;
}

function AgentHeader({ agentId, summary, pipelineCount, onBack }: AgentHeaderProps) {
  const name = summary?.name || agentId;
  const icon = summary?.icon ?? '';
  const showImage = isUsableIcon(icon) && isIconUrl(icon);
  const mark = (summary?.name || agentId).slice(0, 1).toUpperCase();

  return (
    <header className={styles.detailHeader}>
      <Button
        type="text"
        icon={<ArrowLeftOutlined />}
        onClick={onBack}
        aria-label={AGENT100_TEXT.backToList}
      />
      <div className={styles.detailMark} aria-hidden>
        {showImage ? <img src={icon} alt="" /> : mark}
      </div>
      <div className={styles.detailIntro}>
        <h1 className={styles.detailTitle}>{name}</h1>
        {summary?.description ? <p className={styles.detailDesc}>{summary.description}</p> : null}
        <p className={styles.detailMeta}>
          <span>ID: {agentId}</span>
          <span aria-hidden>·</span>
          <span>{AGENT100_TEXT.usageCount(pipelineCount)}</span>
        </p>
      </div>
    </header>
  );
}

export { AgentHeader };
