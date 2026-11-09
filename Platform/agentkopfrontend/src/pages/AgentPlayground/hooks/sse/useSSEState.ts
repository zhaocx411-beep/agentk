/**
 * SSE 状态管理 Hook
 * 封装所有 state 的读写操作，供 useSSE 和 useStream 共享
 */
import { useRef, useCallback, useState } from 'react';
import type {
  MessageItem,
  MessagePart,
  ToolCardState,
  JobEvent,
  ItemCompletedPayload,
  RunStartedPayload,
  StepCompletedPayload,
  ToolExecutedPayload,
  RunCompletedPayload,
  RunFailedPayload,
  JobSubmittedPayload,
  JobCompletedPayload,
  JobFailedPayload,
  WakePolicy,
  RunEvent,
  DeltaPayload,
  AskUserAnswer,
  AskUserCardStatus,
  AskUserQuestion,
  AskUserRequestedPayload,
  SessionLedgerEvent,
} from '../../types';
import type { SSEState } from './types';
import { INITIAL_SSE_STATE } from './types';
import { abortAfterExecutionGrace, remainingExecutionGrace } from '@/utils/executionGrace';
import {
  extractUserAttachments,
  extractUserInputText,
  normalizeUserMessageParts,
  mergeUserMessageParts,
  shouldMergeUserMessages,
} from './helpers';
import { sessionLedgerEventKey } from '../../utils/sessionLedgerOrder';

/** 不阻塞输入的工具：后台进度轮询，用户应可继续提问 */
const NON_BLOCKING_TOOLS = new Set(['job_status']);

function askUserAnswersFromPayload(payload: unknown): AskUserAnswer[] {
  const record =
    payload && typeof payload === 'object' && !Array.isArray(payload)
      ? (payload as Record<string, unknown>)
      : {};
  const rawAnswers = Array.isArray(record.answers) ? record.answers : [];
  return rawAnswers.flatMap((answer) => {
    if (!answer || typeof answer !== 'object' || Array.isArray(answer)) return [];
    const item = answer as Record<string, unknown>;
    const questionId = String(item.questionId ?? item.question_id ?? '');
    const selected = Array.isArray(item.selected)
      ? item.selected.filter((value): value is string => typeof value === 'string')
      : [];
    return questionId ? [{ questionId, selected }] : [];
  });
}

function formatExecutionFailure(error: string, status: string): string {
  const maxRounds = error.match(/exceeded max tool rounds\s+(\d+)/i)?.[1];
  if (maxRounds) {
    return `工具调用轮数已达到上限（${maxRounds} 轮），执行已停止`;
  }
  return error || status || '未知错误';
}

