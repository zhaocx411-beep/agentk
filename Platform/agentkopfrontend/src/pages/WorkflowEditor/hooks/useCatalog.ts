/**
 * useCatalog - 获取后端能力目录（tool / pipeline 契约）
 * Pipeline 编辑器的节点配置面板需要真实的工具列表
 *
 * useQuery 用 fetchCapabilities；命令式缓存仍走 getCapabilities。
 * 同 key 多处订阅只会发一次请求。
 */
import { useQuery } from '@tanstack/react-query';
import { CAPABILITIES_QUERY_KEY, fetchCapabilities } from '@/services/workflow';
import type { CapabilityCatalog } from '@/pages/AgentPlayground/types';

interface UseCatalogState {
  catalog: CapabilityCatalog | null;
  loading: boolean;
  error: string | null;
}

/**
 * 拉取能力目录，提供工具列表给节点配置面板
 * 失败时降级为空列表，不阻塞画布编辑
 */
export function useCatalog(): UseCatalogState {
  const { data, isPending, error } = useQuery({
    queryKey: CAPABILITIES_QUERY_KEY,
    queryFn: fetchCapabilities,
  });

  let errorMessage: string | null = null;
  if (error) {
    errorMessage = error instanceof Error ? error.message : '获取能力目录失败';
  }

  return {
    catalog: data ?? null,
    loading: isPending,
    error: errorMessage,
  };
}
