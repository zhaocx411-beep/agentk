/**
 * AgentCenter - Agent 列表页
 * 官方 / 我的已拆为二级菜单，由路由传入 scope 决定渲染哪一侧，页内不再有分类 Tab
 */
import { useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { AGENT_LOOPS_QUERY_KEY, getAgentLoops } from './api';
import { toAgentCard } from './utils';
import type { AgentCardData, AgentScope } from './types';
import CreateAgentModal from './components/CreateAgentModal';
import MineAgentPanel from './components/MineAgentPanel';
import OfficialAgentGallery from './components/official/OfficialAgentGallery';
import styles from './AgentCenter.module.less';

interface AgentCenterProps {
  /** 由路由决定：official 展示官方示范目录，mine 展示自建 Agent 列表 */
  scope: AgentScope;
}

function AgentCenter({ scope }: AgentCenterProps) {
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  const pageRef = useRef<HTMLDivElement>(null);
  const isMine = scope === 'mine';

  const scrollPageTop = () => {
    pageRef.current?.scrollTo({ top: 0 });
  };

  const { data: agents = [], isLoading } = useQuery({
    queryKey: AGENT_LOOPS_QUERY_KEY,
    queryFn: getAgentLoops,
    enabled: isMine,
  });

  const cards = useMemo<AgentCardData[]>(
    () =>
      [...agents]
        .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
        .map(toAgentCard),
    [agents],
  );

  const handleOpen = (item: AgentCardData) => {
    navigate(`/agents/${item.key}`);
  };

  return (
    <div className={styles.page} ref={pageRef} data-catalog-scroll>
      {isMine ? (
        <MineAgentPanel
          cards={cards}
          loading={isLoading}
          onCreate={() => setCreateOpen(true)}
          onOpen={handleOpen}
          onPageReset={scrollPageTop}
        />
      ) : (
        <OfficialAgentGallery />
      )}

      <CreateAgentModal open={createOpen} onClose={() => setCreateOpen(false)} />
    </div>
  );
}

export default AgentCenter;
