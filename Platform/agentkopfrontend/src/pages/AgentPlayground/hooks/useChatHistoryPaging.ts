/**
 * 对话历史分页：进会话拉最新 execution 的完整事件（必要时补页），上滚到顶再补更早对话
 * 账本接口按 seq 倒序分页；合并进 ledgerStore 后全量重放，与 stream 续订事件按 seq 拼接
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { CHAT_LOAD_MORE_THRESHOLD_PX, SESSION_LEDGER_BOOTSTRAP_PAGES } from '../constants';
import type { SessionLedgerEvent } from '../types';
import { fetchSessionEventsPage, type SessionLedgerBootstrap } from '../utils';
import {
  comparePlatformSessionLedgerEvents,
  sessionLedgerEventKey,
} from '../utils/sessionLedgerOrder';

export interface OlderHistoryBatch {
  events: SessionLedgerEvent[];
  hasMore: boolean;
}

/** 整页都是已渲染过的事件时，最多再往前翻几页，避免一次滚动打太多请求 */
const MAX_PAGES_PER_SCROLL = 5;
/** 进会话 bootstrap：最多补拉几页，覆盖「最新一页不含 user.input」的长 execution */
const MAX_BOOTSTRAP_PAGES = SESSION_LEDGER_BOOTSTRAP_PAGES;

export interface LoadLatestOptions {
  /** 第一页就绪：先渲染最新内容并订 stream，其余页继续后台补 */
  onFirstPage?: (bootstrap: SessionLedgerBootstrap) => void;
  /** 每拉完一页写入共享账本（产物/上滚缓存） */
  onPageLoaded?: (bootstrap: SessionLedgerBootstrap) => void;
  /** 限制 bootstrap 最多拉几页；账本缓存已齐时切换会话可设为 1 */
  maxBootstrapPages?: number;
  /** 账本已齐：合并缓存 events，并同步上滚游标 */
  adoptWarmLedger?: SessionLedgerBootstrap;
}
/** 全量重放后等待内容撑开 scrollHeight 的上限 */
const SCROLL_RESTORE_TIMEOUT_MS = 320;

function hasUserInputForExecution(
  events: Iterable<SessionLedgerEvent>,
  executionId?: string,
): boolean {
  if (!executionId) {
    return [...events].some((event) => event.kind === 'user.input');
  }
  return [...events].some(
    (event) => event.kind === 'user.input' && event.executionId === executionId,
  );
}

interface UseChatHistoryPagingOptions {
  /** 上滚加载更早历史：合并 ledgerStore 后全量重放；生成中返回 false 表示未插入 */
  prependReplayEvents: (history: SessionLedgerEvent[]) => boolean;
  /** 生成中不插入更早历史，避免打断流式现场 */
  isBusy?: boolean;
  /** 优先从本地账本缓存取更早事件；返回 null 时走 events 接口 */
  resolveOlderEvents?: (
    sessionId: string,
    seenEventKeys: ReadonlySet<string>,
  ) => OlderHistoryBatch | null;
  /** 网络翻页拉取到 events 后同步写入共享账本（供产物汇总） */
  onEventsPageLoaded?: (sessionId: string, bootstrap: SessionLedgerBootstrap) => void;
}

function isNearScrollTop(el: HTMLElement): boolean {
  return el.scrollTop <= CHAT_LOAD_MORE_THRESHOLD_PX;
}

/**
 * 全量重放后 React 可能晚一帧才撑开高度；过早回填会让 scrollTop 卡在 0，无法连续上滚加载。
 */