export function useSSEState() {
  const lastSeqRef = useRef(0);
  /** 本会话已知的全部账本事件；stream id 与平台 seq 不同域，优先用 eventId 复合键保存 */
  const ledgerStoreRef = useRef(new Map<string, SessionLedgerEvent>());
  /** /events 回放与 stream 续传共用：同一 agentk eventId 只处理一次 */
  const seenEventIdsRef = useRef(new Set<string>());
  const streamTextRef = useRef('');
  const streamReasonRef = useRef('');
  const abortRef = useRef<AbortController | null>(null);
  const streamRetryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  /** 切会话后让旧连接静默存活到 worker 绑定窗口结束。 */
  const streamGenerationRef = useRef(0);
  const bindGraceUntilRef = useRef(0);
  /** 记录最后一个 delta 事件的后端 ts（毫秒），用于给 assistant 消息打准确时间戳 */
  const lastTsRef = useRef<number>(0);
  /** session stream 的 round 状态。账本事件有序、delta 尽力投递，二者必须按 round 隔离。 */
  const currentRoundIdRef = useRef<string | null>(null);
  const runningRoundIdsRef = useRef(new Set<string>());
  const ignoredRoundIdsRef = useRef(new Set<string>());
  const roundBlocksRef = useRef(new Map<string, Map<string, string>>());
  /** 已 round.completed 的 round，迟到的 delta/assistant.* 不再回灌流式区 */
  const completedRoundIdsRef = useRef(new Set<string>());
  /** 已开始、尚未成功/失败的工具调用；工具执行期间也应视为忙碌（禁用输入、显示停止） */
  const pendingToolCallIdsRef = useRef(new Set<string>());
  /** execution.started 至 execution.finished 之间为 true，覆盖 round/工具之间的空窗期 */
  const executionInProgressRef = useRef(false);
  /** cancel 请求发出到 interrupted 到达之间，忽略所有在途 delta。 */
  const cancelPendingRef = useRef(false);

  // ===== 后台任务跟随 =====
  // job 与唤醒 round 均由 session stream 推送，前端只维护展示和去重状态。
  /** 已提交未终态的 jobId */
  const pendingJobsRef = useRef(new Set<string>());
  /** 待跟随的唤醒 run id（按发现顺序接力） */
  const wakeQueueRef = useRef<string[]>([]);
  /** 待轮询账本：runId -> 已消费的事件 seq 游标 */
  const watchRunsRef = useRef(new Map<string, number>());
  /** 已处理的 job 事件（kind:jobId）——推送/对账/轮询三条路径共用去重 */
  const seenJobEventsRef = useRef(new Set<string>());
  /** job 跟随轮询定时器（useStream 使用，resetFollowState 统一清理） */
  const followTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  /** run 终态回调（由 useSSE.settleRun 注入），run.completed/run.failed 后触发，参数为终态 run 的 id */
  const onRunSettledRef = useRef<((runId?: string) => void) | null>(null);

  const [state, setState] = useState<SSEState>({ ...INITIAL_SSE_STATE });

  /** 追加流式文本（同步更新 ref，异步更新 state 触发渲染） */
  const appendStreamingText = useCallback((text: string, ts?: number) => {
    streamTextRef.current += text;
    if (ts && ts > 0) lastTsRef.current = ts;
    setState((prev) => ({ ...prev, streamingText: streamTextRef.current }));
  }, []);

  /** 追加流式 reasoning 文本 */
  const appendStreamingReason = useCallback((text: string, ts?: number) => {
    streamReasonRef.current += text;
    if (ts && ts > 0) lastTsRef.current = ts;
    setState((prev) => ({ ...prev, streamingReason: streamReasonRef.current }));
  }, []);

  const readRoundOutput = useCallback((roundId: string, kind: 'text' | 'reasoning') => {
    const blocks = roundBlocksRef.current.get(roundId);
    if (!blocks) return '';
    return [...blocks.entries()]
      .filter(([key]) => key.startsWith(`${kind}:`))
      .sort(([left], [right]) => Number(left.split(':')[1]) - Number(right.split(':')[1]))
      .map(([, text]) => text)
      .join('');
  }, []);

  const syncStreamingRound = useCallback(
    (roundId: string) => {
      if (currentRoundIdRef.current !== roundId) return;
      const text = readRoundOutput(roundId, 'text');
      const reasoning = readRoundOutput(roundId, 'reasoning');
      streamTextRef.current = text;
      streamReasonRef.current = reasoning;
      setState((prev) => ({ ...prev, streamingText: text, streamingReason: reasoning }));
    },
    [readRoundOutput],
  );

  /** 写入一个 round block；账本事件覆盖，delta 事件追加。 */
  const setRoundBlock = useCallback(
    (roundId: string, block: number, kind: 'text' | 'reasoning', text: string, append: boolean) => {
      if (cancelPendingRef.current || ignoredRoundIdsRef.current.has(roundId)) return;
      const blocks = roundBlocksRef.current.get(roundId) || new Map<string, string>();
      const key = `${kind}:${block}`;
      blocks.set(key, append ? `${blocks.get(key) || ''}${text}` : text);
      roundBlocksRef.current.set(roundId, blocks);
      syncStreamingRound(roundId);
    },
    [syncStreamingRound],
  );

  /** 工具执行后的后续模型轮次可能直接从 delta 开始，没有单独的 round.started。 */
  const activateRound = useCallback((roundId: string) => {
    if (cancelPendingRef.current || ignoredRoundIdsRef.current.has(roundId)) return false;
    currentRoundIdRef.current = roundId;
    runningRoundIdsRef.current.add(roundId);
    if (!roundBlocksRef.current.has(roundId)) {
      roundBlocksRef.current.set(roundId, new Map());
    }
    setState((prev) => ({
      ...prev,
      currentRunId: roundId,
      isRunning: true,
      isCancelling: false,
      error: null,
    }));
    return true;
  }, []);

  const applySessionDelta = useCallback(
    (delta: DeltaPayload) => {
      if (completedRoundIdsRef.current.has(delta.roundId)) return;
      if (!runningRoundIdsRef.current.has(delta.roundId) && !activateRound(delta.roundId)) {
        return;
      }
      setRoundBlock(delta.roundId, delta.block, delta.kind, delta.text || '', true);
    },
    [activateRound, setRoundBlock],
  );

  /** round / 工具 / 整次 execution 任一未结束，即视为运行中 */
  const computeIsRunning = useCallback(
    () =>
      executionInProgressRef.current ||
      runningRoundIdsRef.current.size > 0 ||
      pendingToolCallIdsRef.current.size > 0,
    [],
  );

  const beginExecutionFollow = useCallback(() => {
    executionInProgressRef.current = true;
    setState((prev) => ({ ...prev, isRunning: true, error: null }));
  }, []);

  /** 中断时丢弃当前 round 的半成品，并屏蔽随后到达的在途事件。 */
  const discardActiveRounds = useCallback(() => {
    const roundIds = new Set(runningRoundIdsRef.current);
    if (currentRoundIdRef.current) roundIds.add(currentRoundIdRef.current);
    for (const roundId of roundIds) {
      ignoredRoundIdsRef.current.add(roundId);
      roundBlocksRef.current.delete(roundId);
    }
    runningRoundIdsRef.current.clear();
    pendingToolCallIdsRef.current.clear();
    executionInProgressRef.current = false;
    currentRoundIdRef.current = null;
    streamTextRef.current = '';
    streamReasonRef.current = '';
    setState((prev) => ({
      ...prev,
      streamingText: '',
      streamingReason: '',
      isRunning: false,
      isCancelling: false,
      toolCards: prev.toolCards.map((tool) =>
        tool.status === 'running' ? { ...tool, status: 'err' as const } : tool,
      ),
    }));
  }, []);

  const resetSessionStreamState = useCallback(() => {
    currentRoundIdRef.current = null;
    runningRoundIdsRef.current.clear();
    pendingToolCallIdsRef.current.clear();
    executionInProgressRef.current = false;
    ignoredRoundIdsRef.current.clear();
    completedRoundIdsRef.current.clear();
    roundBlocksRef.current.clear();
    cancelPendingRef.current = false;
    streamTextRef.current = '';
    streamReasonRef.current = '';
    seenEventIdsRef.current.clear();
  }, []);

  const clearLedgerStore = useCallback(() => {
    ledgerStoreRef.current.clear();
    seenEventIdsRef.current.clear();
  }, []);

  const mergeLedgerEvents = useCallback((events: SessionLedgerEvent[]) => {
    for (const event of events) {
      if (event.seq > 0) {
        ledgerStoreRef.current.set(sessionLedgerEventKey(event), event);
      }
    }
  }, []);

  /**
   * 历史账本回放结束后强制放开输入：
   * 静态回放不订 stream，残留的 running 工具/未闭合 round 永远不会收到终态事件，
   * 若继续锁输入会误显示「停止生成」。真正生成中的忙碌态由下次 send 订流后再建立。
   */
  const finalizeReplayBusyState = useCallback(() => {
    runningRoundIdsRef.current.clear();
    pendingToolCallIdsRef.current.clear();
    executionInProgressRef.current = false;
    currentRoundIdRef.current = null;
    streamTextRef.current = '';
    streamReasonRef.current = '';
    setState((prev) => ({
      ...prev,
      streamingText: '',
      streamingReason: '',
      isRunning: false,
      isCancelling: false,
      // ChatArea 还会按 toolCards.status===running 判定 isBusy，必须一并终态化
      toolCards: prev.toolCards.map((t) =>
        t.status === 'running' ? { ...t, status: 'ok' as const } : t,
      ),
    }));
  }, []);

  /**
   * 续流回放：保留 pending 工具 / 未闭合 round，仅同步 isRunning。
   * 切换回进行中的会话时，账本可能停在 tool.exec_started，后续只有 keepalive。
   */
  const syncReplayBusyStateForResume = useCallback(() => {
    executionInProgressRef.current = true;
    streamTextRef.current = '';
    streamReasonRef.current = '';
    setState((prev) => ({
      ...prev,
      streamingText: '',
      streamingReason: '',
      isRunning: true,
      isCancelling: false,
    }));
  }, []);

  /**
   * 强制终态化某个工具调用。
   * ask_user 问卷失效等场景不会再收到 tool.exec_*，残留的 pending 会一直锁住输入。
   */
  const settleToolCall = useCallback(
    (toolCallId: string, status: 'ok' | 'err' = 'ok') => {
      if (!toolCallId) return;
      pendingToolCallIdsRef.current.delete(toolCallId);
      setState((prev) => ({
        ...prev,
        isRunning: computeIsRunning(),
        toolCards: prev.toolCards.map((tool) =>
          tool.toolCallId === toolCallId && tool.status === 'running' ? { ...tool, status } : tool,
        ),
      }));
    },
    [computeIsRunning],
  );

  /** 将问卷改为只读终态，但保留在原对话位置。 */
  const settleAskUser = useCallback(
    (
      toolCallId: string,
      status: Exclude<AskUserCardStatus, 'pending'>,
      answers: AskUserAnswer[] = [],
    ) => {
      if (!toolCallId) return;
      pendingToolCallIdsRef.current.delete(toolCallId);
      setState((prev) => ({
        ...prev,
        isRunning: computeIsRunning(),
        pendingAskUser: prev.pendingAskUser?.toolCallId === toolCallId ? null : prev.pendingAskUser,
        messages: prev.messages.map((message) => ({
          ...message,
          parts: message.parts.map((part) =>
            part.type === 'ask-user' && part.toolCallId === toolCallId
              ? {
                  ...part,
                  status,
                  ...(answers.length > 0 ? { answers } : {}),
                }
              : part,
          ),
        })),
        toolCards: prev.toolCards.map((tool) =>
          tool.toolCallId === toolCallId && tool.status === 'running'
            ? { ...tool, status: 'ok' as const }
            : tool,
        ),
      }));
    },
    [computeIsRunning],
  );

  // ========== 底层 helpers ==========

  /** 用户停止/重发立即断开；普通切走在绑定窗口内静默保活。 */
  const closeStream = useCallback((options?: { force?: boolean }) => {
    streamGenerationRef.current += 1;
    if (streamRetryTimerRef.current) {
      clearTimeout(streamRetryTimerRef.current);
      streamRetryTimerRef.current = null;
    }
    const controller = abortRef.current;
    abortRef.current = null;
    if (!controller) return;

    const remaining = remainingExecutionGrace(bindGraceUntilRef.current);
    bindGraceUntilRef.current = 0;
    if (!options?.force && remaining > 0) {
      abortAfterExecutionGrace(controller, remaining);
      return;
    }
    controller.abort();
  }, []);

  /** 新增一条消息；连续重复的用户同文气泡会合并（乐观发送 + 账本回放去重）
   *  noMerge：回放更早历史时用，这批消息比末尾那条更早，不能与之合并 */
  const addMessage = useCallback(
    (item: MessageItem, runId?: string | null, opts?: { noMerge?: boolean }) => {
      setState((prev) => {
        const msg = {
          ...item,
          runId: runId || item.runId,
          createdAt: item.createdAt || new Date().toISOString(),
          // 用户消息没有后端 id，用当前时间戳的毫秒部分作为自增 id
          id: item.id || Date.now(),
        };

        if (msg.role === 'user' && !opts?.noMerge) {
          let matchingIndex = -1;
          for (let index = prev.messages.length - 1; index >= 0; index -= 1) {
            const messageItem = prev.messages[index];
            const sameExecution =
              !messageItem.executionId ||
              !msg.executionId ||
              messageItem.executionId === msg.executionId;
            if (
              messageItem.role === 'user' &&
              sameExecution &&
              shouldMergeUserMessages(messageItem, msg)
            ) {
              matchingIndex = index;
              break;
            }
          }
          const matching = prev.messages[matchingIndex];
          if (matching) {
            const next = [...prev.messages];
            next[matchingIndex] = {
              ...matching,
              parts: mergeUserMessageParts(matching, msg),
              id: item.id || matching.id,
              createdAt: item.createdAt || matching.createdAt,
              executionId: item.executionId || matching.executionId,
              runId: runId || item.runId || matching.runId,
            };
            return { ...prev, messages: next };
          }
        }

        if (msg.executionId && typeof msg.id === 'number') {
          const insertionIndex = prev.messages.findIndex(
            (existing) =>
              existing.executionId === msg.executionId &&
              typeof existing.id === 'number' &&
              existing.id > msg.id,
          );
          if (insertionIndex >= 0) {
            const next = [...prev.messages];
            next.splice(insertionIndex, 0, msg);
            return { ...prev, messages: next };
          }
        }

        return { ...prev, messages: [...prev.messages, msg] };
      });
    },
    [],
  );

  /** 更新或创建 toolCard
   *  防止状态降级：已处于终态 (ok/err) 的卡片，不允许被回退为 running
   */
  const updateToolCard = useCallback((update: Partial<ToolCardState> & { toolCallId: string }) => {
    setState((prev) => {
      const exists = prev.toolCards.find((t) => t.toolCallId === update.toolCallId);
      if (exists) {
        // 已终态(ok/err)时，忽略 status: 'running' 的降级覆盖
        const effectiveUpdate =
          exists.status !== 'running' && update.status === 'running'
            ? { ...update, status: exists.status }
            : update;
        return {
          ...prev,
          toolCards: prev.toolCards.map((t) =>
            t.toolCallId === update.toolCallId
              ? {
                  ...t,
                  ...effectiveUpdate,
                  toolName: effectiveUpdate.toolName || t.toolName,
                }
              : t,
          ),
        };
      }
      const created: ToolCardState = {
        toolCallId: update.toolCallId,
        toolName: update.toolName || update.toolCallId,
        status: update.status || 'running',
        duration: update.duration,
        input: update.input,
        output: update.output,
      };
      return { ...prev, toolCards: [...prev.toolCards, created] };
    });
  }, []);

  /** 新增一条 ledger 事件 */
  const addLedger = useCallback((seq: number, kind: string, summary: string) => {
    setState((prev) => ({
      ...prev,
      ledger: [...prev.ledger, { seq, kind, summary }],
    }));
  }, []);

  /** 更新或创建 Job */
  const updateJob = useCallback((job: JobEvent) => {
    setState((prev) => {
      const exists = prev.jobs.find((j) => j.jobId === job.jobId);
      if (exists) {
        return {
          ...prev,
          jobs: prev.jobs.map((j) => (j.jobId === job.jobId ? { ...j, ...job } : j)),
        };
      }
      return { ...prev, jobs: [...prev.jobs, job] };
    });
  }, []);

  /** 往消息流追加一条 job 卡片占位消息（data-job part，动态数据渲染时从 state.jobs 查）
   *  对齐 demoui addJobCard：卡片追加在事件到达时的位置，不重复追加 */
  const appendJobCardMessage = useCallback((jobId: string, seq = 0) => {
    setState((prev) => {
      const exists = prev.messages.some((m) =>
        m.parts.some((p) => p.type === 'data-job' && p.jobId === jobId),
      );
      if (exists) return prev;
      return {
        ...prev,
        messages: [
          ...prev.messages,
          {
            role: 'assistant',
            parts: [{ type: 'data-job', jobId }],
            createdAt: new Date().toISOString(),
            id: seq,
          },
        ],
      };
    });
  }, []);

  /** 流式现场收尾（对齐 demoui finalizeAssistantBubble）：
   *  run 终态时若还有未落地的流式累积（item 没到），把它落成一条 assistant 消息 */
  const flushStreaming = useCallback((seq = 0) => {
    if (!streamTextRef.current && !streamReasonRef.current) return;
    const parts: MessagePart[] = [];
    if (streamReasonRef.current) {
      parts.push({ type: 'reasoning', text: streamReasonRef.current });
    }
    if (streamTextRef.current) {
      parts.push({ type: 'text', text: streamTextRef.current });
    }
    streamTextRef.current = '';
    streamReasonRef.current = '';
    setState((prev) => ({
      ...prev,
      messages: [
        ...prev.messages,
        {
          role: 'assistant',
          parts,
          runId: prev.currentRunId || undefined,
          createdAt: new Date().toISOString(),
          id: seq,
        },
      ],
      streamingText: '',
      streamingReason: '',
    }));
  }, []);

  /**
   * 确认不再跟随后的收尾：把残留流式内容落成气泡并放开输入。
   * 用于 catchUp 后仍无 round/job，或 204 空转耗尽——不是收到 `stream.end` 就立刻调用
   *（`stream.end` 只表示本条 SSE 连接关闭，需无感重连续流）。
   */
  const finalizeStreamEnd = useCallback(
    (seq = 0) => {
      flushStreaming(seq);
      runningRoundIdsRef.current.clear();
      currentRoundIdRef.current = null;
      setState((prev) => ({
        ...prev,
        isRunning: computeIsRunning(),
        isCancelling: false,
      }));
    },
    [computeIsRunning, flushStreaming],
  );

  /** 中断只停止后续生成：把用户已经看到的内容冻结成消息，再隔离该 round 的后续事件。 */
  const preserveInterruptedOutput = useCallback(
    (seq = 0) => {
      flushStreaming(seq);
      discardActiveRounds();
    },
    [discardActiveRounds, flushStreaming],
  );

  /** job 账本事件的统一入口：SSE 推送、终态对账、后续轮询都汇到这里，按 kind:jobId 去重。
   *  replay=true（历史回放）时只更新 jobs 状态，不动跟随引用（避免看历史时被唤醒 run 劫持） */
  const handleJobEvent = useCallback(
    (
      kind: string,
      payload: unknown,
      runId?: string,
      seq = 0,
      replay = false,
      showMessage = !replay,
    ) => {
      const p = (payload || {}) as JobSubmittedPayload & JobCompletedPayload & JobFailedPayload;
      const jobId = p.jobId || '';
      if (!jobId) return;
      const key = `${kind}:${jobId}`;
      if (seenJobEventsRef.current.has(key)) return;
      seenJobEventsRef.current.add(key);

      if (kind === 'job.submitted') {
        if (!replay) {
          pendingJobsRef.current.add(jobId);
          if (runId) {
            const existing = watchRunsRef.current.get(runId) || 0;
            watchRunsRef.current.set(runId, Math.max(existing, lastSeqRef.current));
          }
        }
        // session 历史回放也需要恢复消息流中的任务卡片；run 详情回放则只更新右栏。
        if (showMessage) appendJobCardMessage(jobId, seq);
        updateJob({
          jobId,
          toolCallId: p.toolCallId,
          kind: p.kind || 'async',
          remoteTaskId: p.remoteTaskId,
          wakePolicy: p.wakePolicy as WakePolicy | undefined,
          status: 'submitted',
        });
        // 后台任务已提交：同步工具阶段结束，释放对该 toolCall 的输入锁定，允许继续提问
        if (p.toolCallId) {
          pendingToolCallIdsRef.current.delete(p.toolCallId);
          updateToolCard({
            toolCallId: p.toolCallId,
            status: 'ok',
          });
        }
        setState((prev) => ({ ...prev, isRunning: computeIsRunning() }));
        addLedger(
          seq,
          'job.submitted',
          `${jobId} · ${p.kind || ''}${p.wakePolicy ? ' · ' + p.wakePolicy : ''}`,
        );
        return;
      }

      pendingJobsRef.current.delete(jobId);
      // 终态也要释放同步工具锁：job.submitted 可能因去重或事件缺失没走到释放逻辑
      if (p.toolCallId && pendingToolCallIdsRef.current.has(p.toolCallId)) {
        pendingToolCallIdsRef.current.delete(p.toolCallId);
        updateToolCard({ toolCallId: p.toolCallId, status: 'ok' });
        setState((prev) => ({ ...prev, isRunning: computeIsRunning() }));
      }
      // 终态事件先于 submitted 到达（对账补齐等）时，保证卡片存在
      if (showMessage) appendJobCardMessage(jobId, seq);
      if (kind === 'job.completed') {
        updateJob({
          jobId,
          toolCallId: p.toolCallId,
          kind: p.kind || 'async',
          wakePolicy: p.wakePolicy as WakePolicy | undefined,
          status: 'completed',
          result: p.result,
          wakeRunId: p.wakeRunId,
        });
        if (!replay && p.wakeRunId && !wakeQueueRef.current.includes(p.wakeRunId)) {
          wakeQueueRef.current.push(p.wakeRunId);
        }
        addLedger(seq, 'job.completed', `${jobId}${p.wakeRunId ? ' → wake ' + p.wakeRunId : ''}`);
      } else if (kind === 'job.failed') {
        updateJob({
          jobId,
          toolCallId: p.toolCallId,
          kind: p.kind || 'async',
          wakePolicy: p.wakePolicy as WakePolicy | undefined,
          status: 'failed',
          error: p.error,
        });
        addLedger(seq, 'job.failed', `${p.error || jobId}`);
      }
    },
    [updateJob, addLedger, appendJobCardMessage, updateToolCard, computeIsRunning],
  );

  /** 清空 job 跟随状态（切换会话 / 重置时调用） */
  const resetFollowState = useCallback(() => {
    pendingJobsRef.current.clear();
    wakeQueueRef.current = [];
    watchRunsRef.current.clear();
    seenJobEventsRef.current.clear();
    if (followTimerRef.current) {
      clearTimeout(followTimerRef.current);
      followTimerRef.current = null;
    }
  }, []);

  // ========== applyRunEvent：事件路由与分发 ==========

  const applyRunEvent = useCallback(
    (
      ev: RunEvent,
      options?: {
        skipMessages?: boolean;
        runId?: string;
        replay?: boolean;
        skipDedupe?: boolean;
        /** 回放更早历史：新气泡不与当前末尾消息合并 */
        olderReplay?: boolean;
        /** 全量重放账本时跳过重复写入 ledgerStore */
        skipLedgerStore?: boolean;
      },
    ) => {
      const eventId = ev.eventId?.trim();
      if (eventId) {
        if (seenEventIdsRef.current.has(eventId)) return;
        seenEventIdsRef.current.add(eventId);
      }

      const seq = ev.seq;
      if (!options?.skipDedupe && seq > 0 && seq <= lastSeqRef.current) return;
      if (!options?.skipDedupe && seq > 0) lastSeqRef.current = seq;

      const eventName = ev.kind;
      const payload = ev.payload;

      if (seq > 0 && !options?.skipLedgerStore) {
        const storedEvent: SessionLedgerEvent = {
          seq,
          executionId: ev.executionId,
          eventId: eventId || undefined,
          kind: eventName,
          roundId: options?.runId,
          data: payload,
          payload,
          createdAt: ev.createdAt || new Date().toISOString(),
        };
        ledgerStoreRef.current.set(sessionLedgerEventKey(storedEvent), storedEvent);
      }

      switch (eventName) {
        case 'user.input': {
          if (!options?.skipMessages) {
            const text = extractUserInputText(payload);
            const parts = normalizeUserMessageParts(text, extractUserAttachments(payload));
            if (parts.length > 0) {
              addMessage(
                {
                  role: 'user',
                  parts,
                  createdAt: ev.createdAt,
                  id: seq,
                  executionId: ev.executionId,
                },
                options?.runId,
                { noMerge: options?.olderReplay },
              );
            }
          }
          // 同一 execution 从起点续流时会重放 ask 之前的 user.input，不能撤下当前问卷；
          // 普通消息接管会创建新 execution，此时 executionId 不同，应清理旧问卷。
          if (!options?.olderReplay) {
            setState((prev) => {
              const pending = prev.pendingAskUser;
              if (!pending) return prev;
              if (pending.executionId && pending.executionId === ev.executionId) return prev;
              return {
                ...prev,
                pendingAskUser: null,
                messages: prev.messages.map((message) => ({
                  ...message,
                  parts: message.parts.map((part) =>
                    part.type === 'ask-user' && part.toolCallId === pending.toolCallId
                      ? { ...part, status: 'dismissed' as const }
                      : part,
                  ),
                })),
              };
            });
          }
          // 后端已接受新输入，说明上一轮遗留的同步工具阶段已作废；
          // 其终态事件可能永远不来（如打断后被取消的 compose），必须释放锁，
          // 否则输入框/顶栏/画布会在本轮结束后仍永久显示「生成中」
          if (!options?.replay && pendingToolCallIdsRef.current.size > 0) {
            const staleToolIds = new Set(pendingToolCallIdsRef.current);
            pendingToolCallIdsRef.current.clear();
            setState((prev) => ({
              ...prev,
              isRunning: computeIsRunning(),
              toolCards: prev.toolCards.map((tool) =>
                tool.toolCallId && staleToolIds.has(tool.toolCallId) && tool.status === 'running'
                  ? { ...tool, status: 'ok' as const }
                  : tool,
              ),
            }));
          }
          addLedger(seq, 'user.input', '用户消息');
          break;
        }

        case 'ask_user.requested': {
          const p = payload as AskUserRequestedPayload;
          const toolCallId = String(p.toolCallId ?? '');
          const questions = (Array.isArray(p.questions) ? p.questions : []).filter(
            (q): q is AskUserQuestion =>
              !!q && typeof q.id === 'string' && Array.isArray(q.options) && q.options.length > 0,
          );
          if (!options?.skipMessages && toolCallId && questions.length > 0) {
            addMessage(
              {
                role: 'assistant',
                parts: [
                  {
                    type: 'ask-user',
                    toolCallId,
                    questions,
                    status: 'pending',
                  },
                ],
                createdAt: ev.createdAt,
                id: seq,
                executionId: ev.executionId,
              },
              options?.runId,
              { noMerge: true },
            );
          }
          // skipMessages（回看历史 run）只是右栏视察，不弹出待答问卷；
          // 会话切换回放（skipMessages=false）会重放此事件，问卷未答时恢复展示。
          // olderReplay（上翻加载更早历史）里的问卷必然已有下文，重新弹出只会得到
          // 「no longer awaiting an answer」，当前待答状态以最新一页为准
          if (
            !options?.skipMessages &&
            !options?.olderReplay &&
            toolCallId &&
            questions.length > 0
          ) {
            setState((prev) => ({
              ...prev,
              pendingAskUser: {
                toolCallId,
                questions,
                executionId: ev.executionId,
              },
            }));
          }
          addLedger(seq, 'ask_user.requested', `等待用户回答（${questions.length} 题）`);
          break;
        }

        case 'ask_user.responded': {
          const p =
            payload && typeof payload === 'object' && !Array.isArray(payload)
              ? (payload as Record<string, unknown>)
              : {};
          const explicitToolCallId = String(p.toolCallId ?? p.tool_call_id ?? '');
          const answers = askUserAnswersFromPayload(payload);
          setState((prev) => {
            const toolCallId = explicitToolCallId || prev.pendingAskUser?.toolCallId || '';
            if (!toolCallId) return prev;
            pendingToolCallIdsRef.current.delete(toolCallId);
            return {
              ...prev,
              isRunning: computeIsRunning(),
              pendingAskUser:
                !options?.olderReplay && prev.pendingAskUser?.toolCallId === toolCallId
                  ? null
                  : prev.pendingAskUser,
              messages: prev.messages.map((message) => ({
                ...message,
                parts: message.parts.map((part) =>
                  part.type === 'ask-user' && part.toolCallId === toolCallId
                    ? {
                        ...part,
                        status: 'submitted' as const,
                        ...(answers.length > 0 ? { answers } : {}),
                      }
                    : part,
                ),
              })),
              toolCards: prev.toolCards.map((tool) =>
                tool.toolCallId === toolCallId && tool.status === 'running'
                  ? { ...tool, status: 'ok' as const }
                  : tool,
              ),
            };
          });
          addLedger(seq, 'ask_user.responded', '问卷已回答');
          break;
        }

        case 'round.started': {
          const roundId = options?.runId || `round-${seq}`;
          if (roundId) completedRoundIdsRef.current.delete(roundId);
          if (cancelPendingRef.current) {
            ignoredRoundIdsRef.current.add(roundId);
          } else {
            activateRound(roundId);
          }
          if (cancelPendingRef.current) {
            setState((prev) => ({ ...prev, isRunning: false, isCancelling: true }));
          }
          addLedger(seq, 'round.started', roundId);
          break;
        }

        case 'assistant.reasoning': {
          const p = payload as Record<string, unknown>;
          const text = String(p.text ?? p.content ?? p.delta ?? '');
          const roundId = options?.runId || currentRoundIdRef.current;
          if (roundId && completedRoundIdsRef.current.has(roundId)) break;
          if (roundId && (runningRoundIdsRef.current.has(roundId) || activateRound(roundId))) {
            setRoundBlock(roundId, Number(p.block ?? p.gen ?? 0), 'reasoning', text, false);
          }
          break;
        }

        case 'reasoning.delta':
        case 'text.delta': {
          const p = payload as Record<string, unknown>;
          const roundId = options?.runId || String(p.roundId ?? '') || currentRoundIdRef.current;
          if (roundId && completedRoundIdsRef.current.has(roundId)) break;
          const kind = eventName === 'text.delta' ? 'text' : 'reasoning';
          const text = String(p.text ?? p.delta ?? p.content ?? '');
          const block = Number(p.block ?? p.gen ?? 0);
          if (
            roundId &&
            text &&
            (runningRoundIdsRef.current.has(roundId) || activateRound(roundId))
          ) {
            setRoundBlock(roundId, block, kind, text, true);
          }
          break;
        }

        case 'assistant.text': {
          const p = payload as Record<string, unknown>;
          const text = String(p.text ?? p.content ?? p.delta ?? '');
          const roundId = options?.runId || currentRoundIdRef.current;
          if (roundId && completedRoundIdsRef.current.has(roundId)) break;
          if (roundId && (runningRoundIdsRef.current.has(roundId) || activateRound(roundId))) {
            setRoundBlock(roundId, Number(p.block ?? p.gen ?? 0), 'text', text, false);
          }
          break;
        }

        case 'assistant.tool_call': {
          const p = payload as Record<string, unknown>;
          const roundId = options?.runId || currentRoundIdRef.current;
          if (roundId && ignoredRoundIdsRef.current.has(roundId)) break;
          const toolCallId = String(p.toolCallId ?? p.tool_call_id ?? p.id ?? '');
          const toolName = String(p.toolName ?? p.tool_name ?? p.name ?? '');
          if (toolCallId) {
            if (!NON_BLOCKING_TOOLS.has(toolName)) {
              pendingToolCallIdsRef.current.add(toolCallId);
            }
            updateToolCard({
              toolCallId,
              toolName,
              status: 'running',
              input: p.input ?? p.arguments ?? p.args,
            });
            if (!options?.skipMessages) {
              const parts: MessagePart[] = [];
              if (streamReasonRef.current) {
                parts.push({ type: 'reasoning', text: streamReasonRef.current });
              }
              if (streamTextRef.current) {
                parts.push({ type: 'text', text: streamTextRef.current });
              }
              parts.push({
                type: 'tool-call',
                toolCallId,
                toolName,
                input: p.input ?? p.arguments ?? p.args,
                payload: p,
              });
              setState((prev) => ({
                ...prev,
                isRunning: true,
                messages: [
                  ...prev.messages,
                  {
                    role: 'assistant',
                    parts,
                    runId: prev.currentRunId || undefined,
                    createdAt: ev.createdAt,
                    id: seq,
                  },
                ],
                streamingText: '',
                streamingReason: '',
              }));
            } else {
              setState((prev) => ({ ...prev, isRunning: computeIsRunning() }));
            }
            streamTextRef.current = '';
            streamReasonRef.current = '';
            if (roundId) roundBlocksRef.current.delete(roundId);
          }
          addLedger(seq, 'assistant.tool_call', toolName || toolCallId);
          break;
        }

        case 'tool.exec_started': {
          const p = payload as Record<string, unknown>;
          // 被取消 round 的工具事件可能在重连补账本时重放；不拦会把工具锁加回来，
          // 而该轮已中断，终态事件永远不会来，输入框会永久卡在「生成中」
          const execRoundId = options?.runId || currentRoundIdRef.current;
          if (execRoundId && ignoredRoundIdsRef.current.has(execRoundId)) break;
          const toolCallId = String(p.toolCallId ?? p.tool_call_id ?? p.id ?? '');
          const toolName = String(p.toolName ?? p.tool_name ?? p.name ?? '');
          if (toolCallId) {
            if (!NON_BLOCKING_TOOLS.has(toolName)) {
              pendingToolCallIdsRef.current.add(toolCallId);
            }
            updateToolCard({
              toolCallId,
              toolName,
              status: 'running',
              input: p.input ?? p.arguments ?? p.args,
            });
            // 同步工具执行中保持忙碌；job_status 等非阻塞工具不锁输入
            if (!NON_BLOCKING_TOOLS.has(toolName)) {
              setState((prev) => ({ ...prev, isRunning: true, error: null }));
            }
          }
          addLedger(seq, 'tool.exec_started', toolName || toolCallId);
          break;
        }

        case 'tool.exec_succeeded':
        case 'tool.exec_failed': {
          const p = payload as Record<string, unknown>;
          const toolCallId = String(p.toolCallId ?? p.tool_call_id ?? p.id ?? '');
          const failed = eventName === 'tool.exec_failed';
          const execToolName = String(p.toolName ?? p.tool_name ?? p.name ?? '').trim();
          if (toolCallId) {
            pendingToolCallIdsRef.current.delete(toolCallId);
            updateToolCard({
              toolCallId,
              ...(execToolName ? { toolName: execToolName } : {}),
              status: failed ? 'err' : 'ok',
              // 保留完整 tool.exec_* payload，结果、错误、耗时、产物等字段均可回看。
              output: p,
              duration:
                typeof p.durationMillis === 'number'
                  ? p.durationMillis
                  : typeof p.duration_ms === 'number'
                    ? p.duration_ms
                    : undefined,
            });
          }
          setState((prev) => ({
            ...prev,
            isRunning: computeIsRunning(),
            stats: { ...prev.stats, tools: prev.stats.tools + 1 },
          }));
          addLedger(
            seq,
            eventName,
            String(
              p.toolName ?? p.tool_name ?? p.name ?? (failed ? '工具执行失败' : '工具执行完成'),
            ),
          );
          break;
        }

        case 'round.completed': {
          const roundId = options?.runId || currentRoundIdRef.current;
          const ignored = !!roundId && ignoredRoundIdsRef.current.has(roundId);
          if (!options?.skipMessages && !ignored) flushStreaming(seq);
          if (roundId) {
            completedRoundIdsRef.current.add(roundId);
            runningRoundIdsRef.current.delete(roundId);
            roundBlocksRef.current.delete(roundId);
          }
          if (currentRoundIdRef.current === roundId) currentRoundIdRef.current = null;
          if (options?.replay || ignored) {
            streamTextRef.current = '';
            streamReasonRef.current = '';
          }
          setState((prev) => ({
            ...prev,
            // 仍有工具在跑时不放开输入
            isRunning: computeIsRunning(),
            isCancelling: false,
            stats: { ...prev.stats, steps: prev.stats.steps + 1 },
            ...(options?.replay || ignored
              ? { streamingText: '', streamingReason: '' }
              : undefined),
          }));
          addLedger(seq, 'round.completed', '回复完成');
          break;
        }

        case 'stream.end': {
          // 仅表示本条 SSE 连接关闭（网关/服务端主动断流），不代表 round 已完成。
          // 续流由 useStream 无感重连 + /events catchUp 负责；此处只记账。
          addLedger(seq, 'stream.end', '实时流关闭，准备续连');
          break;
        }

        case 'execution.started': {
          executionInProgressRef.current = true;
          setState((prev) => ({
            ...prev,
            isRunning: computeIsRunning(),
            isCancelling: false,
            error: null,
          }));
          addLedger(seq, 'execution.started', '执行开始');
          break;
        }

        case 'execution.finished': {
          const finished =
            payload && typeof payload === 'object' && !Array.isArray(payload)
              ? (payload as Record<string, unknown>)
              : {};
          const status = String(finished.status ?? '').trim();
          const executionError = String(finished.error ?? '').trim();
          const cancelled = status.includes('CANCELLED');
          const failed = !cancelled && (status.includes('FAILED') || Boolean(executionError));
          const failureMessage = failed
            ? `执行失败：${formatExecutionFailure(executionError, status)}`
            : null;
          const terminalToolStatus = failed || cancelled ? ('err' as const) : ('ok' as const);
          const roundId = options?.runId || currentRoundIdRef.current;
          if (roundId) completedRoundIdsRef.current.add(roundId);
          for (const activeRoundId of runningRoundIdsRef.current) {
            completedRoundIdsRef.current.add(activeRoundId);
          }
          if (!options?.skipMessages) flushStreaming(seq);
          runningRoundIdsRef.current.clear();
          pendingToolCallIdsRef.current.clear();
          executionInProgressRef.current = false;
          currentRoundIdRef.current = null;
          streamTextRef.current = '';
          streamReasonRef.current = '';
          setState((prev) => ({
            ...prev,
            isRunning: false,
            isCancelling: false,
            streamingText: '',
            streamingReason: '',
            error: failureMessage,
            // execution.finished 是整次执行的最终事实；即使漏收 tool.exec_*，
            // 也不能让残留 running 工具继续把输入区显示成「停止生成」。
            toolCards: prev.toolCards.map((tool) =>
              tool.status === 'running' ? { ...tool, status: terminalToolStatus } : tool,
            ),
          }));
          addLedger(
            seq,
            'execution.finished',
            failed ? (failureMessage ?? '执行失败') : cancelled ? '执行已取消' : '执行完成',
          );
          break;
        }

        case 'round.cancelled': {
          if (!options?.skipMessages) preserveInterruptedOutput(seq);
          else discardActiveRounds();
          cancelPendingRef.current = false;
          addLedger(seq, 'round.cancelled', '已停止生成');
          break;
        }

        case 'round.failed': {
          const p = payload as Record<string, unknown>;
          const error = String(p.error ?? p.message ?? '运行失败');
          discardActiveRounds();
          cancelPendingRef.current = false;
          setState((prev) => ({ ...prev, isRunning: false, isCancelling: false, error }));
          addLedger(seq, 'round.failed', error);
          break;
        }

        case 'interrupted': {
          const roundId = options?.runId || currentRoundIdRef.current;
          if (roundId) ignoredRoundIdsRef.current.add(roundId);
          if (!options?.skipMessages) preserveInterruptedOutput(seq);
          else discardActiveRounds();
          cancelPendingRef.current = false;
          const p = payload as Record<string, unknown>;
          addLedger(seq, 'interrupted', String(p.detail ?? p.reason ?? '已停止生成'));
          break;
        }

        case 'error': {
          const p = payload as Record<string, unknown>;
          const error = String(p.message ?? p.error ?? '运行失败');
          discardActiveRounds();
          cancelPendingRef.current = false;
          setState((prev) => ({ ...prev, error }));
          addLedger(seq, 'error', error);
          break;
        }

        case 'run.started': {
          const p = payload as RunStartedPayload;
          setState((prev) => ({
            ...prev,
            runSnapshot: {
              profileVersion: p.profileVersion,
              promptSha256: p.promptSha256,
              workerId: p.workerId,
            },
          }));
          addLedger(
            seq,
            'run.started',
            `${p.model || ''} · ${(p.tools || []).length} 个工具 · v${p.profileVersion || '?'}`,
          );
          break;
        }

        case 'step.completed': {
          const p = payload as StepCompletedPayload;
          setState((prev) => {
            const stats = {
              ...prev.stats,
              steps: Math.max(prev.stats.steps, p.step || 0),
              inTok: prev.stats.inTok + (p.usage?.inputTokens || 0),
              outTok: prev.stats.outTok + (p.usage?.outputTokens || 0),
              modelMs: prev.stats.modelMs + (p.modelMillis || 0),
            };
            return { ...prev, stats };
          });
          addLedger(
            seq,
            'step.completed',
            `第 ${p.step || 0} 步 · 输入 ${p.usage?.inputTokens || 0} / 输出 ${p.usage?.outputTokens || 0}${p.usage?.cacheReadTokens ? ` / 缓存 ${p.usage.cacheReadTokens}` : ''} · ${p.modelMillis || 0}ms`,
          );
          break;
        }

        case 'item.completed': {
          const p = payload as ItemCompletedPayload;
          const item = p.item;
          const itemRecord = item as { role?: string; type?: string } | undefined;
          const itemType = typeof itemRecord?.type === 'string' ? itemRecord.type : '';
          // 兼容 role 缺失、仅用 type=user_message / user 标识的用户项
          const role =
            item?.role ||
            (itemType === 'user_message' || itemType === 'user' ? 'user' : 'assistant');
          addLedger(seq, 'item.completed', role);

          if (role === 'assistant') {
            // tool-call 部分始终更新 toolCards（即使 skipMessages）
            for (const pt of item?.parts || []) {
              if (pt.type === 'tool-call' && pt.toolCallId && pt.toolName) {
                updateToolCard({
                  toolCallId: pt.toolCallId,
                  toolName: pt.toolName,
                  status: 'running',
                  input: pt.input,
                });
              }
            }
            // 重放历史事件时（skipMessages），只更新 toolCards，不改 messages
            if (!options?.skipMessages) {
              // 对齐 demoui completeAssistantItem：append-only，每个 item 落地都追加新气泡，
              // 绝不删除已有消息；正文以账本为准，账本没有 text 时用流式累积兜底
              // （delta 是瞬态尽力投递，账本 item 可能只有 reasoning part）
              const itemParts = item?.parts || [];
              const reasoningTexts = itemParts
                .filter((pt) => pt.type === 'reasoning' && typeof pt.text === 'string')
                .map((pt) => (pt as { text: string }).text);
              const textTexts = itemParts
                .filter((pt) => pt.type === 'text' && typeof pt.text === 'string')
                .map((pt) => (pt as { text: string }).text);
              const toolCallParts: MessagePart[] = [];
              for (const pt of itemParts) {
                if (pt.type === 'tool-call' && pt.toolCallId && pt.toolName) {
                  toolCallParts.push({
                    type: 'tool-call',
                    toolCallId: pt.toolCallId,
                    toolName: pt.toolName,
                    input: pt.input,
                    payload: pt.payload,
                  });
                }
              }

              const parts: MessagePart[] = [];
              if (reasoningTexts.length > 0) {
                for (const t of reasoningTexts) parts.push({ type: 'reasoning', text: t });
              } else if (streamReasonRef.current) {
                parts.push({ type: 'reasoning', text: streamReasonRef.current });
              }
              // 正文按 demoui 拼成一段（账本优先，流式兜底）
              const text = textTexts.length > 0 ? textTexts.join('') : streamTextRef.current;
              if (text) parts.push({ type: 'text', text });
              parts.push(...toolCallParts);

              // stream.end 可能已把同一段 delta 落成气泡；账本迟到时避免重复助手气泡
              const streamAlreadyFlushed = !streamTextRef.current && !streamReasonRef.current;

              // finalizeAssistantBubble：清空流式现场（ref 与 state 同步清）
              streamTextRef.current = '';
              streamReasonRef.current = '';
              setState((prev) => {
                if (streamAlreadyFlushed && text && toolCallParts.length === 0) {
                  const last = prev.messages[prev.messages.length - 1];
                  if (last?.role === 'assistant') {
                    const lastText = last.parts
                      .filter((p): p is Extract<MessagePart, { type: 'text' }> => p.type === 'text')
                      .map((p) => p.text)
                      .join('');
                    if (lastText === text) {
                      return { ...prev, streamingText: '', streamingReason: '' };
                    }
                  }
                }
                return {
                  ...prev,
                  messages:
                    parts.length > 0
                      ? [
                          ...prev.messages,
                          {
                            role: 'assistant',
                            parts,
                            runId: prev.currentRunId || undefined,
                            // 优先用后端最后一个 delta 的 ts 作为准确时间戳，fallback 到当前时间
                            createdAt: lastTsRef.current
                              ? new Date(lastTsRef.current).toISOString()
                              : new Date().toISOString(),
                            id: seq,
                          },
                        ]
                      : prev.messages,
                  streamingText: '',
                  streamingReason: '',
                };
              });
            }
          } else if (role === 'user') {
            // 新账本可能用 item.completed(role=user) 代替独立的 user.input
            if (!options?.skipMessages) {
              const source = item ?? payload;
              const text = extractUserInputText(source);
              const parts = normalizeUserMessageParts(text, extractUserAttachments(source));
              if (parts.length > 0) {
                addMessage(
                  {
                    role: 'user',
                    parts,
                    createdAt: ev.createdAt,
                    id: seq,
                  },
                  options?.runId,
                  { noMerge: options?.olderReplay },
                );
              }
            }
          } else if (role === 'tool') {
            for (const pt of item?.parts || []) {
              if (pt.type === 'tool-result' && pt.toolCallId) {
                updateToolCard({
                  toolCallId: pt.toolCallId,
                  output: pt.output,
                  status: pt.isError ? 'err' : 'ok',
                });
              }
            }
          }
          break;
        }

        case 'tool.executed': {
          const p = payload as ToolExecutedPayload;
          setState((prev) => ({
            ...prev,
            stats: { ...prev.stats, tools: prev.stats.tools + 1 },
          }));
          if (p.toolCallId) {
            updateToolCard({
              toolCallId: p.toolCallId,
              status: p.isError ? 'err' : 'ok',
              duration: p.durationMillis,
            });
          }
          addLedger(
            seq,
            'tool.executed',
            `${p.toolName || ''} · ${p.durationMillis || 0}ms${p.isError ? ' · 失败' : ''}`,
          );
          break;
        }

        case 'run.completed': {
          const p = payload as RunCompletedPayload;
          addLedger(seq, 'run.completed', `运行完成 · ${p.steps || 0} 步`);
          // run 结束但最后的 item 没到时，把流式累积落成消息（对齐 demoui finalizeAssistantBubble）
          if (!options?.skipMessages) flushStreaming(seq);
          // 终态化所有残留 running 的 toolCard
          setState((prev) => ({
            ...prev,
            isRunning: false,
            isCancelling: false,
            toolCards: prev.toolCards.map((t) =>
              t.status === 'running' ? { ...t, status: 'ok' as const } : t,
            ),
          }));
          // run 终态后先对账再决定去向：接力唤醒 run / 轮询等待后台任务 / 真正收尾
          if (!options?.replay) onRunSettledRef.current?.(options?.runId);
          break;
        }

        case 'run.failed': {
          const p = payload as RunFailedPayload;
          const errMsg = p.error || '未知错误';
          addLedger(seq, 'run.failed', `${errMsg} · ${p.steps || 0} 步`);
          // run 结束但最后的 item 没到时，把流式累积落成消息
          if (!options?.skipMessages) flushStreaming(seq);
          // 终态化所有残留 running 的 toolCard
          setState((prev) => ({
            ...prev,
            isRunning: false,
            isCancelling: false,
            error: errMsg,
            toolCards: prev.toolCards.map((t) =>
              t.status === 'running' ? { ...t, status: 'err' as const } : t,
            ),
          }));
          // run 终态后先对账再决定去向：接力唤醒 run / 轮询等待后台任务 / 真正收尾
          if (!options?.replay) onRunSettledRef.current?.(options?.runId);
          break;
        }

        case 'job.submitted':
        case 'job.completed':
        case 'job.failed': {
          handleJobEvent(
            eventName,
            payload,
            options?.runId,
            seq,
            options?.replay,
            !options?.skipMessages,
          );
          break;
        }

        default:
          break;
      }
    },
    [
      addLedger,
      addMessage,
      updateToolCard,
      handleJobEvent,
      flushStreaming,
      activateRound,
      setRoundBlock,
      discardActiveRounds,
      preserveInterruptedOutput,
      computeIsRunning,
    ],
  );

  return {
    state,
    setState,
    lastSeqRef,
    ledgerStoreRef,
    mergeLedgerEvents,
    clearLedgerStore,
    lastTsRef,
    streamTextRef,
    streamReasonRef,
    abortRef,
    streamRetryTimerRef,
    streamGenerationRef,
    bindGraceUntilRef,
    closeStream,
    addMessage,
    addLedger,
    applyRunEvent,
    applySessionDelta,
    appendStreamingText,
    appendStreamingReason,
    handleJobEvent,
    resetFollowState,
    pendingJobsRef,
    wakeQueueRef,
    watchRunsRef,
    followTimerRef,
    onRunSettledRef,
    currentRoundIdRef,
    runningRoundIdsRef,
    pendingToolCallIdsRef,
    executionInProgressRef,
    computeIsRunning,
    beginExecutionFollow,
    ignoredRoundIdsRef,
    cancelPendingRef,
    discardActiveRounds,
    preserveInterruptedOutput,
    resetSessionStreamState,
    settleToolCall,
    settleAskUser,
    finalizeReplayBusyState,
    syncReplayBusyStateForResume,
    finalizeStreamEnd,
    flushStreaming,
  };
}

export type SSEStateHandle = ReturnType<typeof useSSEState>;
