/**
 * 离线跑数 - API 接口
 */
import type {
  DatasetExportFormat,
  DatasetQueryParams,
  DatasetQueryResponse,
  DatasetTask,
  DatasetTaskCreateParams,
  DatasetTaskQueryParams,
  DatasetTaskQueryResponse,
  DatasetTaskUpdateParams,
  DatasetUpdateParams,
  DatasetUpdateResponse,
  DatasetUploadParams,
  DatasetUploadResponse,
  DatasetTemplateUploadParams,
  AgentConfig,
  OfflineJobSubmitParams,
  OfflineJob,
  OfflineJobQueryParams,
  OfflineJobQueryResponse,
  JobCell,
  OfflineJobExport,
  TraceDetail,
} from './types';
import request, { get, post, patch, del } from '@/utils/request';

// ==================== 数据集管理 API ====================

/** 查询数据集列表（usage 必传：EVAL / TEMPLATE） */
export async function queryDatasets(params: DatasetQueryParams): Promise<DatasetQueryResponse> {
  return get<DatasetQueryResponse>('/agent/operation/backend/datasets/query', params);
}

/**
 * 查询数据集用例列表（分页；含 referImages / extraFields）
 * datasetId / usage / pipelineName 至少传一个；
 * 创作工作台模版数据：usage=TEMPLATE，通常再带 pipelineName
 */
export async function queryDatasetTasks(
  params: DatasetTaskQueryParams,
): Promise<DatasetTaskQueryResponse> {
  return get<DatasetTaskQueryResponse>('/agent/operation/backend/datasets/tasks/query', params);
}