function restoreScrollAfterHistoryLoad(
  el: HTMLElement,
  prevHeight: number,
  prevTop: number,
  shouldRestore: () => boolean,
): Promise<void> {
  return new Promise((resolve) => {
    let settled = false;
    const apply = () => {
      // 会话已切换时，旧会话的异步锚点恢复不能覆盖新会话的贴底位置。
      if (!shouldRestore()) return;
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

export function useChatHistoryPaging({
  prependReplayEvents,
  isBusy = false,
  resolveOlderEvents,
  onEventsPageLoaded,
}: UseChatHistoryPagingOptions) {
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  /** 已加载到第几页（1 = 最新一页） */
  const pageRef = useRef(0);
  const hasMoreRef = useRef(false);
  /** 已渲染过的事件，按 eventId / executionId+seq 跨页去重 */
  const seenEventKeysRef = useRef(new Set<string>());
  const loadingOlderRef = useRef(false);
  const sessionRef = useRef<string | null>(null);
  const isBusyRef = useRef(isBusy);
  const handleChatScrollRef = useRef<(chatRef: React.RefObject<HTMLDivElement | null>) => void>(
    () => {},
  );

  const resolveOlderEventsRef = useRef(resolveOlderEvents);
  resolveOlderEventsRef.current = resolveOlderEvents;
  const onEventsPageLoadedRef = useRef(onEventsPageLoaded);
  onEventsPageLoadedRef.current = onEventsPageLoaded;

  useEffect(() => {
    isBusyRef.current = isBusy;
  }, [isBusy]);

  const reset = useCallback((sessionId: string | null = null) => {
    sessionRef.current = sessionId;
    pageRef.current = 0;
    hasMoreRef.current = false;
    seenEventKeysRef.current = new Set<string>();
    loadingOlderRef.current = false;
  }, []);

  /**
   * 打开会话 bootstrap：至少覆盖「当前 execution 含 user.input」所需页数。
   * 不拉全量历史——更早 execution 由上滚分页补齐；total≤200 也不再阻塞扫完所有页。
   */
  const loadLatest = useCallback(
    async (sessionId: string, options?: LoadLatestOptions): Promise<SessionLedgerBootstrap> => {
      reset(sessionId);
      const byEventKey = new Map<string, SessionLedgerEvent>();
      let page = 0;
      let apiHasMore = true;
      let eventTotal = 0;
      let firstPageNotified = false;
      const pageCap = options?.maxBootstrapPages ?? MAX_BOOTSTRAP_PAGES;

      while (apiHasMore && page < pageCap) {
        page += 1;
        const batch = await fetchSessionEventsPage(sessionId, page);
        if (sessionRef.current !== sessionId) {
          return { events: [], total: 0, pagesLoaded: 0, exhausted: false };
        }

        if (batch.total > 0) eventTotal = batch.total;

        for (const event of batch.events) {
          byEventKey.set(sessionLedgerEventKey(event), event);
        }

        apiHasMore = batch.hasMore;
        pageRef.current = page;
        hasMoreRef.current = apiHasMore;

        const loaded = [...byEventKey.values()].sort(comparePlatformSessionLedgerEvents);
        const latestExecutionId = loaded[loaded.length - 1]?.executionId;

        options?.onPageLoaded?.({
          events: loaded,
          total: eventTotal,
          pagesLoaded: page,
          exhausted: !apiHasMore,
        });

        if (!firstPageNotified && page === 1 && options?.onFirstPage) {
          firstPageNotified = true;
          options.onFirstPage({
            events: loaded,
            total: eventTotal,
            pagesLoaded: page,
            exhausted: !apiHasMore,
          });
        }

        if (hasUserInputForExecution(loaded, latestExecutionId)) {
          break;
        }
        if (!apiHasMore || batch.events.length === 0) break;
      }

      if (options?.adoptWarmLedger) {
        const warm = options.adoptWarmLedger;
        for (const event of warm.events) {
          byEventKey.set(sessionLedgerEventKey(event), event);
        }
        if (warm.total > 0) eventTotal = Math.max(eventTotal, warm.total);
        const events = [...byEventKey.values()].sort(comparePlatformSessionLedgerEvents);
        seenEventKeysRef.current = new Set(events.map(sessionLedgerEventKey));
        pageRef.current = warm.pagesLoaded;
        hasMoreRef.current = warm.total > 0 && events.length < warm.total;
        return {
          events,
          total: eventTotal || warm.total,
          pagesLoaded: warm.pagesLoaded,
          exhausted: warm.exhausted,
        };
      }

      const events = [...byEventKey.values()].sort(comparePlatformSessionLedgerEvents);
      seenEventKeysRef.current = new Set(events.map(sessionLedgerEventKey));
      return { events, total: eventTotal, pagesLoaded: page, exhausted: !apiHasMore };
    },
    [reset],
  );

  const maybeContinueLoading = useCallback((chatRef: React.RefObject<HTMLDivElement | null>) => {
    const el = chatRef.current;
    if (!el || !sessionRef.current) return;
    if (!hasMoreRef.current || isBusyRef.current || loadingOlderRef.current) return;
    if (!isNearScrollTop(el)) return;
    requestAnimationFrame(() => handleChatScrollRef.current(chatRef));
  }, []);

  /** 滚动到顶部附近时补一页更早的对话，并按高度差回填滚动位置 */
  const handleChatScroll = useCallback(
    (chatRef: React.RefObject<HTMLDivElement | null>) => {
      const el = chatRef.current;
      const sessionId = sessionRef.current;
      if (!el || !sessionId) return;
      if (loadingOlderRef.current || !hasMoreRef.current || isBusyRef.current) return;
      if (!isNearScrollTop(el)) return;

      loadingOlderRef.current = true;
      setIsLoadingHistory(true);

      void (async () => {
        // 锚点：插入更早消息后按高度差回填，保持用户当前阅读位置
        const prevHeight = el.scrollHeight;
        const prevTop = el.scrollTop;
        try {
          const cachedBatch = resolveOlderEventsRef.current?.(sessionId, seenEventKeysRef.current);

          if (cachedBatch) {
            hasMoreRef.current = cachedBatch.hasMore;
            if (cachedBatch.events.length === 0) return;

            if (!prependReplayEvents(cachedBatch.events)) return;
            for (const event of cachedBatch.events) {
              seenEventKeysRef.current.add(sessionLedgerEventKey(event));
            }

            const node = chatRef.current;
            if (node) {
              await restoreScrollAfterHistoryLoad(
                node,
                prevHeight,
                prevTop,
                () => sessionRef.current === sessionId,
              );
              maybeContinueLoading(chatRef);
            }
            return;
          }

          const older: SessionLedgerEvent[] = [];
          let page = pageRef.current;
          let hasMore = hasMoreRef.current;
          for (
            let attempt = 0;
            attempt < MAX_PAGES_PER_SCROLL && hasMore && older.length === 0;
            attempt += 1
          ) {
            page += 1;
            const res = await fetchSessionEventsPage(sessionId, page);
            if (sessionRef.current !== sessionId) return;
            hasMore = res.hasMore;
            onEventsPageLoadedRef.current?.(sessionId, {
              events: res.events,
              total: res.total,
              pagesLoaded: page,
            });
            for (const event of res.events) {
              if (seenEventKeysRef.current.has(sessionLedgerEventKey(event))) continue;
              older.push(event);
            }
          }

          pageRef.current = page;
          hasMoreRef.current = hasMore;

          // 生成中未插入：保持页码与游标，下次滚动重试
          if (older.length > 0 && !prependReplayEvents(older)) return;

          for (const event of older) {
            seenEventKeysRef.current.add(sessionLedgerEventKey(event));
          }

          if (older.length > 0) {
            const node = chatRef.current;
            if (node) {
              await restoreScrollAfterHistoryLoad(
                node,
                prevHeight,
                prevTop,
                () => sessionRef.current === sessionId,
              );
              maybeContinueLoading(chatRef);
            }
          } else if (hasMore) {
            // 本批全是已见过的 seq，直接尝试下一页
            maybeContinueLoading(chatRef);
          }
        } catch {
          // 静默失败，下次滚动可重试
        } finally {
          if (sessionRef.current === sessionId) setIsLoadingHistory(false);
          loadingOlderRef.current = false;
        }
      })();
    },
    [maybeContinueLoading, prependReplayEvents],
  );

  handleChatScrollRef.current = handleChatScroll;

  return { isLoadingHistory, setIsLoadingHistory, loadLatest, reset, handleChatScroll };
}
