import { useState, useCallback } from 'react';
import type { AxiosRequestConfig } from 'axios';
import { http } from '@/utils/request';

interface UseRequestOptions<T> {
  /** 是否手动触发（默认 false，即自动请求） */
  manual?: boolean;
  /** 成功回调 */
  onSuccess?: (data: T) => void;
  /** 失败回调 */
  onError?: (error: Error) => void;
}

interface UseRequestResult<T> {
  /** 响应数据 */
  data: T | undefined;
  /** 加载状态 */
  loading: boolean;
  /** 错误信息 */
  error: Error | undefined;
  /** 手动触发请求 */
  run: (config?: AxiosRequestConfig) => Promise<T | undefined>;
}

/**
 * useRequest - 通用请求 Hook
 * 封装接口请求的 loading、error、data 状态管理
 *
 * @param config 请求配置
 * @param options 可选配置
 * @returns 请求状态及操作方法
 *
 * @example
 * const { data, loading, run } = useRequest<UserInfo>({ url: '/user/info' });
 */
export function useRequest<T = unknown>(
  config: AxiosRequestConfig,
  options?: UseRequestOptions<T>,
): UseRequestResult<T> {
  const [data, setData] = useState<T | undefined>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | undefined>();

  const run = useCallback(
    async (overrideConfig?: AxiosRequestConfig) => {
      try {
        setLoading(true);
        setError(undefined);
        const result = await http<T>({ ...config, ...overrideConfig });
        setData(result);
        options?.onSuccess?.(result);
        return result;
      } catch (err) {
        const error = err instanceof Error ? err : new Error(String(err));
        setError(error);
        options?.onError?.(error);
        return undefined;
      } finally {
        setLoading(false);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [config.url, config.method],
  );

  return { data, loading, error, run };
}
