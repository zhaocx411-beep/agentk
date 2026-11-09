/**
 * 会话账本缓存（React Query）：产物 / 画布 / 对话上滚共用。
 *
 * 策略见 constants.ts SESSION_LEDGER_*：
 * - 冷启动：bootstrap N 页 → 后台补到 ceil(total/20)
 * - 热切换：缓存已齐时只同步第 1 页（由 loadSession 控制）
 */
import { useEffect } from 'react';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { isOfficialPendingSessionId } from '@/pages/AgentCenter/utils';
import {
  isSessionLedgerComplete,
  SESSION_EVENTS_PAGE_SIZE,
  sessionLedgerTargetPages,
} from '@/pages/AgentPlayground/constants';
import { fetchAllSessionEvents } from '@/pages/AgentPlayground/utils';
import type { SessionLedgerBootstrap } from '@/pages/AgentPlayground/utils';
import type { SessionLedgerEvent } from '@/pages/AgentPlayground/types';
import {
  comparePlatformSessionLedgerEvents,
  sessionLedgerEventKey,
} from '@/pages/AgentPlayground/utils/sessionLedgerOrder';

export const OFFICIAL_SESSION_LEDGER_QUERY_KEY = 'official-session-ledger';

const backfillInflight = new Map<string, Promise<void>>();

export interface OfficialSessionLedgerCache extends SessionLedgerBootstrap {
  isBackfilling?: boolean;
}

export function officialSessionLedgerQueryKey(sessionId: string) {
  return [OFFICIAL_SESSION_LEDGER_QUERY_KEY, sessionId] as const;
}

export function mergeSessionEventsBySeq(
  base: readonly SessionLedgerEvent[],
  incoming: readonly SessionLedgerEvent[],
): SessionLedgerEvent[] {
  const byEventKey = new Map<string, SessionLedgerEvent>();
  for (const event of base) byEventKey.set(sessionLedgerEventKey(event), event);
  for (const event of incoming) byEventKey.set(sessionLedgerEventKey(event), event);
  return [...byEventKey.values()].sort(comparePlatformSessionLedgerEvents);
}

export function readOfficialSessionLedgerCache(
  queryClient: QueryClient,
  sessionId: string,
): OfficialSessionLedgerCache | undefined {
  return queryClient.getQueryData<OfficialSessionLedgerCache>(
    officialSessionLedgerQueryKey(sessionId),
  );
}

function writeOfficialSessionLedgerCache(
  queryClient: QueryClient,
  sessionId: string,
  next: OfficialSessionLedgerCache,
): void {
  queryClient.setQueryData<OfficialSessionLedgerCache>(
    officialSessionLedgerQueryKey(sessionId),
    next,
  );
}

export function isLedgerComplete(cache: OfficialSessionLedgerCache | undefined): boolean {
  if (!cache || cache.pagesLoaded === 0) return false;
  return Boolean(cache.exhausted) || isSessionLedgerComplete(cache.events.length, cache.total);
}

/** 增量写入（上滚 / bootstrap 逐页） */
export function seedOfficialSessionLedgerCache(
  queryClient: QueryClient,
  sessionId: string,
  partial: SessionLedgerBootstrap,
): void {
  const prev = readOfficialSessionLedgerCache(queryClient, sessionId);
  const total = partial.pagesLoaded > 0 ? partial.total : (prev?.total ?? 0);
  writeOfficialSessionLedgerCache(queryClient, sessionId, {
    events: mergeSessionEventsBySeq(prev?.events ?? [], partial.events),
    total,
    pagesLoaded: Math.max(partial.pagesLoaded, prev?.pagesLoaded ?? 0),
    exhausted:
      Boolean(partial.exhausted) || Boolean(prev?.exhausted && total <= (prev?.total ?? 0)),
    isBackfilling: prev?.isBackfilling,
  });
}

/**
 * bootstrap 结束：合并缓存并视情况触发后台补页。
 * @param skipBackfill 热切换时已齐，只写入第 1 页同步结果
 */
export function commitSessionLedgerBootstrap(
  queryClient: QueryClient,
  sessionId: string,
  bootstrap: SessionLedgerBootstrap,
  options?: { skipBackfill?: boolean },
): void {
  const prev = readOfficialSessionLedgerCache(queryClient, sessionId);
  const events = mergeSessionEventsBySeq(prev?.events ?? [], bootstrap.events);
  const total = bootstrap.pagesLoaded > 0 ? bootstrap.total : (prev?.total ?? 0);
  const exhausted =
    Boolean(bootstrap.exhausted) || Boolean(prev?.exhausted && total <= (prev?.total ?? 0));
  const complete = exhausted || isSessionLedgerComplete(events.length, total);
  writeOfficialSessionLedgerCache(queryClient, sessionId, {
    events,
    total,
    pagesLoaded: complete
      ? Math.max(
          bootstrap.pagesLoaded,
          total > 0 ? sessionLedgerTargetPages(total) : bootstrap.pagesLoaded,
        )
      : bootstrap.pagesLoaded,
    exhausted,
    isBackfilling: prev?.isBackfilling,
  });
  if (!options?.skipBackfill && !complete) {
    void backfillSessionLedger(queryClient, sessionId, bootstrap.pagesLoaded);
  }
}

