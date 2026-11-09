/**
 * 历史会话列表分页 + 滚到底加载下一页（对齐调试台 Sidebar）
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import type { Session, SessionListResult } from '@/pages/AgentPlayground/types';
import { SESSIONS_PAGE_SIZE } from '@/pages/AgentPlayground/constants';

export { SESSIONS_PAGE_SIZE };

interface UseSessionListPaginationOptions {
  /** 拉取指定页；返回 records 已由 getSessions 映射为 Session */
  fetchPage: (page: number) => Promise<SessionListResult>;
  /** 切换 Agent / 官方卡片等时重置列表 */
  resetKey?: string | number | null;
  enabled?: boolean;
}

export function useSessionListPagination({
  fetchPage,
  resetKey = 'default',
  enabled = true,
}: UseSessionListPaginationOptions) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsHasMore, setSessionsHasMore] = useState(false);
  const [isLoadingMoreSessions, setIsLoadingMoreSessions] = useState(false);
  const historyListRef = useRef<HTMLDivElement>(null);
  const pageRef = useRef(0);
  const loadedKeyRef = useRef<string | number | null | undefined>(undefined);

  const mergeSessions = useCallback((prev: Session[], incoming: Session[]) => {
    if (!incoming.length) return prev;
    const ids = new Set(prev.map((item) => item.id));
    const appended = incoming.filter((item) => !ids.has(item.id));
    return appended.length ? [...prev, ...appended] : prev;
  }, []);

  const loadFirstPage = useCallback(async () => {
    setSessionsLoading(true);
    try {
      const res = await fetchPage(1);
      setSessions(res.sessions);
      pageRef.current = res.page;
      setSessionsHasMore(res.hasMore);
    } catch {
      setSessions([]);
      pageRef.current = 0;
      setSessionsHasMore(false);
    } finally {
      setSessionsLoading(false);
    }
  }, [fetchPage]);

  useEffect(() => {
    if (!enabled) return;
    const key = resetKey ?? 'default';
    if (loadedKeyRef.current === key) return;
    loadedKeyRef.current = key;
    void loadFirstPage();
  }, [enabled, resetKey, loadFirstPage]);

  const refreshSessions = useCallback(() => {
    loadedKeyRef.current = undefined;
    void loadFirstPage();
  }, [loadFirstPage]);

  const prependSession = useCallback((session: Session) => {
    setSessions((prev) => {
      const rest = prev.filter((item) => item.id !== session.id);
      return [session, ...rest];
    });
  }, []);

  const updateSessionTitle = useCallback((sessionId: string, title: string) => {
    setSessions((prev) =>
      prev.map((item) => (item.id === sessionId ? { ...item, title } : item)),
    );
  }, []);

  const handleHistoryScroll = useCallback(() => {
    const el = historyListRef.current;
    if (!el || isLoadingMoreSessions || !sessionsHasMore || sessionsLoading) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight >= 20) return;

    setIsLoadingMoreSessions(true);
    fetchPage(pageRef.current + 1)
      .then((res) => {
        if (res.sessions.length) {
          setSessions((prev) => mergeSessions(prev, res.sessions));
          pageRef.current = res.page;
        }
        setSessionsHasMore(res.hasMore && res.sessions.length > 0);
      })
      .catch(() => undefined)
      .finally(() => setIsLoadingMoreSessions(false));
  }, [fetchPage, isLoadingMoreSessions, mergeSessions, sessionsHasMore, sessionsLoading]);

  return {
    sessions,
    setSessions,
    sessionsLoading,
    sessionsHasMore,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    refreshSessions,
    prependSession,
    updateSessionTitle,
    invalidateLoadedKey: () => {
      loadedKeyRef.current = undefined;
    },
  };
}
