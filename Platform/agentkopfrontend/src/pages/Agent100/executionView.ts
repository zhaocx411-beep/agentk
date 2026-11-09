/**
 * Agent100 执行结果展示辅助
 */
import { isPlainObject } from './utils';
import type { Agent100Execution, Agent100HistoryItem, Agent100RunEvent } from './types';

const TERMINAL = new Set(['completed', 'failed', 'cancelled']);

export function isTerminalStatus(status: string | undefined): boolean {
  return Boolean(status && TERMINAL.has(status));
}

export function executionArgs(detail: Agent100Execution | null): Record<string, unknown> {
  if (!detail?.request || !isPlainObject(detail.request)) return {};
  const snapshot = detail.request as Record<string, unknown>;
  if (isPlainObject(snapshot.args)) return snapshot.args;
  const flat: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(snapshot)) {
    if (key === 'pipeline' || key === 'title' || key === 'args') continue;
    flat[key] = value;
  }
  return flat;
}

export function executionOutput(detail: Agent100Execution | null): Record<string, unknown> | null {
  if (!detail || !isPlainObject(detail.output)) return null;
  return detail.output;
}

export function mergeLiveHistoryItem(
  items: Agent100HistoryItem[],
  live: Agent100Execution | null | undefined,
  pipeline: string,
): Agent100HistoryItem[] {
  if (!live?.executionId || live.pipeline !== pipeline) return items;
  const index = items.findIndex((item) => item.executionId === live.executionId);
  const current = index >= 0 ? items[index] : undefined;
  const next: Agent100HistoryItem = {
    sessionId: live.sessionId,
    executionId: live.executionId,
    pipeline: live.pipeline,
    title: (typeof live.request?.title === 'string' ? live.request.title : undefined) || current?.title,
    status: live.status,
    error: live.error,
    hasOutput: Boolean(live.output) || current?.hasOutput,
    createdAt: current?.createdAt,
    updatedAt: current?.updatedAt,
  };
  if (index < 0) return [next, ...items];
  const copy = items.slice();
  copy[index] = { ...current, ...next };
  return copy;
}

function isInternalEvent(event: Agent100RunEvent): boolean {
  const message = event.message?.trim() ?? '';
  if (event.step?.startsWith('request:') || event.step?.startsWith('response:')) return true;
  return message.startsWith('agentk.') || message.startsWith('narrato.');
}

export function isUserProgressEvent(event: Agent100RunEvent): boolean {
  if (event.type !== 'progress' || isInternalEvent(event)) return false;
  const message = event.message?.trim() ?? '';
  const hasPercent = typeof event.data?.percent === 'number';
  return hasPercent || message.includes('正在');
}

export function userProgressEvents(events: Agent100RunEvent[] | undefined): Agent100RunEvent[] {
  return (events ?? []).filter(isUserProgressEvent);
}

export function latestProgress(events: Agent100RunEvent[] | undefined): {
  message: string;
  percent?: number;
} | null {
  const items = userProgressEvents(events);
  const event = items[items.length - 1];
  if (!event) return null;
  const percent = typeof event.data?.percent === 'number' ? event.data.percent : undefined;
  return { message: event.message?.trim() || '处理中', percent };
}

export interface PreviewMediaItem {
  url: string;
  mediaType: 'image' | 'video' | 'audio';
}

function mediaTypeOf(type: string | undefined): PreviewMediaItem['mediaType'] {
  const value = (type ?? '').toLowerCase();
  if (value.includes('video')) return 'video';
  if (value.includes('audio')) return 'audio';
  return 'image';
}

export function collectPreviewMedia(detail: Agent100Execution | null): PreviewMediaItem[] {
  const output = executionOutput(detail);
  if (!output) return [];
  const seen = new Set<string>();
  const items: PreviewMediaItem[] = [];

  const add = (url: unknown, type?: string) => {
    if (typeof url !== 'string' || !url.trim() || seen.has(url)) return;
    seen.add(url);
    items.push({ url, mediaType: mediaTypeOf(type) });
  };

  const artifacts = output.artifacts;
  if (Array.isArray(artifacts)) {
    for (const artifact of artifacts) {
      if (!isPlainObject(artifact)) continue;
      add(artifact.uri ?? artifact.url, typeof artifact.type === 'string' ? artifact.type : undefined);
    }
  }
  if (Array.isArray(output.videoUrls)) {
    for (const url of output.videoUrls) add(url, 'video');
  }
  if (Array.isArray(output.imageUrls)) {
    for (const url of output.imageUrls) add(url, 'image');
  }
  if (typeof output.videoUri === 'string') add(output.videoUri, 'video');
  if (typeof output.uri === 'string') add(output.uri, typeof output.type === 'string' ? output.type : 'video');
  return items.sort((a, b) => a.url.localeCompare(b.url));
}
