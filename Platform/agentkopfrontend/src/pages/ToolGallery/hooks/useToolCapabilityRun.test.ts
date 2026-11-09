/** @vitest-environment jsdom */
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const apiMocks = vi.hoisted(() => ({
  cancelCapabilityRun: vi.fn(),
  getCapabilityInvocation: vi.fn(),
  getCapabilityRun: vi.fn(),
  startCapabilityRun: vi.fn(),
}));

vi.mock('../api', () => apiMocks);
vi.mock('@/utils/feedback', () => ({
  message: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}));

import { useToolCapabilityRun } from './useToolCapabilityRun';

const runningResult = {
  sessionId: 'ses_1',
  executionId: 'exe_1',
  status: 'running' as const,
  execution: {
    id: 'exe_1',
    sessionId: 'ses_1',
    name: 'generate_image',
    status: 'running' as const,
  },
};

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe('useToolCapabilityRun binding grace', () => {
  it('keeps querying briefly after the tool page unmounts', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(10_000);
    apiMocks.startCapabilityRun.mockResolvedValue(runningResult);
    apiMocks.getCapabilityInvocation.mockResolvedValue(runningResult);
    const { result, unmount } = renderHook(() =>
      useToolCapabilityRun({ capabilityId: 'generate_image' }),
    );

    await act(async () => {
      await result.current.runTool({ prompt: '生成图片' });
    });
    const callsBeforeLeave = apiMocks.getCapabilityInvocation.mock.calls.length;

    unmount();
    expect(apiMocks.getCapabilityInvocation.mock.calls.length).toBeGreaterThan(callsBeforeLeave);

    await vi.advanceTimersByTimeAsync(1000);
    expect(apiMocks.getCapabilityInvocation.mock.calls.length).toBeGreaterThanOrEqual(
      callsBeforeLeave + 4,
    );
  });

  it('waits for invoke completion before loading the result from runs', async () => {
    const completedResult = {
      ...runningResult,
      status: 'completed' as const,
      execution: {
        ...runningResult.execution,
        status: 'completed' as const,
        output: { artifacts: [] },
      },
    };
    apiMocks.startCapabilityRun.mockResolvedValue(runningResult);
    apiMocks.getCapabilityInvocation.mockResolvedValue(completedResult);
    apiMocks.getCapabilityRun.mockResolvedValue(completedResult);
    const { result, unmount } = renderHook(() =>
      useToolCapabilityRun({ capabilityId: 'generate_image' }),
    );

    await act(async () => {
      await result.current.runTool({ prompt: '生成图片' });
    });

    await waitFor(() => {
      expect(apiMocks.getCapabilityRun).toHaveBeenCalledWith('exe_1', 'ses_1');
      expect(result.current.runResult?.status).toBe('completed');
    });
    expect(
      apiMocks.getCapabilityInvocation.mock.invocationCallOrder[0],
    ).toBeLessThan(apiMocks.getCapabilityRun.mock.invocationCallOrder[0]);
    unmount();
  });
});
