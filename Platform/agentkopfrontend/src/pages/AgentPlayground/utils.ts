/**
 * AgentPlayground - 页面级工具函数
 * 会话账本拉取与版本快照 JSON 解析，均为纯函数 / 无状态请求，供页面与 hooks 复用
 */
import { getSessionEvents } from './api';
import {
  SESSION_EVENTS_PAGE_SIZE,
  SESSION_LEDGER_MAX_PAGES,
  sessionLedgerTargetPages,
} from './constants';
import type { SessionLedgerEvent } from './types';
import { fetchSessionEventsPageDeduped, type SessionEventsPage } from './utils/sessionEventsFetch';
import {
  comparePlatformSessionLedgerEvents,
  sessionLedgerEventKey,
} from './utils/sessionLedgerOrder';

export type { SessionEventsPage };

/** 进会话 bootstrap 结果 */
export interface SessionLedgerBootstrap {
  events: SessionLedgerEvent[];
  total: number;
  pagesLoaded: number;
  /** 已收到空页、hasMore=false，或已达到 total 对应的最后一页 */
  exhausted?: boolean;
}

async function fetchSessionEventsPageRaw(
  sessionId: string,
  page: number,
  pageSize: number = SESSION_EVENTS_PAGE_SIZE,
): Promise<SessionEventsPage> {
  const res = await getSessionEvents(sessionId, { page, pageSize });
  const events = [...(res.events || [])].sort(comparePlatformSessionLedgerEvents);
  return {
    events,
    page: res.page || page,
    hasMore: res.hasMore && events.length > 0,
    total: res.total ?? 0,
  };
}

/** 拉取一页账本事件：page=1 是最新的一页（同参并发去重） */
export async function fetchSessionEventsPage(
  sessionId: string,
  page: number,
  pageSize: number = SESSION_EVENTS_PAGE_SIZE,
): Promise<SessionEventsPage> {
  return fetchSessionEventsPageDeduped(fetchSessionEventsPageRaw, sessionId, page, pageSize);
}

/** 按时间升序拉取会话全部账本事件（自动翻页，带上限；可跳过已 bootstrap 的页） */
export async function fetchAllSessionEvents(
  sessionId: string,
  pageSize: number = SESSION_EVENTS_PAGE_SIZE,
  bootstrap?: Pick<SessionLedgerBootstrap, 'events' | 'pagesLoaded' | 'total' | 'exhausted'>,
  maxPages: number = SESSION_LEDGER_MAX_PAGES,
): Promise<SessionLedgerBootstrap> {
  const byEventKey = new Map<string, SessionLedgerEvent>();
  for (const event of bootstrap?.events ?? []) {
    byEventKey.set(sessionLedgerEventKey(event), event);
  }

  let page = bootstrap?.pagesLoaded ?? 0;
  let hasMore = true;
  let total = bootstrap?.total ?? 0;
  let exhausted = bootstrap?.exhausted ?? false;

  while (
    hasMore &&
    !exhausted &&
    page < maxPages &&
    (total <= 0 || page < sessionLedgerTargetPages(total))
  ) {
    page += 1;
    const res = await fetchSessionEventsPage(sessionId, page, pageSize);
    if (res.total > 0) total = res.total;
    for (const event of res.events) {
      byEventKey.set(sessionLedgerEventKey(event), event);
    }
    hasMore = res.hasMore;
    exhausted =
      res.events.length === 0 ||
      !res.hasMore ||
      (total > 0 && page >= sessionLedgerTargetPages(total)) ||
      (total > 0 && byEventKey.size >= total);
  }

  const events = [...byEventKey.values()].sort(comparePlatformSessionLedgerEvents);
  return { events, total, pagesLoaded: page, exhausted };
}

/** 解析 JSON 数组字符串为 string[]，非法时返回 undefined */
export function parseJsonStringArray(json?: string): string[] | undefined {
  if (!json) return undefined;
  try {
    const value: unknown = JSON.parse(json);
    return Array.isArray(value)
      ? value.filter((v): v is string => typeof v === 'string')
      : undefined;
  } catch {
    return undefined;
  }
}

/** 从版本快照 specJson 中取 tools 数组，非法时返回 undefined（按无快照处理） */
export function parseSpecTools(specJson?: string): string[] | undefined {
  if (!specJson) return undefined;
  try {
    const value: unknown = JSON.parse(specJson);
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      const tools = (value as Record<string, unknown>).tools;
      return Array.isArray(tools)
        ? tools.filter((v): v is string => typeof v === 'string')
        : undefined;
    }
  } catch {
    return undefined;
  }
  return undefined;
}
