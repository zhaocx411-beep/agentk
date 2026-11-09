/**
 * Capability Lab 执行接口
 * POST /agent/operation/backend/capabilities/invoke
 * GET /agent/operation/backend/capabilities/invoke（实时轮询）
 * GET /agent/operation/backend/capabilities/runs（结果回溯）
 */

export type CapabilityRunStatus =
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | (string & Record<never, never>);

export interface CapabilityArtifact {
  type: string;
  uri: string;
  description?: string;
  model?: string;
  params?: Record<string, unknown>;
}

export interface CapabilityMediaArtifact {
  type: string;
  url: string;
  source?: string;
}

export interface CapabilityExecution {
  id: string;
  sessionId: string;
  name: string;
  status: CapabilityRunStatus;
  output?: Record<string, unknown> | null;
  mediaArtifacts?: CapabilityMediaArtifact[];
  imageUrls?: string[];
  videoUrls?: string[];
  profileVersion?: string;
  error?: string;
  createdAt?: string;
  finishedAt?: string;
  args?: Record<string, unknown>;
}

export interface CapabilityRunResult {
  sessionId: string;
  executionId: string;
  pipeline?: string;
  status: CapabilityRunStatus;
  execution: CapabilityExecution;
}

export interface StartCapabilityRunRequest {
  capabilityId: string;
  title?: string;
  args: Record<string, unknown>;
}

export interface CapabilityRunHistoryRecord {
  userId: string;
  capabilityId: string;
  sessionId: string;
  executionId: string;
  status: CapabilityRunStatus;
  args: Record<string, unknown>;
  output?: Record<string, unknown> | null;
  error?: string;
  createdAt: string;
  finishedAt?: string;
}

export interface CapabilityRunHistoryData {
  page: number;
  pageSize: number;
  total: number;
  records: CapabilityRunHistoryRecord[];
}

export interface CapabilityRunHistoryQuery {
  capabilityId?: string;
  userId?: string;
  status?: CapabilityRunStatus;
  page?: number;
  pageSize?: number;
}
