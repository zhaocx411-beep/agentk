/**
 * 会话 events 分页请求去重：loadLatest 与 useOfficialSessionLedger 并行时共用 in-flight Promise
 */
import type { SessionLedgerEvent } from '../types';

export interface SessionEventsPage {
  events: SessionLedgerEvent[];
  page: number;
  hasMore: boolean;
  total: number;
}

const inflightPages = new Map<string, Promise<SessionEventsPage>>();

function pageFetchKey(sessionId: string, page: number, pageSize: number): string {
  return `${sessionId}\0${page}\0${pageSize}`;
}

export function fetchSessionEventsPageDeduped(
  fetchPage: (sessionId: string, page: number, pageSize: number) => Promise<SessionEventsPage>,
  sessionId: string,
  page: number,
  pageSize: number,
): Promise<SessionEventsPage> {
  const key = pageFetchKey(sessionId, page, pageSize);
  const existing = inflightPages.get(key);
  if (existing) return existing;

  const promise = fetchPage(sessionId, page, pageSize).finally(() => {
    inflightPages.delete(key);
  });
  inflightPages.set(key, promise);
  return promise;
}
