import { Button, Empty, Spin } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { useGoBack } from '@/hooks';
import {
  AGENT100_CONFIG_QUERY_KEY,
  AGENT100_LIST_QUERY_KEY,
  getAgent100Config,
  getAgent100List,
} from '../api';
import { AGENT100_TEXT } from '../mock';
import { findAgentSummary } from '../utils';
import { AgentHeader } from '../components/AgentHeader';
import { PipelineWorkspace } from '../components/PipelineWorkspace';
import styles from '../Agent100.module.less';

function Agent100Detail() {
  const { agentId = '' } = useParams<{ agentId: string }>();
  const goBack = useGoBack('/agent100');

  const listQuery = useQuery({
    queryKey: AGENT100_LIST_QUERY_KEY,
    queryFn: getAgent100List,
  });
  const configQuery = useQuery({
    queryKey: [...AGENT100_CONFIG_QUERY_KEY, agentId],
    queryFn: () => getAgent100Config(agentId),
    enabled: Boolean(agentId),
  });

  const summary = findAgentSummary(listQuery.data?.agents, agentId);

  if (configQuery.isLoading && !configQuery.data) {
    return (
      <div className={styles.detailPageLoading}>
        <Spin size="large" />
      </div>
    );
  }

  if (!configQuery.data) {
    return (
      <div className={styles.detailPageLoading}>
        <Empty description={configQuery.isError ? AGENT100_TEXT.configError : AGENT100_TEXT.detailNotFound}>
          <Button type="link" onClick={goBack}>
            {AGENT100_TEXT.backToList}
          </Button>
        </Empty>
      </div>
    );
  }

  return (
    <div className={styles.detailPage}>
      <AgentHeader
        agentId={agentId}
        summary={summary}
        pipelineCount={configQuery.data.pipelines.length}
        onBack={goBack}
      />
      <PipelineWorkspace
        config={configQuery.data}
        defaultTitle={summary?.name || agentId}
      />
    </div>
  );
}

export default Agent100Detail;
