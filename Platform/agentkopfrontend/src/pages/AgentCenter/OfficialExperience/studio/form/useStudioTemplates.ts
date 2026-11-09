/**
 * 工作台创作模版：datasets/tasks/query?usage=TEMPLATE&pipelineName=
 * 选中后把 extraFields 回填到对应表单
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { fetchCreationTemplatePage, resolveTemplateQueryPipelineName, TEMPLATE_PAGE_SIZE } from '../lib/templateTasks';

export interface StudioTemplateItem {
  id: string;
  name: string;
  description: string;
  extraFields: Record<string, unknown>;
}

export interface StudioTemplatesModel {
  items: StudioTemplateItem[];
  selectedId?: string;
  total: number;
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  error: boolean;
  select: (id: string) => void;
  clear: () => void;
  loadMore: () => void;
}

export function useStudioTemplates(
  pipelineName: string,
  apply: (extraFields: Record<string, unknown>) => void,
): StudioTemplatesModel {
  const name = resolveTemplateQueryPipelineName(pipelineName);
  const applyRef = useRef(apply);
  applyRef.current = apply;

  const query = useInfiniteQuery({
    queryKey: ['creation-templates', name, TEMPLATE_PAGE_SIZE],
    queryFn: ({ pageParam }) => fetchCreationTemplatePage(name, pageParam, TEMPLATE_PAGE_SIZE),
    initialPageParam: 1,
    getNextPageParam: (last) => (last.hasMore ? last.current + 1 : undefined),
    enabled: !!name,
    staleTime: 60_000,
    retry: 1,
  });

  const items = useMemo<StudioTemplateItem[]>(
    () =>
      (query.data?.pages ?? []).flatMap((page) =>
        page.bundles.map((bundle) => ({
          id: bundle.template.id,
          name: bundle.template.name,
          description: bundle.template.description,
          extraFields: bundle.extraFields,
        })),
      ),
    [query.data],
  );
  const total = query.data?.pages?.[0]?.total ?? items.length;
  const [selectedId, setSelectedId] = useState('');

  useEffect(() => {
    if (!selectedId || items.length === 0) return;
    if (!items.some((row) => row.id === selectedId)) {
      setSelectedId('');
      applyRef.current({});
    }
  }, [items, selectedId]);

  useEffect(() => {
    if (!selectedId) return;
    const item = items.find((row) => row.id === selectedId);
    if (item) applyRef.current(item.extraFields);
  }, [selectedId, items]);

  const select = useCallback((id: string) => {
    setSelectedId(id);
  }, []);

  const clear = useCallback(() => {
    setSelectedId('');
    applyRef.current({});
  }, []);

  const {
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = query;
  const loadMore = useCallback(() => {
    if (!hasNextPage || isFetchingNextPage) return;
    void fetchNextPage();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage]);

  return {
    items,
    selectedId: selectedId || undefined,
    total,
    loading: query.isLoading,
    loadingMore: query.isFetchingNextPage,
    hasMore: !!query.hasNextPage,
    error: query.isError,
    select,
    clear,
    loadMore,
  };
}