/** 从 bootstrap 页数之后补到 total 对应页（或触顶 MAX_PAGES） */
export async function backfillSessionLedger(
  queryClient: QueryClient,
  sessionId: string,
  fromPagesLoaded: number,
): Promise<void> {
  if (!sessionId.startsWith('ses_') || isOfficialPendingSessionId(sessionId)) return;

  const existing = backfillInflight.get(sessionId);
  if (existing) return existing;

  const promise = (async () => {
    const cached = readOfficialSessionLedgerCache(queryClient, sessionId);
    if (!cached) return;
    if (isLedgerComplete(cached)) return;

    writeOfficialSessionLedgerCache(queryClient, sessionId, {
      ...cached,
      isBackfilling: true,
    });
    try {
      const result = await fetchAllSessionEvents(sessionId, SESSION_EVENTS_PAGE_SIZE, {
        events: cached.events,
        pagesLoaded: fromPagesLoaded,
        total: cached.total,
        exhausted: cached.exhausted,
      });

      const prev = readOfficialSessionLedgerCache(queryClient, sessionId) ?? cached;
      writeOfficialSessionLedgerCache(queryClient, sessionId, {
        events: mergeSessionEventsBySeq(prev.events, result.events),
        total: result.total,
        pagesLoaded: Math.max(result.pagesLoaded, prev.pagesLoaded),
        exhausted: result.exhausted,
        isBackfilling: false,
      });
    } catch {
      // 历史补页失败不影响实时产物；之后重新进入会话时可以重试。
    } finally {
      const latest = readOfficialSessionLedgerCache(queryClient, sessionId);
      if (latest?.isBackfilling) {
        writeOfficialSessionLedgerCache(queryClient, sessionId, {
          ...latest,
          isBackfilling: false,
        });
      }
    }
  })().finally(() => {
    backfillInflight.delete(sessionId);
  });

  backfillInflight.set(sessionId, promise);
  return promise;
}

export interface OlderLedgerEventsBatch {
  events: SessionLedgerEvent[];
  hasMore: boolean;
}

/** 上滚优先读缓存，避免重复打 events 接口 */
export function pickOlderEventsFromLedgerCache(
  cache: OfficialSessionLedgerCache | undefined,
  seenEventKeys: ReadonlySet<string>,
  pageSize: number = SESSION_EVENTS_PAGE_SIZE,
): OlderLedgerEventsBatch | null {
  if (!cache?.events.length) return null;

  const ordered = [...cache.events].sort(comparePlatformSessionLedgerEvents);
  const orderByEventKey = new Map(
    ordered.map((event, index) => [sessionLedgerEventKey(event), index]),
  );
  const earliestSeenIndex = ordered.findIndex((event) =>
    seenEventKeys.has(sessionLedgerEventKey(event)),
  );
  const olderBoundary = earliestSeenIndex >= 0 ? earliestSeenIndex : ordered.length;
  const older = cache.events
    .filter(
      (event) =>
        !seenEventKeys.has(sessionLedgerEventKey(event)) &&
        (orderByEventKey.get(sessionLedgerEventKey(event)) ?? ordered.length) < olderBoundary,
    )
    .sort((left, right) => comparePlatformSessionLedgerEvents(right, left))
    .slice(0, pageSize)
    .sort(comparePlatformSessionLedgerEvents);

  if (older.length === 0) {
    return isLedgerComplete(cache) ? { events: [], hasMore: false } : null;
  }

  const oldestInBatch = older[0];
  const stillHidden = oldestInBatch
    ? cache.events.some(
        (event) =>
          comparePlatformSessionLedgerEvents(event, oldestInBatch) < 0 &&
          !seenEventKeys.has(sessionLedgerEventKey(event)),
      )
    : false;
  return {
    events: older,
    hasMore: stillHidden || !isLedgerComplete(cache),
  };
}

export function useOfficialSessionLedger(sessionId: string) {
  const queryClient = useQueryClient();
  const enabled = sessionId.startsWith('ses_') && !isOfficialPendingSessionId(sessionId);

  const query = useQuery({
    queryKey: officialSessionLedgerQueryKey(sessionId),
    queryFn: () =>
      readOfficialSessionLedgerCache(queryClient, sessionId) ?? {
        events: [],
        total: 0,
        pagesLoaded: 0,
      },
    enabled,
    staleTime: 0,
    gcTime: 10 * 60_000,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (!enabled) return;
    const cache = readOfficialSessionLedgerCache(queryClient, sessionId);
    if (!cache || isLedgerComplete(cache)) return;

    void backfillSessionLedger(queryClient, sessionId, cache.pagesLoaded);
  }, [
    enabled,
    query.data?.events.length,
    query.data?.pagesLoaded,
    query.data?.total,
    queryClient,
    sessionId,
  ]);

  const cache = readOfficialSessionLedgerCache(queryClient, sessionId);

  return {
    data: cache?.events,
    isLoading: !cache && enabled,
    isFetching: false,
    isLedgerComplete: isLedgerComplete(cache),
    isBackfilling: Boolean(cache?.isBackfilling) || backfillInflight.has(sessionId),
  };
}
