/**
 * Pipeline 持久化共享 API 层
 * 对接 /agent/operation/backend/platform/workflows* 接口
 */
import { get, post } from '@/utils/request';
import type {
  WorkflowEntity,
  WorkflowVersion,
  CreateWorkflowEntityRequest,
  SaveWorkflowVersionDraftRequest,
} from '@/types/workflowPersistence';

const BASE = '/agent/operation/backend/platform';

/** React Query key：我的 Pipeline 列表 */
export const WORKFLOW_ENTITIES_QUERY_KEY = ['workflow-entities'] as const;
/** React Query key：某 Pipeline 的版本列表 */
export const workflowVersionsKey = (workflowId: number) =>
  ['workflow-versions', workflowId] as const;

/** 创建 Pipeline（逻辑实体，latestVersion=0，status=active） */
export function createWorkflowEntity(body: CreateWorkflowEntityRequest) {
  return post<WorkflowEntity>(`${BASE}/workflows`, body);
}

/** 我的 Pipeline 列表（含 latestVersion 等） */
export function getWorkflowEntities() {
  return get<WorkflowEntity[]>(`${BASE}/workflows`);
}

/** Pipeline 的定义版本列表（按 version 降序，含草稿与已发布） */
export function getWorkflowVersions(workflowId: number) {
  return get<WorkflowVersion[]>(`${BASE}/workflows/${workflowId}/versions`);
}

/** 保存版本草稿（version 自增，status=draft，不更新 latestVersion） */
export function saveWorkflowVersionDraft(
  workflowId: number,
  body: SaveWorkflowVersionDraftRequest,
) {
  return post<WorkflowVersion>(`${BASE}/workflows/${workflowId}/versions`, body);
}

/** 发布版本（draft → published；若版本号更大则回写 latestVersion） */
export function publishWorkflowVersion(workflowVersionId: number) {
  return post<WorkflowVersion>(`${BASE}/workflow-versions/${workflowVersionId}/publish`);
}
