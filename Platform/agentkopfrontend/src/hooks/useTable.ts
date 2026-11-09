import { useState, useCallback } from 'react';
import type { PaginationParams } from '@/types/api';

interface UseTableOptions {
  /** 默认每页条数 */
  defaultPageSize?: number;
}

interface UseTableResult {
  /** 分页参数 */
  pagination: PaginationParams;
  /** 数据总条数 */
  total: number;
  /** 加载状态 */
  loading: boolean;
  /** 表格数据 */
  dataSource: unknown[];
  /** 设置加载状态 */
  setLoading: (loading: boolean) => void;
  /** 设置数据 */
  setDataSource: (data: unknown[]) => void;
  /** 设置总条数 */
  setTotal: (total: number) => void;
  /** 页码变化回调 */
  onChange: (page: number, pageSize: number) => void;
  /** 重置分页到第一页 */
  reset: () => void;
}

/**
 * useTable - 通用表格 Hook
 * 管理表格的分页、loading、数据状态
 *
 * @param options 配置项
 * @returns 表格状态及操作方法
 *
 * @example
 * const { pagination, loading, dataSource, onChange } = useTable({ defaultPageSize: 20 });
 */
export function useTable(options?: UseTableOptions): UseTableResult {
  const defaultPageSize = options?.defaultPageSize ?? 10;

  const [pagination, setPagination] = useState<PaginationParams>({
    page: 1,
    pageSize: defaultPageSize,
  });
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [dataSource, setDataSource] = useState<unknown[]>([]);

  const onChange = useCallback((page: number, pageSize: number) => {
    setPagination({ page, pageSize });
  }, []);

  const reset = useCallback(() => {
    setPagination({ page: 1, pageSize: defaultPageSize });
  }, [defaultPageSize]);

  return {
    pagination,
    total,
    loading,
    dataSource,
    setLoading,
    setDataSource,
    setTotal,
    onChange,
    reset,
  };
}
