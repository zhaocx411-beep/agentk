import { useMemo } from 'react';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import {
  buildMangaFoldSliceFromLedger,
  mergeMangaFoldState,
} from './buildMangaFoldSliceFromLedger';
import { foldMangaCanvas, type MangaCanvasModel } from './foldMangaCanvas';
import { useOfficialSessionLedger } from './useOfficialSessionLedger';

/** 画布折叠：共享全量账本缓存 + 合并实时 SSE，补全早期 generate_image 等事件 */
export function useMangaCanvasModel(sessionId: string, liveState: SSEState): MangaCanvasModel {
  const { data: ledgerEvents } = useOfficialSessionLedger(sessionId);

  return useMemo(() => {
    if (!ledgerEvents?.length) return foldMangaCanvas(liveState);
    const slice = buildMangaFoldSliceFromLedger(ledgerEvents);
    const merged = mergeMangaFoldState(slice, liveState);
    return foldMangaCanvas({ ...liveState, ...merged });
  }, [ledgerEvents, liveState]);
}
