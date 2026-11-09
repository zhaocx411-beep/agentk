/**
 * api 类型 pipeline-agent 历史列表
 * GET /platform/chat/pipeline-execs?pipelineName=&page=&pageSize=
 */
import { get } from '@/utils/request';
import type { PlatformPageResult } from '@/types/api';

export interface PipelineExecListRecord {
  id: number;
  publicId: string;
  tenantId?: string;
  userId?: string;
  sessionId: number;
  workflowId?: number;
  agentkSessionId?: string;
  agentkExecId?: string;
  pipelineName: string;
  argsJson?: Record<string, unknown>;
  outputJson?: Record<string, unknown>;
  profileVersion?: string;
  status: string;
  error?: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt?: string;
  updatedAt?: string;
}

export const PIPELINE_EXECS_PAGE_SIZE = 20;

export interface PipelineExecListResult {
  records: PipelineExecListRecord[];
  page: number;
  pageSize: number;
  total: number;
  hasMore: boolean;
}

export function queryPipelineExecsPage(params: {
  pipelineName: string;
  page: number;
  pageSize?: number;
}): Promise<PipelineExecListResult> {
  const pageSize = params.pageSize ?? PIPELINE_EXECS_PAGE_SIZE;
  return get<PlatformPageResult<PipelineExecListRecord>>(
    '/agent/operation/backend/platform/chat/pipeline-execs',
    {
      pipelineName: params.pipelineName,
      page: params.page,
      pageSize,
    },
    { headers: { 'X-No-Loading': '1' } },
  ).then((data) => ({
    records: data.records ?? [],
    page: data.page,
    pageSize: data.pageSize,
    total: data.total,
    hasMore: data.page * data.pageSize < data.total,
  }));
}
