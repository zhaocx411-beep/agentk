/**
 * Pipeline 持久化类型定义
 * 对接 /agent/operation/backend/platform/workflows* 接口
 */

/** Pipeline 逻辑实体 */
export interface WorkflowEntity {
  id: number;
  name: string;
  description: string;
  ownerUserId: string;
  tenantId: string;
  /** 线上最新发布版本号，0 表示未发布 */
  latestVersion: number;
  status: string;
  createdAt: string;
  updatedAt: string;
}

/** Pipeline 定义版本 */
export interface WorkflowVersion {
  id: number;
  workflowId: number;
  version: number;
  /** 通常为 JSON 字符串；兼容部分环境已反序列化为 object */
  definitionJson: string | Record<string, unknown>;
  contentHash: string;
  status: 'draft' | 'published' | string;
  createdBy: string;
  createdAt: string;
  publishedAt?: string;
  publishedBy?: string;
}

/** 创建 Pipeline 请求体 */
export interface CreateWorkflowEntityRequest {
  /** 名字 */
  name: string;
  /** 描述 */
  description: string;
}

/** 保存版本草稿请求体 */
export interface SaveWorkflowVersionDraftRequest {
  definitionJson: string;
}
