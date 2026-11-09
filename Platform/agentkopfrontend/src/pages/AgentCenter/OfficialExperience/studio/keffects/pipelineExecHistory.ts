/**
 * pipeline-execs 列表 →「我的生成」记录
 */
import type { PipelineExecListRecord } from '@/services/pipelineExecs';
import type { GenerationRecord } from '../lib/types';

export function execRecordKey(exec: PipelineExecListRecord): string {
  return exec.publicId || String(exec.id);
}

export function statusFromPipelineExecStatus(status: string): GenerationRecord['status'] {
  const upper = status.toUpperCase();
  if (['RUNNING', 'PROCESSING', 'QUEUED', 'SUBMITTED'].includes(upper)) return 'running';
  if (upper === 'SUCCEEDED' || upper === 'COMPLETED') return 'done';
  if (upper === 'FAILED') return 'failed';
  if (upper === 'CANCELLED' || upper === 'CANCELLING') return 'cancelled';
  return null;
}

export function promptFromPipelineExecArgs(argsJson?: Record<string, unknown>): string {
  if (!argsJson) return '历史记录';
  const url = typeof argsJson.url === 'string' ? argsJson.url.trim() : '';
  if (url) return url;

  const contents = argsJson.contents;
  if (Array.isArray(contents)) {
    for (const item of contents) {
      if (!item || typeof item !== 'object') continue;
      const rec = item as Record<string, unknown>;
      const type = typeof rec.type === 'string' ? rec.type : '';
      if (type === 'goods_title' && typeof rec.text === 'string' && rec.text.trim()) {
        return rec.text.trim();
      }
      if (type === 'prompt' && typeof rec.text === 'string' && rec.text.trim()) {
        return rec.text.trim();
      }
      if ((type === 'url' || rec.url) && typeof rec.url === 'string' && rec.url.trim()) {
        return rec.url.trim();
      }
    }
  }

  const title = typeof argsJson.title === 'string' ? argsJson.title.trim() : '';
  if (title) return title;
  return '历史记录';
}

export function pipelineExecToRecord(exec: PipelineExecListRecord): GenerationRecord {
  const prompt = promptFromPipelineExecArgs(exec.argsJson);
  return {
    id: execRecordKey(exec),
    runId: exec.agentkExecId || exec.publicId,
    templateName: prompt,
    scene: '历史',
    prompt,
    intentPreview: '历史执行记录。点击可查看该次的入参与产物。',
    payloadPreview: prompt,
    status: statusFromPipelineExecStatus(exec.status),
    createdAt: exec.createdAt ? new Date(exec.createdAt).getTime() : Date.now(),
    error: exec.error,
  };
}

export function pipelineExecsToRecords(execs: PipelineExecListRecord[]): GenerationRecord[] {
  return execs
    .map(pipelineExecToRecord)
    .sort((left, right) => right.createdAt - left.createdAt);
}
