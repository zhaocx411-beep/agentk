/**
 * Agent100 - 能力目录
 */
import { Alert, Button, Empty } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { CardGridSkeleton } from '@/components';
import { AGENT100_LIST_QUERY_KEY, getAgent100List } from './api';
import { AgentCard } from './components/AgentCard';
import { AGENT100_SKELETON_COUNT } from './constants';
import { AGENT100_TEXT } from './mock';
import styles from './Agent100.module.less';

function Agent100() {
  const navigate = useNavigate();
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: AGENT100_LIST_QUERY_KEY,
    queryFn: getAgent100List,
  });
  const agents = data?.agents ?? [];

  return (
    <div className={styles.page} data-catalog-scroll>
      <section className={styles.gallery}>
        <header className={styles.hero}>
          <h2 className={styles.heroTitle}>
            {AGENT100_TEXT.heroTitle.lead}
            <em className={styles.heroAccent}>{AGENT100_TEXT.heroTitle.accent}</em>
            {AGENT100_TEXT.heroTitle.tail}
          </h2>
          <p className={styles.heroDesc}>{AGENT100_TEXT.heroDesc}</p>
        </header>

        {isLoading ? (
          <CardGridSkeleton className={styles.grid} variant="info" count={AGENT100_SKELETON_COUNT} />
        ) : null}

        {isError ? (
          <div className={styles.stateFrame}>
            <Alert type="error" showIcon message={AGENT100_TEXT.listError} />
            <Button onClick={() => void refetch()}>重新加载</Button>
          </div>
        ) : null}

        {!isLoading && !isError && agents.length === 0 ? (
          <div className={styles.empty}>
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} />
            <h3 className={styles.emptyTitle}>{AGENT100_TEXT.emptyTitle}</h3>
            <p className={styles.emptyDesc}>{AGENT100_TEXT.emptyDesc}</p>
          </div>
        ) : null}

        {!isLoading && !isError && agents.length > 0 ? (
          <section className={styles.grid} aria-label={AGENT100_TEXT.listAria}>
            {agents.map((agent) => (
              <AgentCard
                key={agent.id}
                agent={agent}
                onOpen={(item) => navigate(`/agent100/${encodeURIComponent(item.id)}`)}
              />
            ))}
          </section>
        ) : null}
      </section>
    </div>
  );
}

export default Agent100;
