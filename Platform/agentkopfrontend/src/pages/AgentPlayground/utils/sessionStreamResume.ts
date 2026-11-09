/**
 * 切换历史会话时，判断是否应使用 sessions/query 里的 executionId 续订实时流。
 * 若账本最新一页已落库 execution.finished（终态），则不再订 stream。
 */
import type { Session, SessionLedgerEvent } from '../types';
import { comparePlatformSessionLedgerEvents } from './sessionLedgerOrder';

const ACTIVE_STREAM_STATUSES = new Set(['running', 'waiting_job', 'waiting_user']);

const TERMINAL_EXECUTION_STATUS_MARKERS = [
  'EXECUTION_STATUS_COMPLETED',
  'EXECUTION_STATUS_CANCELLED',
  'EXECUTION_STATUS_FAILED',
] as const;

function readExecutionId(session: Session): string {
  return session.lastExecutionId?.trim() || session.lastAgentkRunId?.trim() || '';
}

function readRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function readExecutionFinishedStatus(event: SessionLedgerEvent): string {
  const direct = readRecord(event.data) ?? readRecord(event.payload);
  if (direct && typeof direct.status === 'string') {
    return direct.status.trim();
  }
  const raw = event.payloadJson?.trim();
  if (!raw) return '';
  try {
    const parsed: unknown = JSON.parse(raw);
    const record = readRecord(parsed);
    return typeof record?.status === 'string' ? record.status.trim() : '';
  } catch {
    return '';
  }
}

function isTerminalExecutionStatus(status: string): boolean {
  if (!status) return true;
  return TERMINAL_EXECUTION_STATUS_MARKERS.some((marker) => status.includes(marker));
}

/** 账本中该 execution 的最新事件是否已是终态 execution.finished */
export function isExecutionFinishedInLedger(
  events: SessionLedgerEvent[],
  executionId: string,
): boolean {
  const trimmed = executionId.trim();
  if (!trimmed || events.length === 0) return false;

  const execEvents = events.filter((event) => event.executionId === trimmed);
  if (execEvents.length === 0) return false;

  const latest = execEvents.reduce((max, event) => (event.seq > max.seq ? event : max));
  if (latest.kind !== 'execution.finished') return false;
  return isTerminalExecutionStatus(readExecutionFinishedStatus(latest));
}

function pickResumeExecutionIdFromSession(session: Session | undefined | null): string {
  if (!session) return '';
  const executionId = readExecutionId(session);
  if (!executionId) return '';

  const status = session.activeRunStatus?.trim();
  if (!status) {
    return executionId;
  }
  if (ACTIVE_STREAM_STATUSES.has(status)) return executionId;
  return '';
}

/** 账本里尚未 execution.finished 的最新 execution（优先于可能滞后的 sessions 元数据） */
function pickLatestOpenExecutionId(events: SessionLedgerEvent[]): string {
  const execIds = new Set<string>();
  for (const event of events) {
    const id = event.executionId?.trim();
    if (id) execIds.add(id);
  }

  let best = '';
  let bestEvent: SessionLedgerEvent | undefined;
  for (const execId of execIds) {
    if (isExecutionFinishedInLedger(events, execId)) continue;
    const executionEvents = events
      .filter((event) => event.executionId?.trim() === execId)
      .sort(comparePlatformSessionLedgerEvents);
    const latestEvent = executionEvents[executionEvents.length - 1];
    if (
      latestEvent &&
      (!bestEvent || comparePlatformSessionLedgerEvents(latestEvent, bestEvent) > 0)
    ) {
      bestEvent = latestEvent;
      best = execId;
    }
  }
  return best;
}

/**
 * 结合会话元数据 + 账本事件决定是否续订 stream。
 * ledgerEvents 通常为 events?page=1；若该 execution 尚无 execution.finished 则续流，
 * 即使 sessions 列表的 activeRunStatus 已滞后为 completed。
 */
export function pickResumeExecutionId(
  session: Session | undefined | null,
  ledgerEvents: SessionLedgerEvent[] = [],
): string {
  const sessionExecutionId = session ? readExecutionId(session) : '';
  const openExecutionId = pickLatestOpenExecutionId(ledgerEvents);
  const executionId = openExecutionId || sessionExecutionId;
  if (!executionId) return '';

  if (isExecutionFinishedInLedger(ledgerEvents, executionId)) return '';

  // 账本已证明 execution 仍在进行 → 续流，不受 activeRunStatus 滞后影响
  if (openExecutionId) return executionId;

  return pickResumeExecutionIdFromSession(session);
}
