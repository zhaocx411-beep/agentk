import { describe, expect, it } from 'vitest';
import type { SessionLedgerEvent } from '../types';
import {
  comparePlatformSessionLedgerEvents,
  compareSessionLedgerEvents,
  sessionLedgerEventKey,
} from './sessionLedgerOrder';

function event(executionId: string, seq: number, createdAt: string): SessionLedgerEvent {
  return {
    executionId,
    seq,
    kind: 'assistant.text',
    payload: {},
    createdAt,
  };
}

describe('session ledger ordering', () => {
  it('does not treat equal seq values from different executions as duplicates', () => {
    const first = event('exe_1', 1, '2026-09-03T05:00:00.000Z');
    const second = event('exe_2', 1, '2026-09-03T06:00:00.000Z');

    expect(sessionLedgerEventKey(first)).not.toBe(sessionLedgerEventKey(second));
  });

  it('orders different executions by event time instead of their local seq', () => {
    const earlier = event('exe_1', 19, '2026-09-03T05:42:07.833Z');
    const later = event('exe_2', 2, '2026-09-03T05:45:00.000Z');

    expect([later, earlier].sort(compareSessionLedgerEvents)).toEqual([earlier, later]);
  });

  it('orders platform history by its session-level global seq', () => {
    const earlier = event('exe_1', 9, '2026-09-03T08:30:05.000Z');
    const later = event('exe_2', 10, '2026-09-03T08:30:01.000Z');

    expect([later, earlier].sort(comparePlatformSessionLedgerEvents)).toEqual([earlier, later]);
  });
});
