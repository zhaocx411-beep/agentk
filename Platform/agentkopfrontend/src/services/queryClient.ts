/**
 * 全局 React Query 客户端
 *
 * 单独成模块而不是留在 main.tsx，是为了让非组件代码（如 services 里的
 * getCapabilities）也能走同一份缓存做请求去重。
 */
import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 5 * 60 * 1000, // 5 分钟缓存
    },
  },
});
