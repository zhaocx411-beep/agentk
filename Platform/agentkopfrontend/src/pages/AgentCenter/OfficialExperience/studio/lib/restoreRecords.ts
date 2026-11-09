/**
 * 从会话账本重建「我的生成」
 *
 * 生成记录只活在组件 state 里，刷新就没了，但会话账本是后端持久化的：
 * 每条用户消息对应一次生成，紧跟其后开跑的那一轮决定这条记录的状态。
 * 模版、参考图这些本地选择没有随会话保存，重建出来的记录只有提交的对话文本。
 */
import type { LedgerEvent, MessageItem } from '@/pages/AgentPlayground/types';
import type { GenerationRecord } from './types';

const RESTORED_SCENE = '历史记录';
const RESTORED_INTENT = '这一轮由会话账本重建，当时的模版、样本与参考图设置未随会话保存。';

interface RoundOutcome {
  seq: number;
  runId: string;
  status: GenerationRecord['status'];
  error?: string;
}

/** 账本里每一轮的终态；round.started 之后的第一个终态事件即为结果 */
function collectRoundOutcomes(ledger: LedgerEvent[]): RoundOutcome[] {
  const rounds: RoundOutcome[] = [];
  for (const event of [...ledger].sort((left, right) => left.seq - right.seq)) {
    // round.started 的 summary 就是 roundId，右栏按它过滤实时输出
    if (event.kind === 'round.started') {
      rounds.push({ seq: event.seq, runId: event.summary, status: 'running' });
      continue;
    }
    const current = rounds[rounds.length - 1];
    if (!current || current.status !== 'running') continue;
    switch (event.kind) {
      case 'round.completed':
        current.status = 'done';
        break;
      case 'round.cancelled':
      case 'interrupted':
        current.status = 'cancelled';
        break;
      case 'round.failed':
        current.status = 'failed';
        current.error = event.summary;
        break;
      default:
        break;
    }
  }
  return rounds;
}

function messageText(message: MessageItem): string {
  return message.parts
    .map((part) => (part.type === 'text' ? part.text : ''))
    .join('')
    .trim();
}

/** 最新的排在前面，与实时产生的记录顺序一致 */
export function restoreRecords(messages: MessageItem[], ledger: LedgerEvent[]): GenerationRecord[] {
  const rounds = collectRoundOutcomes(ledger);
  const ordered = [...messages].sort((left, right) => (left.id ?? 0) - (right.id ?? 0));
  const records: GenerationRecord[] = [];
  let cursor = 0;

  for (const message of ordered) {
    if (message.role !== 'user') continue;
    const text = messageText(message);
    if (!text) continue;

    const seq = message.id ?? 0;
    while (cursor < rounds.length && rounds[cursor].seq <= seq) cursor += 1;
    const round = cursor < rounds.length ? rounds[cursor] : undefined;
    if (round) cursor += 1;

    records.push({
      id: `restored-${seq}`,
      runId: round?.runId,
      templateName: RESTORED_SCENE,
      scene: RESTORED_SCENE,
      prompt: text,
      intentPreview: RESTORED_INTENT,
      payloadPreview: text,
      // 回放出来的未闭合 round 不会再收到终态事件，一律按已结束处理
      status: !round || round.status === 'running' ? 'done' : round.status,
      createdAt: message.createdAt ? new Date(message.createdAt).getTime() : Date.now(),
      error: round?.error,
    });
  }

  return records.reverse();
}
