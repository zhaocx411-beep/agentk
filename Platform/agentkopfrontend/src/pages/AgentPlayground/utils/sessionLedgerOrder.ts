import type { SessionLedgerEvent } from '../types';

type LedgerEventIdentity = Pick<
  SessionLedgerEvent,
  'seq' | 'kind' | 'createdAt' | 'eventId' | 'executionId'
>;

/** 跨 stream / 平台历史优先以全局 eventId 去重；缺失时才退化为 execution + seq。 */
export function sessionLedgerEventKey(event: LedgerEventIdentity): string {
  const eventId = event.eventId?.trim();
  if (eventId) return `event:${eventId}`;

  const executionId = event.executionId?.trim();
  if (executionId) return `execution:${executionId}:seq:${event.seq}`;

  return `fallback:${event.createdAt}:${event.seq}:${event.kind}`;
}

function eventTime(event: Pick<SessionLedgerEvent, 'createdAt'>): number {
  const value = Date.parse(event.createdAt);
  return Number.isFinite(value) ? value : 0;
}

/** 平台 /events 的 seq 是会话级全局顺序，历史回放必须以它为准。 */
export function comparePlatformSessionLedgerEvents(
  left: SessionLedgerEvent,
  right: SessionLedgerEvent,
): number {
  if (left.seq !== right.seq) return left.seq - right.seq;
  return sessionLedgerEventKey(left).localeCompare(sessionLedgerEventKey(right));
}

/** 会话历史跨 execution 时按事件时间排序，同一 execution 内再按 seq 保序。 */
export function compareSessionLedgerEvents(
  left: SessionLedgerEvent,
  right: SessionLedgerEvent,
): number {
  const timeDiff = eventTime(left) - eventTime(right);
  if (timeDiff !== 0) return timeDiff;

  const leftExecutionId = left.executionId?.trim() ?? '';
  const rightExecutionId = right.executionId?.trim() ?? '';
  if (leftExecutionId && leftExecutionId === rightExecutionId && left.seq !== right.seq) {
    return left.seq - right.seq;
  }

  return sessionLedgerEventKey(left).localeCompare(sessionLedgerEventKey(right));
}
