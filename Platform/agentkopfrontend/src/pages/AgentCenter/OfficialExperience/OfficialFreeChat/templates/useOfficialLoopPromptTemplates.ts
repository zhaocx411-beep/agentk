/**
 * Agent-loop 空态快捷提问：datasets/tasks/query?usage=TEMPLATE&pipelineName=
 * 与 pipeline 工作台共用 fetchCreationTemplatePage，支持滚动翻页
 */
import { useCallback, useMemo } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import type { VirtualTryOnUserInput } from '@/pages/AgentPlayground/types';
import { isOfficialVirtualTryOnLoop } from '../../../utils';
import { suggestionFromTryOnBundle } from '../virtualTryOn/applyVirtualTryOnTemplatePick';
import {
  fetchCreationTemplatePage,
  mapTemplateBundleToChatSuggestion,
  resolveOfficialTemplatePipelineName,
  TEMPLATE_PAGE_SIZE,
  type CreationTemplateBundle,
} from '../../studio/lib/templateTasks';

/** 横滚条每次展示的条数（后端随机返回一批） */
export const COMPOSER_PROMPT_STRIP_COUNT = 6;

/** @deprecated 空态不再用大卡片，保留给灵感触发入口阈值 */
export const FEATURED_LOOP_TEMPLATE_COUNT = COMPOSER_PROMPT_STRIP_COUNT;

export interface OfficialLoopTemplateItem extends ChatSuggestionItem {
  id: string;
  /** 下拉列表里的摘要，通常为 video_prompt 截断 */
  hint: string;
  userInput?: VirtualTryOnUserInput;
}

export interface OfficialLoopPromptTemplatesModel {
  /** 输入框横滚条精选 */
  strip: ChatSuggestionItem[];
  /** @deprecated 使用 strip */
  featured: ChatSuggestionItem[];
  /** 下拉目录（已加载的全部页） */
  catalog: OfficialLoopTemplateItem[];
  fromApi: boolean;
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  total: number;
  loadMore: () => void;
  /** 换一批横滚灵感（重新请求 pageSize=6） */
  refreshStrip: () => void;
  refreshingStrip: boolean;
}

function toCatalogItem(bundle: CreationTemplateBundle, index: number): OfficialLoopTemplateItem {
  const suggestion = mapTemplateBundleToChatSuggestion(bundle);
  const hintSource = suggestion.prompt || bundle.template.description || '';
  const hint = hintSource.length > 72 ? `${hintSource.slice(0, 72)}…` : hintSource;
  return {
    id: String(bundle.template.id || index),
    label: suggestion.label,
    prompt: suggestion.prompt,
    referUrls: suggestion.referUrls,
    hint,
  };
}

function toTryOnCatalogItem(bundle: CreationTemplateBundle, index: number): OfficialLoopTemplateItem {
  const suggestion = suggestionFromTryOnBundle(bundle);
  const hintSource = suggestion.prompt || '';
  return {
    id: String(bundle.template.id || index),
    label: suggestion.label,
    prompt: suggestion.prompt,
    referUrls: suggestion.referUrls,
    hint: hintSource.length > 72 ? `${hintSource.slice(0, 72)}…` : hintSource,
    userInput: suggestion.userInput,
  };
}

export function useOfficialLoopPromptTemplates(
  item: OfficialCatalogCardData | null,
): OfficialLoopPromptTemplatesModel {
  const pipelineName = resolveOfficialTemplatePipelineName(item);
  const isVirtualTryOn = !!item && isOfficialVirtualTryOnLoop(item);

  const stripQuery = useQuery({
    queryKey: ['official-loop-prompt-strip', pipelineName, COMPOSER_PROMPT_STRIP_COUNT],
    queryFn: () =>
      fetchCreationTemplatePage(
        pipelineName,
        1,
        COMPOSER_PROMPT_STRIP_COUNT,
        'agent-loop',
      ),
    enabled: !!pipelineName,
    staleTime: 0,
    retry: 1,
    placeholderData: (previous) => previous,
  });

  const catalogQuery = useInfiniteQuery({
    queryKey: ['official-loop-prompt-templates', pipelineName, TEMPLATE_PAGE_SIZE],
    queryFn: ({ pageParam }) =>
      fetchCreationTemplatePage(pipelineName, pageParam, TEMPLATE_PAGE_SIZE, 'agent-loop'),
    initialPageParam: 1,
    getNextPageParam: (last) => (last.hasMore ? last.current + 1 : undefined),
    enabled: !!pipelineName,
    staleTime: 60_000,
    retry: 1,
  });

  const strip = useMemo<ChatSuggestionItem[]>(() => {
    const bundles = stripQuery.data?.bundles ?? [];
    if (isVirtualTryOn) return bundles.map((bundle) => suggestionFromTryOnBundle(bundle));
    return bundles.map((bundle) => mapTemplateBundleToChatSuggestion(bundle));
  }, [isVirtualTryOn, stripQuery.data]);

  const catalog = useMemo<OfficialLoopTemplateItem[]>(() => {
    let index = 0;
    return (catalogQuery.data?.pages ?? []).flatMap((page) =>
      page.bundles.map((bundle) => {
        const row = isVirtualTryOn ? toTryOnCatalogItem(bundle, index) : toCatalogItem(bundle, index);
        index += 1;
        return row;
      }),
    );
  }, [catalogQuery.data, isVirtualTryOn]);

  const awaitingApi = !!pipelineName && stripQuery.isLoading && strip.length === 0;
  const fromApi = strip.length > 0 || catalog.length > 0;
  const total = catalogQuery.data?.pages?.[0]?.total ?? catalog.length;
  const {
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = catalogQuery;
  const { refetch: refetchStrip } = stripQuery;

  const loadMore = useCallback(() => {
    if (!hasNextPage || isFetchingNextPage) return;
    void fetchNextPage();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage]);

  const refreshStrip = useCallback(() => {
    void refetchStrip();
  }, [refetchStrip]);

  return {
    strip,
    featured: strip,
    catalog,
    fromApi,
    loading: awaitingApi,
    loadingMore: catalogQuery.isFetchingNextPage,
    hasMore: !!catalogQuery.hasNextPage && catalog.length > 0,
    total,
    loadMore,
    refreshStrip,
    refreshingStrip: stripQuery.isFetching && strip.length > 0,
  };
}
