import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getPortfolioAgentNames, portfolioAgentNamesKey } from './api';

/** 作品集按 Agent 查看：必选，每次进入默认选中列表第一项 */
export function useRequiredAgentName(isMine: boolean) {
  const { data: agentNames = [], isLoading } = useQuery({
    queryKey: portfolioAgentNamesKey(isMine),
    queryFn: () => getPortfolioAgentNames(isMine),
    staleTime: 5 * 60 * 1000,
  });

  const [agentName, setAgentName] = useState<string | undefined>();

  useEffect(() => {
    if (isLoading) return;
    if (agentNames.length === 0) {
      setAgentName(undefined);
      return;
    }
    setAgentName((current) =>
      current && agentNames.includes(current) ? current : agentNames[0],
    );
  }, [agentNames, isLoading]);

  const agentReady = agentName != null && !isLoading && agentNames.includes(agentName);

  return {
    agentNames,
    agentNamesLoading: isLoading,
    agentName,
    agentReady,
    selectAgent: setAgentName,
  };
}
