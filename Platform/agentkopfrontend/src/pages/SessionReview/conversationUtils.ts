/**
 * 案例聚合 messages / 会话账本 events → 回看对话条目
 */
import {
  extractUserAttachments,
  extractUserInputText,
} from '@/pages/AgentPlayground/hooks/sse/helpers';
import type { SessionLedgerEvent } from '@/pages/AgentPlayground/types';
import type { PipelineExecRecord, SessionCaseMessage, SessionCaseRun } from './types';

export type ReviewMessageRole = 'user' | 'assistant' | 'tool' | 'system';

export interface ReviewMessageItem {
  key: string;
  role: ReviewMessageRole;
  /** 次要标签：runId / seq 等 */
  kind: string;
  title: string;
  content: string;
  /** 供 collectMediaUrls 抽取图/视频/音频 */
  mediaSource?: unknown;
  createdAt?: string;
  seq: number;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function parseContentJson(raw: string): { text: string; mediaSource: unknown } {
  const trimmed = raw?.trim() ?? '';
  if (!trimmed) return { text: '', mediaSource: null };

  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (typeof parsed === 'string') {
      return { text: parsed, mediaSource: parsed };
    }
    const record = asRecord(parsed);
    if (!record) {
      return { text: trimmed, mediaSource: parsed };
    }
    const textKeys = ['text', 'content', 'message', 'prompt'];
    for (const key of textKeys) {
      const value = record[key];
      if (typeof value === 'string' && value.trim()) {
        return { text: value.trim(), mediaSource: parsed };
      }
    }
    try {
      return { text: JSON.stringify(parsed, null, 2), mediaSource: parsed };
    } catch {
      return { text: trimmed, mediaSource: parsed };
    }
  } catch {
    return { text: trimmed, mediaSource: trimmed };
  }
}

function normalizeRole(role: string): ReviewMessageRole {
  const value = role.trim().toLowerCase();
  if (value === 'user') return 'user';
  if (value === 'assistant' || value === 'agent') return 'assistant';
  if (value === 'tool' || value === 'function') return 'tool';
  return 'system';
}

const ROLE_TITLE: Record<ReviewMessageRole, string> = {
  user: '用户',
  assistant: '助手',
  tool: '工具',
  system: '系统',
};

/** case.messages → 对话时间线（按 agentkItemSeq / id 排序） */
export function caseMessagesToReviewMessages(messages: SessionCaseMessage[]): ReviewMessageItem[] {
  const list = Array.isArray(messages) ? [...messages] : [];
  list.sort((a, b) => {
    const seqA = a.agentkItemSeq ?? a.id;
    const seqB = b.agentkItemSeq ?? b.id;
    return seqA - seqB;
  });

  return list.map((msg) => {
    const role = normalizeRole(msg.role || 'system');
    const { text, mediaSource } = parseContentJson(msg.contentJson || '');
    const seq = msg.agentkItemSeq ?? msg.id;
    return {
      key: `msg-${msg.id}`,
      role,
      kind: msg.runId != null ? `run #${msg.runId}` : `seq ${seq}`,
      title: ROLE_TITLE[role],
      content: text || (mediaSource != null ? '(含媒体产物)' : ''),
      mediaSource,
      createdAt: msg.createdAt,
      seq,
    };
  });
}

function readPayloadRecord(payload: unknown): Record<string, unknown> {
  return payload && typeof payload === 'object' && !Array.isArray(payload)
    ? (payload as Record<string, unknown>)
    : {};
}

function parseMaybeJson(value: unknown): unknown {
  if (typeof value !== 'string') return value;
  const trimmed = value.trim();
  if (!trimmed) return value;
  try {
    return JSON.parse(trimmed) as unknown;
  } catch {
    return value;
  }
}

function readToolName(record: Record<string, unknown>): string {
  const nested =
    readPayloadRecord(record.function).name ??
    readPayloadRecord(record.toolCall).name ??
    readPayloadRecord(record.tool_call).name;
  return String(
    record.toolName ??
      record.tool_name ??
      record.name ??
      record.tool ??
      nested ??
      'tool',
  ).trim();
}

function formatModelValue(value: unknown): string[] {
  if (typeof value === 'string' && value.trim()) return [value.trim()];
  if (Array.isArray(value)) return value.flatMap(formatModelValue);
  const record = readPayloadRecord(value);
  return Object.entries(record).flatMap(([key, item]) =>
    formatModelValue(item).map((model) => `${key}: ${model}`),
  );
}

function collectToolModels(record: Record<string, unknown>, args: unknown): string[] {
  const models = new Set<string>();
  const collect = (source: unknown) => {
    const sourceRecord = readPayloadRecord(parseMaybeJson(source));
    for (const [key, value] of Object.entries(sourceRecord)) {
      if (key === 'model' || key === 'models' || /Model$/.test(key)) {
        for (const model of formatModelValue(value)) {
          if (model) models.add(model);
        }
      }
    }
  };
  collect(record);
  collect(args);
  return [...models];
}

