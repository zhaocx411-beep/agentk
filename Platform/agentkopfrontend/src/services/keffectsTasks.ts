/**
 * keffects 异步任务：一键换装 / 商品详情抓取 / 广告成片共用同一路由，
 * 入参靠 taskType + input 区分。音色列表仍走 clothing-dupe/voice-packs。
 */
import { del, get, post } from '@/utils/request';
import { assertOutboundMediaWithinLimit } from './mediaSizeGuard';

export const KEFFECTS_TASKS_PATH = '/agent/operation/backend/platform/keffects/tasks';

export const CLOTHING_DUPE_WORKFLOW_KEY = 'pipeline:keffects.e-commerce-clothing-dupe';
export const GOODS_SCRAPER_WORKFLOW_KEY = 'pipeline:keffects.e-commerce-goods-scraper';
export const GOODS_STUDIO_WORKFLOW_KEY = 'pipeline:keffects.e-commerce-goods-studio';

export const CLOTHING_DUPE_TASK_TYPE = 'e-commerce-clothing-dupe';
export const GOODS_SCRAPER_TASK_TYPE = 'e-commerce-goods-scraper';
export const GOODS_STUDIO_TASK_TYPE = 'e-commerce-goods-studio';

export type KeffectsExperienceKind = 'clothing-dupe' | 'goods-scraper' | 'goods-studio';

export type KeffectsTaskStatus =
  | 'SUBMITTED'
  | 'PROCESSING'
  | 'QUEUED'
  | 'RUNNING'
  | 'SUCCEEDED'
  | 'FAILED'
  | 'CANCELLING'
  | 'CANCELLED';

export interface KeffectsTaskError {
  code?: string;
  message?: string;
  details?: Record<string, unknown>;
}

/** 部分列表/详情接口用 outputJson，轮询用 output */
export type KeffectsTaskRaw<TOutput = unknown> = KeffectsTask<TOutput> & {
  outputJson?: TOutput | null;
};

export interface KeffectsTask<TOutput = unknown> {
  taskId: string;
  taskType?: string;
  requestId?: string;
  status: KeffectsTaskStatus | string;
  stage?: string;
  progress?: number;
  createTime?: number;
  updateTime?: number;
  output?: TOutput | null;
  error?: KeffectsTaskError | null;
  callbackPayload?: unknown;
}

export async function createKeffectsTask<TOutput = unknown>(body: object) {
  await assertOutboundMediaWithinLimit({ payload: body });
  return post<KeffectsTask<TOutput>>(KEFFECTS_TASKS_PATH, body);
}

export function getKeffectsTask<TOutput = unknown>(taskId: string) {
  return get<KeffectsTaskRaw<TOutput>>(
    `${KEFFECTS_TASKS_PATH}/${encodeURIComponent(taskId)}`,
    undefined,
    { headers: { 'X-No-Loading': '1' } },
  ).then(normalizeKeffectsTaskOutput);
}

/** 统一 output / outputJson，避免列表与轮询字段不一致 */
export function normalizeKeffectsTaskOutput<TOutput>(
  task: KeffectsTaskRaw<TOutput>,
): KeffectsTask<TOutput> {
  const output = task.output ?? task.outputJson ?? null;
  return { ...task, output };
}

/** 轮询合并：新响应缺 output 时保留已有产物 */
export function mergeKeffectsTask<TOutput>(
  prev: KeffectsTask<TOutput> | null,
  next: KeffectsTaskRaw<TOutput>,
): KeffectsTask<TOutput> {
  const normalized = normalizeKeffectsTaskOutput(next);
  if (!prev) return normalized;
  return {
    ...prev,
    ...normalized,
    output: normalized.output ?? prev.output ?? null,
  };
}

export function cancelKeffectsTask<TOutput = unknown>(taskId: string) {
  return del<KeffectsTask<TOutput>>(`${KEFFECTS_TASKS_PATH}/${encodeURIComponent(taskId)}`);
}

export function isKeffectsTaskActive(status?: string): boolean {
  return (
    status === 'SUBMITTED' ||
    status === 'PROCESSING' ||
    status === 'QUEUED' ||
    status === 'RUNNING'
  );
}

export function resolvePollErrorMessage(error: unknown): string {
  if (!error || typeof error !== 'object') return '查询任务状态失败';
  const maybe = error as {
    message?: string;
    response?: { status?: number; data?: { message?: string } };
  };
  const bodyMessage = maybe.response?.data?.message?.trim();
  if (bodyMessage) return bodyMessage;
  if (maybe.response?.status === 500) return '查询任务状态失败，服务器错误';
  if (maybe.response?.status === 404) return '任务不存在或已失效';
  if (maybe.message && !maybe.message.startsWith('Request failed')) return maybe.message;
  return '查询任务状态失败';
}

export function resolveFailedTask<TOutput>(
  current: KeffectsTask<TOutput> | null,
  taskId: string,
  error: unknown,
): KeffectsTask<TOutput> {
  return {
    ...(current ?? { taskId, status: 'FAILED' }),
    taskId: current?.taskId || taskId,
    status: 'FAILED',
    error: { message: resolvePollErrorMessage(error) },
  };
}

/** 取消接口成功后停轮询；后端可能长期停在 CANCELLING */
export function resolveCancelledTask<TOutput>(
  current: KeffectsTask<TOutput>,
  cancelled?: KeffectsTask<TOutput> | null,
): KeffectsTask<TOutput> {
  const extra = cancelled && typeof cancelled === 'object' ? cancelled : {};
  return {
    ...current,
    ...extra,
    taskId: current.taskId,
    status: 'CANCELLED',
  };
}

export function createKeffectsRequestId(prefix = 'order'): string {
  const date = new Date();
  const stamp = [
    date.getFullYear(),
    String(date.getMonth() + 1).padStart(2, '0'),
    String(date.getDate()).padStart(2, '0'),
  ].join('');
  return `${prefix}-${stamp}-${Date.now().toString().slice(-8)}`;
}

function haystackOf(
  workflowKey?: string,
  item?: { name?: string; description?: string; pipelineName?: string } | null,
): string {
  return [workflowKey, item?.name, item?.description, item?.pipelineName]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
}

/** 按 workflowKey / 名称识别 keffects 体验，避免走 schema 动态表单 */
export function resolveKeffectsKind(
  workflowKey?: string,
  item?: { name?: string; description?: string; pipelineName?: string } | null,
): KeffectsExperienceKind | null {
  const haystack = haystackOf(workflowKey, item);
  if (!haystack) return null;
  if (
    workflowKey === CLOTHING_DUPE_WORKFLOW_KEY ||
    /e-commerce-clothing-dupe|clothing.?dupe|换装/.test(haystack)
  ) {
    return 'clothing-dupe';
  }
  if (
    workflowKey === GOODS_SCRAPER_WORKFLOW_KEY ||
    /e-commerce-goods-scraper|goods.?scraper|商品详情抓取/.test(haystack)
  ) {
    return 'goods-scraper';
  }
  if (
    workflowKey === GOODS_STUDIO_WORKFLOW_KEY ||
    /e-commerce-goods-studio|goods.?studio|广告成片/.test(haystack)
  ) {
    return 'goods-studio';
  }
  return null;
}
