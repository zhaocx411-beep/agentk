/**
 * OfflineRun - 数据解析工具
 * 单元格指标 JSON 解析与 trace 事件 → 对话视图的转换
 */
import { message } from '@/utils/feedback';
import { downloadMediaByUrl } from '@/services/media';
import { queryCellTrace } from './api';
import type { CellMetrics, JobCell, TraceDetail, TraceEvent, TraceItem } from './types';

/** 安全解析 metricsJson，非法时返回 null */
export function parseMetrics(json?: string): CellMetrics | null {
  if (!json) return null;
  try {
    return JSON.parse(json) as CellMetrics;
  } catch {
    return null;
  }
}

/**
 * 将 trace 会话事件转换为对话视图消息
 * user.input → 用户气泡；assistant.text → 助手气泡；
 * assistant.tool_call → 工具调用；tool.exec_succeeded / tool.exec_failed → 工具结果
 */
export function buildTraceItems(events: TraceEvent[]): TraceItem[] {
  const items: TraceItem[] = [];
  for (const ev of events || []) {
    const d = ev.data || {};
    if (ev.kind === 'user.input') {
      items.push({
        seq: ev.seq,
        role: 'user',
        createdAt: ev.createdAt,
        parts: [{ type: 'text', text: String(d.text ?? ''), mediaSource: d }],
      });
    } else if (ev.kind === 'assistant.text') {
      items.push({
        seq: ev.seq,
        role: 'assistant',
        createdAt: ev.createdAt,
        parts: [{ type: 'text', text: String(d.text ?? ''), mediaSource: d }],
      });
    } else if (ev.kind === 'assistant.tool_call') {
      items.push({
        seq: ev.seq,
        role: 'assistant',
        createdAt: ev.createdAt,
        parts: [
          {
            type: 'tool-call',
            toolName: String(d.toolName ?? ''),
            toolArgs: typeof d.args === 'string' ? d.args : JSON.stringify(d.args ?? {}, null, 2),
          },
        ],
      });
    } else if (ev.kind === 'tool.exec_succeeded') {
      items.push({
        seq: ev.seq,
        role: 'assistant',
        createdAt: ev.createdAt,
        parts: [
          {
            type: 'tool-result',
            toolOutput: JSON.stringify(d.artifacts ?? d, null, 2),
            mediaSource: d,
          },
        ],
      });
    } else if (ev.kind === 'tool.exec_failed') {
      items.push({
        seq: ev.seq,
        role: 'assistant',
        createdAt: ev.createdAt,
        parts: [{ type: 'tool-result', toolOutput: `执行失败: ${String(d.error ?? '')}` }],
      });
    }
  }
  return items;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function pushHttpUrl(out: string[], value: unknown): void {
  if (typeof value !== 'string') return;
  const url = value.trim();
  if (!/^https?:\/\//i.test(url) || out.includes(url)) return;
  out.push(url);
}

/** 从 cell 上收集产出 URL（兼容 outputUris / output_uris） */
export function collectCellOutputUris(cell: JobCell): string[] {
  const raw = cell as JobCell & Record<string, unknown>;
  const uris: string[] = [];
  for (const bucket of [raw.outputUris, raw.output_uris, raw.outputUrls, raw.output_urls]) {
    if (!Array.isArray(bucket)) continue;
    for (const item of bucket) pushHttpUrl(uris, item);
  }
  return uris;
}

/** 从 Trace 工具结果里收集产物 URL */
export function collectTraceArtifactUris(trace: TraceDetail | null | undefined): string[] {
  const uris: string[] = [];
  for (const ev of trace?.events ?? []) {
    const data = ev.data || {};
    const artifacts = data.artifacts ?? (isRecord(data.result) ? data.result.artifacts : undefined);
    if (!Array.isArray(artifacts)) continue;
    for (const item of artifacts) {
      if (!isRecord(item)) continue;
      pushHttpUrl(uris, item.uri);
      pushHttpUrl(uris, item.url);
    }
  }
  return uris;
}

export function normalizeJobCells(data: unknown): JobCell[] {
  const list = Array.isArray(data)
    ? data
    : isRecord(data) && Array.isArray(data.records)
      ? data.records
      : [];
  return list.flatMap((item) => {
    if (!isRecord(item) || typeof item.taskKey !== 'string') return [];
    const cell = item as unknown as JobCell;
    return [{ ...cell, outputUris: collectCellOutputUris(cell) }];
  });
}

/** 终态任务才允许导出产出（PENDING / RUNNING 通常尚无完整结果） */
export function canExportOfflineJob(status: string): boolean {
  return status === 'SUCCEEDED' || status === 'FAILED';
}

/** 有产出地址，或能去 Trace 里找产物时，允许下载 */
export function canDownloadCellOutputs(cell: JobCell): boolean {
  return collectCellOutputUris(cell).length > 0 || !!cell.agentkSessionId || !!cell.pipelineExecId;
}

function filenameFromUrl(url: string, index: number, taskKey: string): string {
  try {
    const path = new URL(url, window.location.origin).pathname;
    const base = decodeURIComponent(path.split('/').filter(Boolean).pop() || '');
    if (base && /\.[a-z0-9]{2,8}$/i.test(base)) return base;
  } catch {
    // 非法 URL 时用用例标识兜底
  }
  return `${taskKey}-${index + 1}`;
}

async function downloadOneUrl(url: string, filename: string): Promise<void> {
  await downloadMediaByUrl(url, filename || 'download');
}

/** 下载用例产出媒体：优先 cell.outputUris，没有则从 Trace artifacts 取 */
export async function downloadCellOutputs(cell: JobCell, bizId?: string): Promise<void> {
  let uris = collectCellOutputUris(cell);
  if (uris.length === 0 && bizId) {
    const trace = await queryCellTrace(bizId, cell.taskKey);
    uris = collectTraceArtifactUris(trace);
  }
  if (uris.length === 0) {
    message.warning('该用例暂无产出媒体');
    return;
  }
  for (let i = 0; i < uris.length; i += 1) {
    await downloadOneUrl(uris[i], filenameFromUrl(uris[i], i, cell.taskKey));
    if (i < uris.length - 1) {
      await new Promise((resolve) => {
        window.setTimeout(resolve, 200);
      });
    }
  }
  message.success(uris.length > 1 ? `已开始下载 ${uris.length} 个文件` : '已开始下载');
}
