import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  filterMaterialFilesByDrama,
  flattenMaterialFiles,
  sortDramaEpisodes,
} from '@/pages/MaterialLibrary/utils';
import type { MaterialCategoryFilter, MaterialDramaFilter } from '@/pages/MaterialLibrary/types';
import { MATERIAL_DRAMA_ALL } from '@/pages/MaterialLibrary/types';
import { useShortDramaCatalog } from '@/pages/MaterialLibrary/useShortDramaCatalog';
import type { WorksMediaType } from '@/pages/WorksGallery/types';
import {
  fetchPickerMaterialsPage,
  fetchPickerPortfolio,
  fetchPickerUploads,
  MEDIA_PICKER_PAGE_SIZE,
} from './api';
import type { MediaKind, MediaPickerTab, MediaSortOrder } from './types';
import { sortAssets, toMaterialAssets, toPortfolioAssets, toUploadAssets } from './utils';

interface UseMediaPickerListOptions {
  open: boolean;
  tab: MediaPickerTab;
  kinds: MediaKind[];
  filterKind: MediaKind | 'all';
  sort: MediaSortOrder;
  page: number;
  materialCategory?: MaterialCategoryFilter;
  materialDrama?: MaterialDramaFilter;
}

function activeKind(kinds: MediaKind[], filterKind: MediaKind | 'all'): MediaKind | undefined {
  if (filterKind !== 'all') return filterKind;
  return kinds.length === 1 ? kinds[0] : undefined;
}

export function useMediaPickerList({
  open,
  tab,
  kinds,
  filterKind,
  sort,
  page,
  materialCategory = 'all',
  materialDrama = MATERIAL_DRAMA_ALL,
}: UseMediaPickerListOptions) {
  const kind = activeKind(kinds, filterKind);
  const pageSize = MEDIA_PICKER_PAGE_SIZE;
  const useCatalog = open && tab === 'material' && materialCategory === 'short_drama';
  const catalog = useShortDramaCatalog(useCatalog);

  const uploadQueryParams = {
    mediaType: kind,
    status: 'success' as const,
    page,
    pageSize,
  };

  const portfolioQueryParams = {
    assetType: kind && kind !== 'audio' ? (kind as WorksMediaType) : undefined,
    page,
    pageSize,
  };

  const uploadsQuery = useQuery({
    queryKey: ['media-picker', 'uploads', uploadQueryParams] as const,
    queryFn: () => fetchPickerUploads(uploadQueryParams),
    enabled: open && tab === 'uploads',
    staleTime: 60_000,
    placeholderData: (previous) => previous,
  });

  const portfolioQuery = useQuery({
    queryKey: ['media-picker', 'portfolio', portfolioQueryParams] as const,
    queryFn: () => fetchPickerPortfolio(portfolioQueryParams),
    enabled: open && tab === 'portfolio',
    staleTime: 60_000,
    placeholderData: (previous) => previous,
  });

  const materialQuery = useQuery({
    queryKey: ['media-picker', 'material', { page, pageSize, category: materialCategory }] as const,
    queryFn: () =>
      fetchPickerMaterialsPage(
        page,
        pageSize,
        materialCategory === 'all' ? undefined : materialCategory,
      ),
    enabled: open && tab === 'material' && !useCatalog,
    staleTime: 5 * 60 * 1000,
    placeholderData: (previous) => previous,
  });

  return useMemo(() => {
    const visibleKinds = filterKind === 'all' ? kinds : [filterKind];
    const emptyDrama = {
      dramas: catalog.dramas,
      unlabeledDrama: catalog.unlabeled,
      dramaLoading: catalog.isFetching && catalog.dramas.length === 0,
    };

    if (tab === 'uploads') {
      return {
        items: sortAssets(toUploadAssets(uploadsQuery.data?.records ?? [], visibleKinds), sort),
        total: uploadsQuery.data?.total ?? 0,
        pageSize,
        isLoading: uploadsQuery.isLoading,
        isFetching: uploadsQuery.isFetching,
        isError: uploadsQuery.isError,
        refetch: uploadsQuery.refetch,
        ...emptyDrama,
      };
    }

    if (tab === 'portfolio') {
      return {
        items: sortAssets(
          toPortfolioAssets(portfolioQuery.data?.records ?? [], visibleKinds),
          sort,
        ),
        total: portfolioQuery.data?.total ?? 0,
        pageSize,
        isLoading: portfolioQuery.isLoading,
        isFetching: portfolioQuery.isFetching,
        isError: portfolioQuery.isError,
        refetch: portfolioQuery.refetch,
        ...emptyDrama,
      };
    }

    if (useCatalog) {
      const filtered = filterMaterialFilesByDrama(catalog.files, materialDrama);
      const ordered =
        materialDrama === MATERIAL_DRAMA_ALL ? filtered : sortDramaEpisodes(filtered);
      const assets =
        materialDrama === MATERIAL_DRAMA_ALL
          ? sortAssets(toMaterialAssets(ordered, visibleKinds), sort)
          : toMaterialAssets(ordered, visibleKinds);
      const start = (page - 1) * pageSize;
      return {
        items: assets.slice(start, start + pageSize),
        total: assets.length,
        pageSize,
        isLoading: catalog.isLoading,
        isFetching: catalog.isFetching,
        isError: catalog.isError,
        refetch: catalog.refetch,
        dramas: catalog.dramas,
        unlabeledDrama: catalog.unlabeled,
        dramaLoading: catalog.isFetching && catalog.dramas.length === 0,
      };
    }

    return {
      items: sortAssets(
        toMaterialAssets(flattenMaterialFiles(materialQuery.data?.records ?? []), visibleKinds),
        sort,
      ),
      total: materialQuery.data?.total ?? 0,
      pageSize,
      isLoading: materialQuery.isLoading,
      isFetching: materialQuery.isFetching,
      isError: materialQuery.isError,
      refetch: materialQuery.refetch,
      ...emptyDrama,
    };
  }, [
    catalog.dramas,
    catalog.files,
    catalog.isError,
    catalog.isFetching,
    catalog.isLoading,
    catalog.refetch,
    catalog.unlabeled,
    filterKind,
    kinds,
    materialDrama,
    materialQuery.data,
    materialQuery.isError,
    materialQuery.isFetching,
    materialQuery.isLoading,
    materialQuery.refetch,
    page,
    pageSize,
    portfolioQuery.data,
    portfolioQuery.isError,
    portfolioQuery.isFetching,
    portfolioQuery.isLoading,
    portfolioQuery.refetch,
    sort,
    tab,
    uploadsQuery.data,
    uploadsQuery.isError,
    uploadsQuery.isFetching,
    uploadsQuery.isLoading,
    uploadsQuery.refetch,
    useCatalog,
  ]);
}
