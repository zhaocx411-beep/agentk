import { describe, expect, it } from 'vitest';
import {
  isCapabilityRunNotReady,
  resolveCapabilityRunDeadline,
} from './runErrors';
import { CAPABILITY_RUN_TIMEOUT_MS } from '../constants';

describe('isCapabilityRunNotReady', () => {
  it('skips legacy code 600 responses', () => {
    expect(isCapabilityRunNotReady({ code: 600, message: 'get session failed' })).toBe(true);
  });

  it('skips HTTP 404 execution visibility delays', () => {
    expect(
      isCapabilityRunNotReady({
        response: {
          status: 404,
          data: {
            code: 404,
            message: 'NOT_FOUND: execution not found',
          },
        },
      }),
    ).toBe(true);
  });

  it('skips runs responses requested before an invocation completes', () => {
    expect(
      isCapabilityRunNotReady({
        response: {
          status: 200,
          data: {
            code: 400,
            message: "output not ready, status='running'",
          },
        },
      }),
    ).toBe(true);
  });

  it('does not hide unrelated backend failures', () => {
    expect(
      isCapabilityRunNotReady({
        response: {
          status: 500,
          data: {
            code: 500,
            message: 'INTERNAL_ERROR',
          },
        },
      }),
    ).toBe(false);
  });
});

describe('resolveCapabilityRunDeadline', () => {
  it('uses the backend creation time when available', () => {
    const createdAt = '2026-09-02T08:00:00.000Z';
    expect(resolveCapabilityRunDeadline(createdAt)).toBe(
      Date.parse(createdAt) + CAPABILITY_RUN_TIMEOUT_MS,
    );
  });

  it('falls back to the current time for invalid timestamps', () => {
    expect(resolveCapabilityRunDeadline('invalid', 1000)).toBe(
      1000 + CAPABILITY_RUN_TIMEOUT_MS,
    );
  });
});
