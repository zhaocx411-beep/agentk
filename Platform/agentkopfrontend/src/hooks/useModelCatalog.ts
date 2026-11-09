/**
 * 全平台模型目录：统一走 GET /agentk/models（React Query 共享缓存）
 */
import { useQuery } from '@tanstack/react-query';
import { MODELS_QUERY_KEY, fetchModels } from '@/services/workflow';
import type { ModelCatalog } from '@/pages/AgentPlayground/types';

interface UseModelCatalogOptions {
  /** 默认 true；弹窗等场景可按需启用 */
  enabled?: boolean;
}

export function useModelCatalog(options: UseModelCatalogOptions = {}) {
  const { enabled = true } = options;
  const query = useQuery<ModelCatalog>({
    queryKey: MODELS_QUERY_KEY,
    queryFn: fetchModels,
    enabled,
    staleTime: 5 * 60 * 1000,
  });

  return {
    ...query,
    catalog: query.data ?? null,
    isLoading: query.isLoading || query.isFetching,
  };
}
