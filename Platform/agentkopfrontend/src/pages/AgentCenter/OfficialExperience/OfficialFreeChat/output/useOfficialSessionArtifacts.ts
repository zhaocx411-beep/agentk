/**
 * 当前产物：优先用 SSE 实时态展示，账本缓存逐页补齐后合并，不阻塞首屏。
 */
import { useMemo } from 'react';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import { collectSessionInputMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import { collectArtifacts, filterSessionOutputArtifacts } from '../../studio/lib/pipeline';
import { classifyArtifactTier, sortArtifactsByTier } from '../../studio/lib/artifactTier';
import type { ArtifactItem } from '../../studio/lib/types';
import { buildMangaFoldSliceFromLedger } from '../manga/buildMangaFoldSliceFromLedger';
import { useOfficialSessionLedger } from '../manga/useOfficialSessionLedger';

function mergeArtifacts(enableFinalTier: boolean, ...groups: ArtifactItem[][]): ArtifactItem[] {
  const byUrl = new Map<string, ArtifactItem>();
  for (const group of groups) {
    for (const item of group) {
      const tier = enableFinalTier
        ? item.tier ??
          classifyArtifactTier(item.url, {
            sourceStage: item.sourceStage,
            toolName: item.sourceTool,
          })
        : undefined;
      const normalized: ArtifactItem = { ...item, tier };
      const existing = byUrl.get(item.url);
      if (!existing) {
        byUrl.set(item.url, normalized);
        continue;
      }
      byUrl.set(item.url, {
        ...existing,
        sourceTool: existing.sourceTool ?? normalized.sourceTool,
        sourceStage: existing.sourceStage ?? normalized.sourceStage,
        tier:
          enableFinalTier && (existing.tier === 'final' || normalized.tier === 'final')
            ? 'final'
            : enableFinalTier
              ? existing.tier ?? normalized.tier
              : undefined,
      });
    }
  }
  return sortArtifactsByTier([...byUrl.values()]);
}

export function useOfficialSessionArtifacts(
  sessionId: string,
  liveState: SSEState,
  enableFinalTier = false,
) {
  const enabled = sessionId.startsWith('ses_');
  const {
    data: ledgerEvents,
    isLoading,
    isFetching,
    isLedgerComplete,
    isBackfilling,
  } =
    useOfficialSessionLedger(sessionId);

  const artifacts = useMemo(() => {
    const collectOptions = { enableFinalTier };
    const live = collectArtifacts(liveState, [], collectOptions);
    let merged: ArtifactItem[];
    if (!ledgerEvents?.length) {
      merged = live;
    } else {
      const slice = buildMangaFoldSliceFromLedger(ledgerEvents);
      const fromLedger = collectArtifacts(
        {
          ...liveState,
          toolCards: slice.toolCards,
          jobs: slice.jobs,
          messages: slice.messages,
          streamingText: '',
          streamingReason: '',
        },
        [],
        collectOptions,
      );
      merged = mergeArtifacts(enableFinalTier, fromLedger, live);
    }

    const inputUrls = collectSessionInputMediaUrls({
      messages: liveState.messages,
      toolCards: liveState.toolCards,
      ledgerEvents: ledgerEvents ?? [],
    });
    return filterSessionOutputArtifacts(merged, inputUrls);
  }, [enableFinalTier, ledgerEvents, liveState]);

  const loadingArtifacts =
    enabled &&
    artifacts.length === 0 &&
    (isLoading || isFetching || isBackfilling);

  return {
    artifacts,
    loadingArtifacts,
    loadingMoreArtifacts: enabled && isBackfilling && !isLedgerComplete,
  };
}
