import { describe, expect, it } from 'vitest';
import type { Session, SessionLedgerEvent } from '../types';
import {
  isExecutionFinishedInLedger,
  pickResumeExecutionId,
} from './sessionStreamResume';

function session(overrides: Partial<Session> = {}): Session {
  return {
    id: 'ses_test',
    title: '测试会话',
    ...overrides,
  };
}

function event(
  seq: number,
  kind: string,
  executionId = 'exe_current',
  payload?: unknown,
): SessionLedgerEvent {
  return {
    seq,
    kind,
    executionId,
    payload,
    createdAt: '2026-09-02T08:00:00.000Z',
  };
}

describe('pickResumeExecutionId', () => {
  it('resumes an execution that has input but no terminal event', () => {
    const events = [
      event(1, 'execution.started'),
      event(2, 'user.input', 'exe_current', { text: '生成视频' }),
    ];

    expect(pickResumeExecutionId(session(), events)).toBe('exe_current');
  });

  it('does not resume a cancelled execution', () => {
    const events = [
      event(1, 'execution.started'),
      event(2, 'user.input'),
      event(3, 'execution.finished', 'exe_current', {
        status: 'EXECUTION_STATUS_CANCELLED',
        error: 'canceled',
      }),
    ];

    expect(pickResumeExecutionId(session(), events)).toBe('');
  });

  it('recognizes terminal status from payloadJson', () => {
    const finished = {
      ...event(3, 'execution.finished'),
      payloadJson: JSON.stringify({ status: 'EXECUTION_STATUS_FAILED' }),
      payload: undefined,
    };

    expect(isExecutionFinishedInLedger([finished], 'exe_current')).toBe(true);
  });

  it('prefers a newer open execution over an older finished one', () => {
    const events = [
      event(1, 'execution.started', 'exe_old'),
      event(2, 'execution.finished', 'exe_old', {
        status: 'EXECUTION_STATUS_COMPLETED',
      }),
      event(3, 'execution.started', 'exe_new'),
      event(4, 'user.input', 'exe_new'),
    ];

    expect(
      pickResumeExecutionId(
        session({
          lastExecutionId: 'exe_old',
          activeRunStatus: 'completed',
        }),
        events,
      ),
    ).toBe('exe_new');
  });

  it('uses active session metadata when the ledger page is empty', () => {
    expect(
      pickResumeExecutionId(
        session({
          lastExecutionId: 'exe_from_session',
          activeRunStatus: 'waiting_job',
        }),
      ),
    ).toBe('exe_from_session');
  });
});
