import { describe, expect, it, vi } from 'vitest';

const requestMocks = vi.hoisted(() => ({
  request: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  del: vi.fn(),
}));

vi.mock('@/utils/request', () => ({
  default: requestMocks.request,
  get: requestMocks.get,
  post: requestMocks.post,
  patch: requestMocks.patch,
  del: requestMocks.del,
}));

import { submitOfflineJob } from './api';

describe('submitOfflineJob', () => {
  it('submits the selected session mode explicitly', async () => {
    requestMocks.post.mockResolvedValue({ bizId: 'eval_1' });

    await submitOfflineJob({
      datasetId: 100,
      agentId: 20,
      sessionMode: 'GROUPED',
    });

    expect(requestMocks.post).toHaveBeenCalledWith('/agent/operation/backend/offline-jobs/submit', {
      datasetId: 100,
      agentId: 20,
      sessionMode: 'GROUPED',
    });
  });
});