/** 上传数据集（支持 yaml / json / xlsx；xlsx 须带 taskInstruction） */
export async function uploadDataset(params: DatasetUploadParams): Promise<DatasetUploadResponse> {
  const formData = new FormData();
  formData.append('file', params.file);
  formData.append('datasetType', params.datasetType);
  formData.append('name', params.name);
  if (params.version) {
    formData.append('version', params.version);
  }
  if (params.description) {
    formData.append('description', params.description);
  }
  if (params.usage) {
    formData.append('usage', params.usage);
  }
  if (params.pipelineName) {
    formData.append('pipelineName', params.pipelineName);
  }
  if (params.taskInstruction) {
    formData.append('taskInstruction', params.taskInstruction);
  }
  // FormData 上传需要绕过默认 JSON Content-Type，用 axios 实例直接发
  const { default: request } = await import('@/utils/request');
  const res = await request({
    method: 'POST',
    url: '/agent/operation/backend/datasets/upload',
    data: formData,
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  const data = res.data as { code: number; message: string; data: DatasetUploadResponse };
  if (data.code !== 200 && data.code !== 0) {
    throw new Error(data.message || '上传失败');
  }
  return data.data;
}

/** 上传数据集素材/模版（仅 .json；usage 固定 TEMPLATE） */
export async function uploadDatasetTemplate(
  params: DatasetTemplateUploadParams,
): Promise<DatasetUploadResponse> {
  const formData = new FormData();
  formData.append('file', params.file);
  formData.append('datasetType', params.datasetType);
  formData.append('name', params.name);
  formData.append('pipelineName', params.pipelineName);
  if (params.description) {
    formData.append('description', params.description);
  }
  if (params.version) {
    formData.append('version', params.version);
  }
  const { default: request } = await import('@/utils/request');
  const res = await request({
    method: 'POST',
    url: '/agent/operation/backend/datasets/templates/upload',
    data: formData,
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  const data = res.data as { code: number; message: string; data: DatasetUploadResponse };
  if (data.code !== 200 && data.code !== 0) {
    throw new Error(data.message || '上传失败');
  }
  return data.data;
}

/** 更新数据集元信息（描述 / 类型） */
export async function updateDataset(
  id: number,
  params: DatasetUpdateParams,
): Promise<DatasetUpdateResponse> {
  return patch<DatasetUpdateResponse>(`/agent/operation/backend/datasets/${id}`, params);
}

/** 删除数据集（被离线任务引用时后端返回 412） */
export async function deleteDataset(id: number): Promise<void> {
  return del<void>(`/agent/operation/backend/datasets/delete/${id}`);
}

/** 新增单条用例 */
export async function createDatasetTask(
  datasetId: number,
  params: DatasetTaskCreateParams,
): Promise<DatasetTask> {
  return post<DatasetTask>(`/agent/operation/backend/datasets/${datasetId}/tasks`, params);
}

/** 更新单条用例（prompt 与 linkUrl 至少传一项；linkUrl 传空字符串表示清空） */
export async function updateDatasetTask(
  taskId: number,
  params: DatasetTaskUpdateParams,
): Promise<DatasetTask> {
  return patch<DatasetTask>(`/agent/operation/backend/datasets/tasks/${taskId}`, params);
}

/** 删除单条用例（数据集至少保留 1 条，删除最后一条后端返回 412） */
export async function deleteDatasetTask(taskId: number): Promise<void> {
  return del<void>(`/agent/operation/backend/datasets/tasks/${taskId}`);
}

/** 下载数据集（默认 YAML，支持 ?format=json / xlsx） */
export function downloadDataset(
  id: number,
  name: string,
  version: string,
  format: DatasetExportFormat = 'yaml',
): void {
  const base = `${import.meta.env.VITE_API_BASE_URL}/agent/operation/backend/datasets/${id}/export`;
  const url = format === 'yaml' ? base : `${base}?format=${format}`;
  const link = document.createElement('a');
  link.href = url;
  link.download = `${name}-${version}.${format}`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
}

// ==================== 离线跑数 API ====================

/** 获取 Agent 可选配置（含 agents 列表，供离线评测选择） */
export async function getAgentConfig(): Promise<AgentConfig> {
  return get<AgentConfig>('/agent/operation/backend/agent-config');
}

/** 提交离线跑数任务（datasetId + agentId + sessionMode；配置取已发布 Agent 版本快照） */
export async function submitOfflineJob(params: OfflineJobSubmitParams): Promise<OfflineJob> {
  return post<OfflineJob>('/agent/operation/backend/offline-jobs/submit', params);
}

/** 查询离线跑数任务列表（含 pipeline） */
export async function queryOfflineJobs(
  params?: OfflineJobQueryParams,
): Promise<OfflineJobQueryResponse> {
  return get<OfflineJobQueryResponse>('/agent/operation/backend/offline-jobs/query', params);
}

/** 查询离线跑数任务详情（含 pipeline） */
export async function getOfflineJobDetail(bizId: string): Promise<OfflineJob> {
  return get<OfflineJob>(`/agent/operation/backend/offline-jobs/detail`, { bizId });
}

/** 查询离线跑数用例执行结果（含 pipelineExecId / outputUris） */
export async function queryJobCells(bizId: string): Promise<JobCell[]> {
  return get<JobCell[]>('/agent/operation/backend/offline-jobs/cells/query', { bizId });
}

/** 查询用例执行 Trace（勾选 pipeline 时含 pipeline 节点 / loopError / pipelineError） */
export async function queryCellTrace(bizId: string, taskKey: string): Promise<TraceDetail> {
  return get<TraceDetail>('/agent/operation/backend/offline-jobs/cells/trace/query', {
    bizId,
    taskKey,
  });
}

/**
 * 删除离线跑数任务。
 * 约束：RUNNING 拒绝；非终态（非 SUCCEEDED/FAILED）也拒绝。
 */
export async function deleteOfflineJob(bizId: string): Promise<void> {
  return del<void>('/agent/operation/backend/offline-jobs/delete', { bizId });
}

function triggerJsonSave(data: unknown, filename: string): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], {
    type: 'application/json;charset=utf-8',
  });
  const objectUrl = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = objectUrl;
  link.download = filename;
  link.rel = 'noopener';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000);
}

function isExportPayload(value: unknown): value is OfflineJobExport {
  if (!value || typeof value !== 'object') return false;
  const rec = value as Record<string, unknown>;
  return typeof rec.bizId === 'string' && Array.isArray(rec.cells);
}

function unwrapExportPayload(body: unknown): OfflineJobExport | null {
  if (isExportPayload(body)) return body;
  if (!body || typeof body !== 'object') return null;
  const data = (body as { data?: unknown }).data;
  return isExportPayload(data) ? data : null;
}

/**
 * 下载离线跑数产出数据。
 * 接口返回任务 JSON（bizId / cells / output_uris / trace_json），落盘为 `{bizId}.json`。
 */
export async function downloadOfflineJobExport(bizId: string): Promise<string> {
  const res = await request({
    method: 'GET',
    url: '/agent/operation/backend/offline-jobs/export',
    params: { bizId },
    timeout: 120000,
  });
  const payload = unwrapExportPayload(res.data);
  if (!payload) {
    throw new Error('暂无产出数据');
  }
  const filename = `${payload.bizId}.json`;
  triggerJsonSave(payload, filename);
  return filename;
}
