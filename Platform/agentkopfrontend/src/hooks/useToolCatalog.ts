/**
 * 工具名单明细：统一走 GET /agentk/tools（React Query 共享缓存）
 */
import { useQuery } from '@tanstack/react-query';
import { TOOLS_QUERY_KEY, fetchTools } from '@/services/workflow';
import type { AgentkToolCatalog } from '@/pages/AgentPlayground/types';

interface UseToolCatalogOptions {
  /** 默认 true；弹窗等场景可按需启用 */
  enabled?: boolean;
}

export function useToolCatalog(options: UseToolCatalogOptions = {}) {
  const { enabled = true } = options;
  const query = useQuery<AgentkToolCatalog>({
    queryKey: TOOLS_QUERY_KEY,
    queryFn: fetchTools,
    enabled,
    staleTime: 5 * 60 * 1000,
  });

  return {
    ...query,
    catalog: query.data ?? null,
    tools: query.data?.tools ?? [],
    isLoading: query.isLoading || query.isFetching,
  };
}
