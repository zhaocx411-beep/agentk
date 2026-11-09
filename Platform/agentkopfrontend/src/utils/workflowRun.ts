/**
 * Pipeline / 工作流运行相关的公共工具
 * 编辑器（WorkflowEditor）、运行页（WorkflowRunner）、工作流 Agent 详情三处共用，
 * 抽到全局避免各自维护一份状态映射导致行为漂移
 */
import { createSession, getCapabilities } from '@/services/workflow';
import type { getWorkflowRunDetail } from '@/services/workflow';
import type {
  CreateSessionRequest,
  Session,
  WorkflowNodeStatus,
  WorkflowRun,
} from '@/pages/AgentPlayground/types';
import { buildSessionProfile } from '@/utils/profile';

/** 后端节点状态字符串 → 前端 WorkflowNodeStatus，未知状态按等待处理 */
export function mapNodeStatus(status: string): WorkflowNodeStatus {
  if (status === 'completed' || status === 'failed' || status === 'running' || status === 'skipped') {
    return status;
  }
  return 'pending';
}

const PIPELINE_RUN_STATUS: Record<string, WorkflowRun['status']> = {
  completed: 'completed',
  failed: 'failed',
};

/** 后端 Pipeline 运行状态 → 前端三态，其余按运行中 */
export function mapPipelineRunStatus(status?: string): WorkflowRun['status'] {
  return PIPELINE_RUN_STATUS[status ?? ''] ?? 'running';
}

/** 运行详情接口 → WorkflowRun 的节点 / 输出补丁 */
export function detailToPatch(
  detail: NonNullable<Awaited<ReturnType<typeof getWorkflowRunDetail>>>,
) {
  return {
    nodes: (detail.nodes ?? []).map((n) => ({
      name: n.nodeId,
      label: n.nodeId,
      status: mapNodeStatus(n.status),
      output: n.output,
    })),
    output: detail.output,
    status: mapPipelineRunStatus(detail.status),
    error: detail.status === 'failed' ? detail.error || '执行失败' : undefined,
  };
}

/**
 * Pipeline 会话统一按实例能力目录建会话：先读取 capabilities，把目录里的
 * tool / pipeline 全量暴露给会话，再创建 session。
 *
 * 未发布的草稿定义已无法随 profile 下发（新 Profile schema 去掉了 workflows 字段），
 * 调试未发布定义请改用 workflowVersionId 让平台绑定该版本。
 */
export async function createPipelineSession(
  body: Omit<CreateSessionRequest, 'profile' | 'sessionKind'>,
): Promise<Session> {
  const catalog = await getCapabilities().catch(() => null);
  const profile = buildSessionProfile({ catalog, toolNames: catalog?.toolNames ?? [] });
  return createSession({ ...body, profile, sessionKind: 'pipeline' });
}
