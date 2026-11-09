/**
 * 数据回看 - 会话账本 events 分页（与 Agent 对话同源接口，上滚加载更早事件）
 */
import { useCallback, useRef, useState } from 'react';
import { getSessionEvents } from '@/pages/AgentPlayground/api';
import { CHAT_LOAD_MORE_THRESHOLD_PX, SESSION_EVENTS_PAGE_SIZE } from '@/pages/AgentPlayground/constants';
import type { SessionLedgerEvent } from '@/pages/AgentPlayground/types';
import { fetchSessionEventsPage } from '@/pages/AgentPlayground/utils';

const MAX_PAGES_PER_SCROLL = 5;
const SCROLL_RESTORE_TIMEOUT_MS = 320;

function isNearScrollTop(el: HTMLElement): boolean {
  return el.scrollTop <= CHAT_LOAD_MORE_THRESHOLD_PX;
}

function restoreScrollAfterHistoryLoad(
  el: HTMLElement,
  prevHeight: number,
  prevTop: number,
): Promise<void> {
  return new Promise((resolve) => {
    let settled = false;
    const apply = () => {
      el.scrollTop = Math.max(0, el.scrollHeight - prevHeight + prevTop);
    };
    const finish = () => {
      if (settled) return;
      settled = true;
      observer.disconnect();
      window.clearTimeout(fallbackTimer);
      apply();
      resolve();
    };

    if (el.scrollHeight > prevHeight + 1) {
      apply();
      resolve();
      return;
    }

    const target = el.firstElementChild ?? el;
    const observer = new ResizeObserver(() => {
      if (el.scrollHeight > prevHeight + 1) finish();
    });
    observer.observe(target);

    const fallbackTimer = window.setTimeout(finish, SCROLL_RESTORE_TIMEOUT_MS);
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (el.scrollHeight > prevHeight + 1) finish();
      });
    });
  });
}

export function useReviewSessionEvents() {
  const sessionRef = useRef<string | null>(null);
  const pageRef = useRef(0);
  const hasMoreRef = useRef(false);
  const seenSeqRef = useRef(new Set<number>());
  const loadingOlderRef = useRef(false);
  const initialLoadingRef = useRef(false);

  const [events, setEvents] = useState<SessionLedgerEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [error, setError] = useState('');
  const [total, setTotal] = useState(0);

  const reset = useCallback(() => {
    sessionRef.current = null;
    pageRef.current = 0;
    hasMoreRef.current = false;
    seenSeqRef.current = new Set();
    loadingOlderRef.current = false;
    initialLoadingRef.current = false;
    setEvents([]);
    setError('');
    setTotal(0);
    setLoadingOlder(false);
  }, []);

  const mergeEvents = useCallback((incoming: SessionLedgerEvent[]) => {
    setEvents((prev) => {
      const bySeq = new Map<number, SessionLedgerEvent>();
      for (const event of prev) bySeq.set(event.seq, event);
      for (const event of incoming) bySeq.set(event.seq, event);
      return [...bySeq.values()].sort((left, right) => left.seq - right.seq);
    });
  }, []);

  const loadInitial = useCallback(
    async (sessionId: string) => {
      reset();
      sessionRef.current = sessionId;
      initialLoadingRef.current = true;
      setLoading(true);
      setError('');
      try {
        const res = await getSessionEvents(sessionId, {
          page: 1,
          pageSize: SESSION_EVENTS_PAGE_SIZE,
        });
        if (sessionRef.current !== sessionId) return;
        const sorted = [...(res.events || [])].sort((left, right) => left.seq - right.seq);
        pageRef.current = 1;
        hasMoreRef.current = res.hasMore;
        seenSeqRef.current = new Set(sorted.map((event) => event.seq));
        setTotal(res.total);
        setEvents(sorted);
      } catch (err: unknown) {
        if (sessionRef.current !== sessionId) return;
        setError(err instanceof Error ? err.message : '拉取对话历史失败');
      } finally {
        initialLoadingRef.current = false;
        if (sessionRef.current === sessionId) setLoading(false);
      }
    },
    [reset],
  );

  const handleScroll = useCallback(
    (container: HTMLDivElement | null) => {
      const sessionId = sessionRef.current;
      if (!container || !sessionId) return;
      if (loadingOlderRef.current || !hasMoreRef.current || initialLoadingRef.current) return;
      if (!isNearScrollTop(container)) return;

      loadingOlderRef.current = true;
      setLoadingOlder(true);

      void (async () => {
        const prevHeight = container.scrollHeight;
        const prevTop = container.scrollTop;
        try {
          const older: SessionLedgerEvent[] = [];
          let page = pageRef.current;
          let hasMore = hasMoreRef.current;

          for (
            let attempt = 0;
            attempt < MAX_PAGES_PER_SCROLL && hasMore && older.length === 0;
            attempt += 1
          ) {
            page += 1;
            const batch = await fetchSessionEventsPage(sessionId, page);
            if (sessionRef.current !== sessionId) return;
            hasMore = batch.hasMore;
            for (const event of batch.events) {
              if (seenSeqRef.current.has(event.seq)) continue;
              older.push(event);
            }
          }

          pageRef.current = page;
          hasMoreRef.current = hasMore;

          if (older.length > 0) {
            for (const event of older) seenSeqRef.current.add(event.seq);
            mergeEvents(older);
            await restoreScrollAfterHistoryLoad(container, prevHeight, prevTop);
            if (isNearScrollTop(container) && hasMoreRef.current) {
              handleScroll(container);
            }
          } else if (hasMore) {
            handleScroll(container);
          }
        } catch {
          // 静默失败，下次滚动可重试
        } finally {
          if (sessionRef.current === sessionId) setLoadingOlder(false);
          loadingOlderRef.current = false;
        }
      })();
    },
    [mergeEvents],
  );

  return {
    events,
    loading,
    loadingOlder,
    error,
    total,
    loadInitial,
    reset,
    handleScroll,
  };
}
