import type { SessionReviewQueryParams } from './types';

export const DEFAULT_SESSION_REVIEW_PAGE = 1;
export const DEFAULT_SESSION_REVIEW_PAGE_SIZE = 20;

const NUMERIC_FILTER_KEYS = [
  'agentLoopId',
  'agentLoopVersionId',
  'workflowId',
  'workflowVersionId',
] as const;

type NumericFilterKey = (typeof NUMERIC_FILTER_KEYS)[number];

function parsePositiveInteger(value: string | null, fallback?: number): number | undefined {
  if (!value) return fallback;
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function readTrimmed(searchParams: URLSearchParams, key: string): string | undefined {
  return searchParams.get(key)?.trim() || undefined;
}

/** 从地址栏恢复已执行的筛选条件和分页状态。 */
export function parseSessionReviewSearch(search: string): SessionReviewQueryParams {
  const searchParams = new URLSearchParams(search);
  const query: SessionReviewQueryParams = {
    page:
      parsePositiveInteger(searchParams.get('page'), DEFAULT_SESSION_REVIEW_PAGE) ??
      DEFAULT_SESSION_REVIEW_PAGE,
    pageSize:
      parsePositiveInteger(searchParams.get('pageSize'), DEFAULT_SESSION_REVIEW_PAGE_SIZE) ??
      DEFAULT_SESSION_REVIEW_PAGE_SIZE,
    userId: readTrimmed(searchParams, 'userId'),
    status: readTrimmed(searchParams, 'status'),
    createdFrom: readTrimmed(searchParams, 'createdFrom'),
    createdTo: readTrimmed(searchParams, 'createdTo'),
  };

  for (const key of NUMERIC_FILTER_KEYS) {
    const value = parsePositiveInteger(searchParams.get(key));
    if (value !== undefined) query[key as NumericFilterKey] = value;
  }

  return query;
}

/** 将已执行的查询写入地址栏，默认分页值省略以保持 URL 简洁。 */
export function serializeSessionReviewSearch(query: SessionReviewQueryParams): string {
  const searchParams = new URLSearchParams();

  if (query.page !== DEFAULT_SESSION_REVIEW_PAGE) {
    searchParams.set('page', String(query.page));
  }
  if (query.pageSize !== DEFAULT_SESSION_REVIEW_PAGE_SIZE) {
    searchParams.set('pageSize', String(query.pageSize));
  }

  for (const key of NUMERIC_FILTER_KEYS) {
    const value = query[key];
    if (value !== undefined) searchParams.set(key, String(value));
  }

  for (const key of ['userId', 'status', 'createdFrom', 'createdTo'] as const) {
    const value = query[key]?.trim();
    if (value) searchParams.set(key, value);
  }

  return searchParams.toString();
}
