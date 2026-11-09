import { isIconUrl, isUsableIcon } from '../utils';
import type { Agent100Summary } from '../types';
import styles from '../Agent100.module.less';

interface AgentCardProps {
  agent: Agent100Summary;
  onOpen: (agent: Agent100Summary) => void;
}

function AgentCard({ agent, onOpen }: AgentCardProps) {
  const showImage = isUsableIcon(agent.icon) && isIconUrl(agent.icon);
  const mark = agent.name.slice(0, 1).toUpperCase();

  return (
    <article
      className={styles.card}
      role="button"
      tabIndex={0}
      onClick={() => onOpen(agent)}
      onKeyDown={(event) => {
        if (event.key === 'Enter') onOpen(agent);
      }}
    >
      <div className={styles.cardHead}>
        <div className={styles.cardMark} aria-hidden>
          {showImage ? <img src={agent.icon} alt="" /> : mark}
        </div>
        <div className={styles.cardHeading}>
          <h3 className={styles.cardTitle}>{agent.name}</h3>
          <p className={styles.cardId}>ID: {agent.id}</p>
        </div>
      </div>
      <p className={styles.cardDesc}>{agent.description}</p>
    </article>
  );
}

export { AgentCard };
