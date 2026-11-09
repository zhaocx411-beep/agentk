import { describe, expect, it } from 'vitest';
import { parseSessionReviewSearch, serializeSessionReviewSearch } from './listSearchParams';

describe('SessionReview list search params', () => {
  it('restores pagination and filters from the URL', () => {
    expect(
      parseSessionReviewSearch(
        '?page=4&pageSize=50&userId=wu_fuxingyi&status=active&agentLoopId=33&workflowVersionId=88&createdFrom=2026-09-01+00%3A00%3A00&createdTo=2026-09-07+23%3A59%3A59',
      ),
    ).toEqual({
      page: 4,
      pageSize: 50,
      userId: 'wu_fuxingyi',
      status: 'active',
      agentLoopId: 33,
      workflowVersionId: 88,
      createdFrom: '2026-09-01 00:00:00',
      createdTo: '2026-09-07 23:59:59',
    });
  });

  it('serializes non-default pagination and active filters', () => {
    expect(
      serializeSessionReviewSearch({
        page: 3,
        pageSize: 20,
        userId: '  user_1  ',
        status: 'active',
        agentLoopVersionId: 42,
      }),
    ).toBe('page=3&agentLoopVersionId=42&userId=user_1&status=active');
  });

  it('falls back to the default pagination for invalid URL values', () => {
    expect(parseSessionReviewSearch('?page=0&pageSize=oops&workflowId=-2')).toEqual({
      page: 1,
      pageSize: 20,
      userId: undefined,
      status: undefined,
      createdFrom: undefined,
      createdTo: undefined,
    });
  });
});