function formatToolCallContent(record: Record<string, unknown>, args: unknown): string {
  const parsedArgs = parseMaybeJson(args) ?? {};
  const models = collectToolModels(record, parsedArgs);
  const payloadText = JSON.stringify(record, null, 2);
  return [
    models.length > 0 ? `模型：${models.join(' · ')}` : '',
    payloadText ? `完整事件：\n${payloadText}` : '',
  ]
    .filter(Boolean)
    .join('\n\n');
}

/** 会话账本 events → 对话时间线（按 seq 升序，与 Agent 对话生成过程同源） */
export function sessionEventsToReviewMessages(events: SessionLedgerEvent[]): ReviewMessageItem[] {
  const list = Array.isArray(events) ? [...events] : [];
  list.sort((left, right) => left.seq - right.seq);

  const items: ReviewMessageItem[] = [];
  const toolNames = new Map<string, string>();
  for (const event of list) {
    const payload = event.data ?? event.payload ?? {};
    const record = readPayloadRecord(payload);

    switch (event.kind) {
      case 'user.input': {
        const text = extractUserInputText(payload);
        const attachments = extractUserAttachments(payload);
        if (!text && attachments.length === 0) break;
        items.push({
          key: `ev-${event.seq}`,
          role: 'user',
          kind: `seq ${event.seq}`,
          title: '用户',
          content: text,
          mediaSource: attachments.length > 0 ? { attachments, ...record } : undefined,
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      case 'assistant.reasoning': {
        const text = String(record.text ?? record.content ?? '').trim();
        if (!text) break;
        items.push({
          key: `ev-${event.seq}`,
          role: 'assistant',
          kind: '思考',
          title: '思考',
          content: text,
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      case 'assistant.text': {
        const text = String(record.text ?? record.content ?? '').trim();
        if (!text) break;
        items.push({
          key: `ev-${event.seq}`,
          role: 'assistant',
          kind: '回复',
          title: '助手',
          content: text,
          mediaSource: payload,
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      case 'assistant.tool_call': {
        const toolName = readToolName(record);
        const toolCallId = String(
          record.toolCallId ?? record.tool_call_id ?? record.id ?? '',
        ).trim();
        const args = record.args ?? record.arguments ?? record.input;
        if (toolCallId) toolNames.set(toolCallId, toolName);
        items.push({
          key: `ev-${event.seq}`,
          role: 'tool',
          kind: toolName,
          title: '工具调用',
          content: formatToolCallContent(record, args),
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      case 'tool.exec_succeeded': {
        const result = readPayloadRecord(record.result);
        const artifacts = record.artifacts ?? result.artifacts;
        const toolCallId = String(
          record.toolCallId ?? record.tool_call_id ?? record.id ?? '',
        ).trim();
        const toolName = readToolName(record);
        const resolvedToolName =
          toolName === 'tool' ? toolNames.get(toolCallId) ?? toolCallId : toolName;
        items.push({
          key: `ev-${event.seq}`,
          role: 'tool',
          kind: resolvedToolName || 'exec_ok',
          title: '工具完成',
          content: JSON.stringify(record, null, 2),
          mediaSource: { artifacts, ...record },
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      case 'tool.exec_failed': {
        const toolCallId = String(
          record.toolCallId ?? record.tool_call_id ?? record.id ?? '',
        ).trim();
        const toolName = readToolName(record);
        const resolvedToolName =
          toolName === 'tool' ? toolNames.get(toolCallId) ?? toolCallId : toolName;
        items.push({
          key: `ev-${event.seq}`,
          role: 'system',
          kind: resolvedToolName || 'exec_failed',
          title: '工具失败',
          content: JSON.stringify(record, null, 2),
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      case 'execution.finished': {
        const status = String(record.status ?? '').trim();
        const executionError = String(record.error ?? '').trim();
        const isCompleted = status.includes('COMPLETED');
        const isCancelled = status.includes('CANCELLED');
        const isFailed = !isCancelled && (status.includes('FAILED') || Boolean(executionError));
        items.push({
          key: `ev-${event.seq}`,
          role: 'system',
          kind: 'execution',
          title: isFailed ? '执行失败' : isCancelled ? '执行取消' : '执行结束',
          content: isCompleted
            ? '执行完成'
            : isFailed
              ? executionError || status
              : isCancelled
                ? '执行已取消'
                : status || '执行结束',
          createdAt: event.createdAt,
          seq: event.seq,
        });
        break;
      }
      default:
        break;
    }
  }
  return items;
}

/** 账本 events 摘要文案 */
export function summarizeSessionEvents(events: SessionLedgerEvent[], total?: number): string {
  const executionIds = new Set(
    events.map((event) => event.executionId).filter((value): value is string => !!value),
  );
  const parts: string[] = [];
  if (typeof total === 'number' && total > 0) {
    parts.push(`共 ${total} 条事件`);
  }
  if (executionIds.size > 0) {
    parts.push(`${executionIds.size} 次 execution`);
  }
  return parts.join(' · ') || '无事件';
}

/** 运行摘要文案 */
export function summarizeCaseRuns(runs: SessionCaseRun[]): string {
  if (!Array.isArray(runs) || runs.length === 0) return '无 Run 记录';
  const completed = runs.filter((r) => r.status === 'completed').length;
  const failed = runs.filter((r) => r.status === 'failed').length;
  const parts = [`${runs.length} 次运行`];
  if (completed) parts.push(`${completed} 完成`);
  if (failed) parts.push(`${failed} 失败`);
  return parts.join(' · ');
}

function formatPipelineArgs(args: Record<string, unknown> | undefined): string {
  if (!args || Object.keys(args).length === 0) return '(无输入参数)';

  const lines: string[] = [];
  const topic = args.topic;
  if (typeof topic === 'string' && topic.trim()) {
    lines.push(`主题：${topic.trim()}`);
  }
  if (args.shots != null && args.shots !== '') {
    lines.push(`分镜数：${String(args.shots)}`);
  }

  const modelParts = [
    typeof args.chatModel === 'string' && args.chatModel.trim()
      ? `对话 ${args.chatModel.trim()}`
      : '',
    typeof args.imageModel === 'string' && args.imageModel.trim()
      ? `图片 ${args.imageModel.trim()}`
      : '',
    typeof args.videoModel === 'string' && args.videoModel.trim()
      ? `视频 ${args.videoModel.trim()}`
      : '',
  ].filter(Boolean);
  if (modelParts.length > 0) {
    lines.push(`模型：${modelParts.join(' · ')}`);
  }

  const known = new Set(['topic', 'shots', 'chatModel', 'imageModel', 'videoModel']);
  for (const [key, value] of Object.entries(args)) {
    if (known.has(key)) continue;
    if (value == null || value === '') continue;
    lines.push(
      `${key}：${typeof value === 'object' ? JSON.stringify(value) : String(value)}`,
    );
  }

  return lines.join('\n') || JSON.stringify(args, null, 2);
}

function formatPipelineOutput(exec: PipelineExecRecord): string {
  const parts = [`Pipeline：${exec.pipelineName}`, `状态：${exec.status || '--'}`];
  if (exec.error?.trim()) parts.push(`错误：${exec.error.trim()}`);

  const artifacts = exec.outputJson?.artifacts;
  if (Array.isArray(artifacts)) {
    for (const artifact of artifacts) {
      if (artifact.description?.trim()) parts.push(artifact.description.trim());
    }
  }

  const clipCount = exec.outputJson?.clipCount;
  if (clipCount != null) {
    parts.push(`片段数：${String(clipCount)}`);
  }

  if (parts.length <= 2 && exec.outputJson && Object.keys(exec.outputJson).length > 0) {
    parts.push(JSON.stringify(exec.outputJson, null, 2));
  }

  return parts.join('\n');
}

function pipelineExecSortKey(exec: PipelineExecRecord): number {
  const ts = Date.parse(exec.startedAt || exec.createdAt || '');
  if (!Number.isNaN(ts)) return ts;
  return exec.id;
}

/** workflow pipeline-execs → 对话时间线（输入参数 + 产物） */
export function pipelineExecsToReviewMessages(execList: PipelineExecRecord[]): ReviewMessageItem[] {
  const list = Array.isArray(execList) ? [...execList] : [];
  list.sort((a, b) => pipelineExecSortKey(a) - pipelineExecSortKey(b));

  const messages: ReviewMessageItem[] = [];
  for (const exec of list) {
    const baseSeq = exec.id;
    messages.push({
      key: `pipeline-${exec.id}-input`,
      role: 'user',
      kind: exec.pipelineName || 'pipeline',
      title: '输入',
      content: formatPipelineArgs(exec.argsJson),
      createdAt: exec.startedAt || exec.createdAt,
      seq: baseSeq * 2,
    });
    messages.push({
      key: `pipeline-${exec.id}-output`,
      role: 'assistant',
      kind: exec.status || 'output',
      title: '产出',
      content: formatPipelineOutput(exec),
      mediaSource: exec.outputJson ?? null,
      createdAt: exec.finishedAt || exec.updatedAt || exec.startedAt,
      seq: baseSeq * 2 + 1,
    });
  }
  return messages;
}

/** Pipeline 执行摘要文案 */
export function summarizePipelineExecs(execList: PipelineExecRecord[]): string {
  if (!Array.isArray(execList) || execList.length === 0) return '无 Pipeline 执行';
  const completed = execList.filter((item) => item.status === 'completed').length;
  const failed = execList.filter((item) => item.status === 'failed').length;
  const parts = [`${execList.length} 次 Pipeline`];
  if (completed) parts.push(`${completed} 完成`);
  if (failed) parts.push(`${failed} 失败`);
  return parts.join(' · ');
}
