import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { queryAllMaterialTasksByCategory } from './api';
import { flattenMaterialFiles, hasUnlabeledDrama, listMaterialDramas } from './utils';

/** 短剧/漫剧全量目录：pageSize=100 自动翻页拉齐，供剧名筛选与客户端分页 */
export function useShortDramaCatalog(enabled: boolean) {
  const query = useQuery({
    queryKey: ['material-library', 'short-drama-catalog'] as const,
    queryFn: () => queryAllMaterialTasksByCategory('short_drama'),
    enabled,
    staleTime: 5 * 60 * 1000,
  });

  const files = useMemo(() => flattenMaterialFiles(query.data ?? []), [query.data]);
  const dramas = useMemo(() => listMaterialDramas(files), [files]);
  const unlabeled = useMemo(() => hasUnlabeledDrama(files), [files]);

  return {
    files,
    dramas,
    unlabeled,
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    isError: query.isError,
    refetch: query.refetch,
  };
}
