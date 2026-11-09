/**
 * 从完整账本重建 foldMangaCanvas 所需的最小 SSE 切片（toolCards / jobs / messages）
 * 对话区分页只回放最新一页，画布需单独拉全量再合并实时 SSE
 */
import { resolveEventPayload } from '@/pages/AgentPlayground/hooks/sse/helpers';
import type { JobEvent, MessageItem, MessagePart, SessionLedgerEvent, ToolCardState } from '@/pages/AgentPlayground/types';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';

export interface MangaFoldSlice {
  toolCards: ToolCardState[];
  jobs: JobEvent[];
  messages: MessageItem[];
}

export function buildMangaFoldSliceFromLedger(events: SessionLedgerEvent[]): MangaFoldSlice {
  const toolCards = new Map<string, ToolCardState>();
  const jobs = new Map<string, JobEvent>();
  const messages: MessageItem[] = [];
  let roundText = '';
  let roundReason = '';

  for (const event of [...events].sort((left, right) => left.seq - right.seq)) {
    const payload = resolveEventPayload(event);
    if (!payload || typeof payload !== 'object' || Array.isArray(payload)) continue;
    const body = payload as Record<string, unknown>;

    switch (event.kind) {
      case 'assistant.tool_call': {
        const toolCallId = String(body.toolCallId ?? body.tool_call_id ?? '');
        if (!toolCallId) break;
        toolCards.set(toolCallId, {
          toolCallId,
          toolName: String(body.toolName ?? body.tool_name ?? ''),
          status: 'running',
          input: body.args ?? body.input ?? body.arguments,
        });
        break;
      }
      case 'tool.exec_succeeded':
      case 'tool.exec_failed': {
        const toolCallId = String(body.toolCallId ?? body.tool_call_id ?? '');
        if (!toolCallId) break;
        const existing = toolCards.get(toolCallId);
        const execToolName = String(body.toolName ?? body.tool_name ?? '').trim();
        toolCards.set(toolCallId, {
          toolCallId,
          toolName: execToolName || existing?.toolName || '',
          status: event.kind === 'tool.exec_failed' ? 'err' : 'ok',
          input: existing?.input,
          output: body,
          duration:
            typeof body.durationMillis === 'number' ? body.durationMillis : existing?.duration,
        });
        break;
      }
      case 'job.submitted': {
        const jobId = String(body.jobId ?? '');
        if (!jobId) break;
        jobs.set(jobId, {
          jobId,
          toolCallId: String(body.toolCallId ?? ''),
          kind: String(body.kind ?? 'async'),
          remoteTaskId: typeof body.remoteTaskId === 'string' ? body.remoteTaskId : undefined,
          wakePolicy: body.wakePolicy as JobEvent['wakePolicy'],
          status: 'submitted',
        });
        break;
      }
      case 'job.completed': {
        const jobId = String(body.jobId ?? '');
        if (!jobId) break;
        const existing = jobs.get(jobId);
        jobs.set(jobId, {
          jobId,
          toolCallId: String(body.toolCallId ?? existing?.toolCallId ?? ''),
          kind: String(body.kind ?? existing?.kind ?? 'async'),
          remoteTaskId:
            typeof body.remoteTaskId === 'string' ? body.remoteTaskId : existing?.remoteTaskId,
          wakePolicy: (body.wakePolicy ?? existing?.wakePolicy) as JobEvent['wakePolicy'],
          wakeRunId: typeof body.wakeRunId === 'string' ? body.wakeRunId : existing?.wakeRunId,
          status: 'completed',
          result: body.result,
        });
        break;
      }
      case 'job.failed': {
        const jobId = String(body.jobId ?? '');
        if (!jobId) break;
        const existing = jobs.get(jobId);
        jobs.set(jobId, {
          jobId,
          toolCallId: String(body.toolCallId ?? existing?.toolCallId ?? ''),
          kind: String(body.kind ?? existing?.kind ?? 'async'),
          status: 'failed',
          error: String(body.error ?? 'failed'),
        });
        break;
      }
      case 'assistant.text': {
        roundText += String(body.text ?? '');
        break;
      }
      case 'assistant.reasoning': {
        roundReason += String(body.text ?? body.reasoning ?? '');
        break;
      }
      case 'round.completed': {
        if (roundText.trim() || roundReason.trim()) {
          const parts: MessagePart[] = [];
          if (roundReason.trim()) parts.push({ type: 'reasoning', text: roundReason });
          if (roundText.trim()) parts.push({ type: 'text', text: roundText });
          messages.push({
            role: 'assistant',
            parts,
            id: event.seq,
            createdAt: event.createdAt,
          });
        }
        roundText = '';
        roundReason = '';
        break;
      }
      case 'round.started':
        roundText = '';
        roundReason = '';
        break;
      default:
        break;
    }
  }

  return {
    toolCards: [...toolCards.values()],
    jobs: [...jobs.values()],
    messages,
  };
}

/** 账本切片打底，实时 SSE 覆盖同 id 的更新态 */
export function mergeMangaFoldState(ledger: MangaFoldSlice, live: SSEState): Pick<SSEState, 'toolCards' | 'jobs' | 'messages'> {
  const toolCardsMap = new Map<string, ToolCardState>();
  for (const card of ledger.toolCards) toolCardsMap.set(card.toolCallId, card);
  for (const card of live.toolCards) {
    const prev = toolCardsMap.get(card.toolCallId);
    if (!prev) {
      toolCardsMap.set(card.toolCallId, card);
      continue;
    }
    toolCardsMap.set(card.toolCallId, {
      ...prev,
      ...card,
      toolName: card.toolName || prev.toolName,
      input: card.input ?? prev.input,
      output: card.output ?? prev.output,
    });
  }

  const jobsMap = new Map<string, JobEvent>();
  for (const job of ledger.jobs) jobsMap.set(job.jobId, job);
  for (const job of live.jobs) {
    const prev = jobsMap.get(job.jobId);
    jobsMap.set(job.jobId, prev ? { ...prev, ...job } : job);
  }

  const ledgerMsgIds = new Set(ledger.messages.map((msg) => msg.id).filter((id) => id != null));
  const messages = [...ledger.messages];
  for (const msg of live.messages) {
    if (msg.id != null && ledgerMsgIds.has(msg.id)) continue;
    messages.push(msg);
  }

  return {
    toolCards: [...toolCardsMap.values()],
    jobs: [...jobsMap.values()],
    messages,
  };
}
