import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { materialTasksQueryKey, queryMaterialTasksPage } from './api';
import {
  filterMaterialFilesByDrama,
  flattenMaterialFiles,
  sortDramaEpisodes,
} from './utils';
import type { MaterialCategoryFilter, MaterialDramaFilter } from './types';
import { MATERIAL_DRAMA_ALL } from './types';
import { useShortDramaCatalog } from './useShortDramaCatalog';

const PAGE_SIZE = 20;

export function useMaterialLibraryList() {
  const [category, setCategory] = useState<MaterialCategoryFilter>('all');
  const [drama, setDrama] = useState<MaterialDramaFilter>(MATERIAL_DRAMA_ALL);
  const [page, setPage] = useState(1);
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null);

  const useCatalog = category === 'short_drama';
  const catalog = useShortDramaCatalog(useCatalog);

  const queryParams = useMemo(
    () => ({
      current: page,
      pageSize: PAGE_SIZE,
      category: category === 'all' ? undefined : category,
    }),
    [category, page],
  );

  const pageQuery = useQuery({
    queryKey: materialTasksQueryKey(queryParams),
    queryFn: () => queryMaterialTasksPage(queryParams),
    enabled: !useCatalog,
    placeholderData: (previous) => previous,
  });

  const catalogFiles = useMemo(() => {
    const filtered = filterMaterialFilesByDrama(catalog.files, drama);
    return drama === MATERIAL_DRAMA_ALL ? filtered : sortDramaEpisodes(filtered);
  }, [catalog.files, drama]);

  const files = useMemo(() => {
    if (!useCatalog) return flattenMaterialFiles(pageQuery.data?.records ?? []);
    const start = (page - 1) * PAGE_SIZE;
    return catalogFiles.slice(start, start + PAGE_SIZE);
  }, [catalogFiles, page, pageQuery.data?.records, useCatalog]);

  const total = useCatalog ? catalogFiles.length : (pageQuery.data?.total ?? 0);
  const isLoading = useCatalog ? catalog.isLoading : pageQuery.isLoading;
  const isFetching = useCatalog ? catalog.isFetching : pageQuery.isFetching;
  const isError = useCatalog ? catalog.isError : pageQuery.isError;

  const changeCategory = (next: MaterialCategoryFilter) => {
    setCategory(next);
    setDrama(MATERIAL_DRAMA_ALL);
    setPage(1);
    setSelectedIndex(null);
  };

  const changeDrama = (next: MaterialDramaFilter) => {
    setDrama(next);
    setPage(1);
    setSelectedIndex(null);
  };

  const changePage = (next: number) => {
    setSelectedIndex(null);
    setPage(next);
  };

  return {
    category,
    drama,
    page,
    pageSize: PAGE_SIZE,
    files,
    total,
    selectedIndex,
    selectedFile: selectedIndex !== null ? (files[selectedIndex] ?? null) : null,
    dramas: catalog.dramas,
    unlabeledDrama: catalog.unlabeled,
    dramaLoading: catalog.isFetching && catalog.dramas.length === 0,
    isLoading,
    isFetching,
    isError,
    setSelectedIndex,
    changeCategory,
    changeDrama,
    changePage,
    refetch: () => (useCatalog ? catalog.refetch() : pageQuery.refetch()),
  };
}
