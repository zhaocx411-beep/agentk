/**
 * 工具执行轮询：识别可跳过的瞬时错误（session 未入库 / 接口抖动）
 */
import type { CapabilityRunResult } from '@/types/capabilityRuns';
import { CAPABILITY_RUN_TIMEOUT_MESSAGE, CAPABILITY_RUN_TIMEOUT_MS } from '../constants';

interface ErrorPayload {
  code?: number;
  message: string;
}

function readErrorPayload(error: unknown): ErrorPayload {
  if (!error || typeof error !== 'object') return { message: '' };
  const record = error as {
    code?: unknown;
    message?: unknown;
    response?: { data?: { code?: unknown; message?: unknown }; status?: number };
  };
  const data = record.response?.data;
  const codeRaw = data?.code ?? record.code;
  const code = typeof codeRaw === 'number' ? codeRaw : Number(codeRaw);
  const message = String(data?.message || record.message || '');
  return { code: Number.isFinite(code) ? code : undefined, message };
}

/**
 * 查询 runs 时 session/execution 尚未可见，或后端瞬时 NOT_FOUND。
 * 这类错误不是 capability 执行失败，轮询应跳过并继续。
 */
export function isCapabilityRunNotReady(error: unknown): boolean {
  const { code, message } = readErrorPayload(error);
  if (code === 600) return true;
  return /NOT_FOUND|output not ready|status\s*=\s*running/i.test(message);
}

/** 根据 createdAt 计算轮询截止时间；缺失则从现在起算 */
export function resolveCapabilityRunDeadline(createdAt?: string, now = Date.now()): number {
  if (createdAt) {
    const started = Date.parse(createdAt);
    if (Number.isFinite(started)) {
      return started + CAPABILITY_RUN_TIMEOUT_MS;
    }
  }
  return now + CAPABILITY_RUN_TIMEOUT_MS;
}

export function buildTimedOutRunResult(
  executionId: string,
  sessionId: string,
  capabilityId: string,
  prev: CapabilityRunResult | null,
): CapabilityRunResult {
  return {
    sessionId,
    executionId,
    pipeline: prev?.pipeline,
    status: 'cancelled',
    execution: {
      id: executionId,
      sessionId,
      name: prev?.execution.name ?? capabilityId,
      status: 'cancelled',
      args: prev?.execution.args,
      output: prev?.execution.output,
      error: CAPABILITY_RUN_TIMEOUT_MESSAGE,
      createdAt: prev?.execution.createdAt,
      finishedAt: new Date().toISOString(),
    },
  };
}
