import { beforeEach, describe, expect, it, vi } from 'vitest';

const requestMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}));

vi.mock('@/utils/request', () => ({
  get: requestMocks.get,
  post: requestMocks.post,
}));

import {
  getCapabilityInvocation,
  getCapabilityRun,
  startCapabilityRun,
} from './api';

const invocation = {
  invocationId: 'inv_1',
  platformSessionId: 477,
  capabilityId: 'agentk.generate_image',
  status: 'running',
  args: { prompt: '测试图片' },
  output: {},
  mediaArtifacts: [],
  imageUrls: [],
  videoUrls: [],
  execution: {
    id: 'inv_1',
    sessionId: 'inv_1',
    name: 'capability_run',
    status: 'running',
    createdAt: '2026-09-07T11:40:35.638Z',
  },
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('capability invoke API', () => {
  it('starts an invocation and maps invocationId to the existing result model', async () => {
    requestMocks.post.mockResolvedValue(invocation);

    const result = await startCapabilityRun({
      capabilityId: 'agentk.generate_image',
      args: { prompt: '测试图片' },
    });

    expect(requestMocks.post).toHaveBeenCalledWith(
      '/agent/operation/backend/capabilities/invoke',
      {
        capabilityId: 'agentk.generate_image',
        args: { prompt: '测试图片' },
      },
      { timeout: 120_000 },
    );
    expect(result).toMatchObject({
      sessionId: 'inv_1',
      executionId: 'inv_1',
      status: 'running',
      execution: {
        id: 'inv_1',
        sessionId: 'inv_1',
        args: { prompt: '测试图片' },
      },
    });
  });

  it('polls a live invocation and normalizes its response', async () => {
    requestMocks.get.mockResolvedValue({
      ...invocation,
      status: 'completed',
      execution: {
        ...invocation.execution,
        status: 'completed',
        output: { imageUrls: ['https://example.com/result.png'] },
      },
    });

    const result = await getCapabilityInvocation('inv_1');

    expect(requestMocks.get).toHaveBeenCalledWith(
      '/agent/operation/backend/capabilities/invoke',
      { invocationId: 'inv_1' },
      expect.any(Object),
    );
    expect(result).toMatchObject({
      executionId: 'inv_1',
      sessionId: 'inv_1',
      status: 'completed',
    });
  });

  it('loads an invocation result from runs with both identifiers', async () => {
    requestMocks.get.mockResolvedValue({
      sessionId: 'inv_1',
      executionId: 'inv_1',
      status: 'completed',
      execution: {
        ...invocation.execution,
        status: 'completed',
        output: { imageUrls: ['https://example.com/result.png'] },
      },
    });

    const result = await getCapabilityRun('inv_1');

    expect(requestMocks.get).toHaveBeenCalledWith(
      '/agent/operation/backend/capabilities/runs',
      { executionId: 'inv_1', sessionId: 'inv_1' },
      expect.any(Object),
    );
    expect(result.execution.output).toEqual({
      imageUrls: ['https://example.com/result.png'],
    });
  });

  it('keeps distinct execution and session identifiers for older history records', async () => {
    requestMocks.get.mockResolvedValue({});

    await getCapabilityRun('exe_old', 'ses_old');

    expect(requestMocks.get).toHaveBeenCalledWith(
      '/agent/operation/backend/capabilities/runs',
      { executionId: 'exe_old', sessionId: 'ses_old' },
      expect.any(Object),
    );
  });
});
