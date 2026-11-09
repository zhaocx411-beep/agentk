import { QueryClient } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const utilsMocks = vi.hoisted(() => ({
  fetchAllSessionEvents: vi.fn(),
}));

vi.mock('@/pages/AgentPlayground/utils', () => utilsMocks);

import {
  backfillSessionLedger,
  mergeSessionEventsBySeq,
  officialSessionLedgerQueryKey,
  type OfficialSessionLedgerCache,
} from './useOfficialSessionLedger';

beforeEach(() => {
  vi.clearAllMocks();
});

function cache(isBackfilling = false): OfficialSessionLedgerCache {
  return {
    events: [
      {
        seq: 20,
        kind: 'assistant.text',
        payload: { text: '最新消息' },
        createdAt: '2026-09-02T12:00:00.000Z',
      },
    ],
    total: 40,
    pagesLoaded: 1,
    isBackfilling,
  };
}

describe('backfillSessionLedger status', () => {
  it('publishes the actual backfill state through query cache', async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(officialSessionLedgerQueryKey('ses_status'), cache());
    let resolveBackfill:
      | ((value: { events: []; total: number; pagesLoaded: number }) => void)
      | undefined;
    utilsMocks.fetchAllSessionEvents.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveBackfill = resolve;
        }),
    );

    const promise = backfillSessionLedger(queryClient, 'ses_status', 1);
    expect(
      queryClient.getQueryData<OfficialSessionLedgerCache>(
        officialSessionLedgerQueryKey('ses_status'),
      )?.isBackfilling,
    ).toBe(true);

    resolveBackfill?.({ events: [], total: 40, pagesLoaded: 2 });
    await promise;
    expect(
      queryClient.getQueryData<OfficialSessionLedgerCache>(
        officialSessionLedgerQueryKey('ses_status'),
      )?.isBackfilling,
    ).toBe(false);
  });

  it('clears the status when historical backfill fails', async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(officialSessionLedgerQueryKey('ses_failed'), cache());
    utilsMocks.fetchAllSessionEvents.mockRejectedValue(new Error('network error'));

    await backfillSessionLedger(queryClient, 'ses_failed', 1);
    expect(
      queryClient.getQueryData<OfficialSessionLedgerCache>(
        officialSessionLedgerQueryKey('ses_failed'),
      )?.isBackfilling,
    ).toBe(false);
  });

  it('stops backfilling after the server reports an empty end page', async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(officialSessionLedgerQueryKey('ses_exhausted'), cache());
    utilsMocks.fetchAllSessionEvents.mockResolvedValue({
      events: [],
      total: 2,
      pagesLoaded: 2,
      exhausted: true,
    });

    await backfillSessionLedger(queryClient, 'ses_exhausted', 1);
    expect(
      queryClient.getQueryData<OfficialSessionLedgerCache>(
        officialSessionLedgerQueryKey('ses_exhausted'),
      ),
    ).toMatchObject({
      total: 2,
      pagesLoaded: 2,
      exhausted: true,
      isBackfilling: false,
    });
    expect(utilsMocks.fetchAllSessionEvents).toHaveBeenCalledTimes(1);
    await backfillSessionLedger(queryClient, 'ses_exhausted', 2);

    expect(utilsMocks.fetchAllSessionEvents).toHaveBeenCalledTimes(1);
  });
});

describe('mergeSessionEventsBySeq', () => {
  it('keeps events whose execution-local seq values are equal', () => {
    const merged = mergeSessionEventsBySeq(
      [
        {
          executionId: 'exe_old',
          seq: 1,
          kind: 'user.input',
          createdAt: '2026-09-03T05:00:00.000Z',
        },
      ],
      [
        {
          executionId: 'exe_new',
          seq: 1,
          kind: 'execution.started',
          createdAt: '2026-09-03T06:00:00.000Z',
        },
      ],
    );

    expect(merged).toHaveLength(2);
    expect(merged.map((event) => event.executionId)).toEqual([
      'exe_old',
      'exe_new',
    ]);
  });
});
