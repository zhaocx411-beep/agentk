/**
 * SSE 流式对话 Hook — 组装层
 * 使用 fetch + ReadableStream 替代 EventSource，解决：
 * 1. EventSource 看不到 HTTP 状态码（404/500 无法区分）
 * 2. 后端 queued → running 有延迟，需要等 stream 端点就绪后再连接
 * 3. 手动控制重试，避免浏览器自动重连导致事件重复
 */
import { useCallback, useEffect, useMemo, useRef } from 'react';
import { message } from '@/utils/feedback';
import { cancelSession, sendMessage, submitAnswers } from '../api';
import {
  attachmentsFromUserInput,
  composeMessageWithAttachments,
  partitionSendMessageAttachments,
  validateSendMessageAttachments,
} from '@/services/media';
import { assertOutboundMediaWithinLimit } from '@/services/mediaSizeGuard';
import type {
  AskUserAnswer,
  MessageAttachment,
  MessageItem,
  SendMessageAttachmentType,
  SendMessageOptions,
  SessionLedgerEvent,
} from '../types';
import {
  asErrorMessage,
  buildToolCardsFromMessages,
  normalizeUserMessageParts,
  resolveEventPayload,
} from './sse/helpers';
import type { SSEState, SSEActions } from './sse/types';
import { INITIAL_SSE_STATE } from './sse/types';
import { useSSEState } from './sse/useSSEState';
import { useStream } from './sse/useStream';
import {
  comparePlatformSessionLedgerEvents,
  compareSessionLedgerEvents,
} from '../utils/sessionLedgerOrder';
import { isExecutionFinishedInLedger } from '../utils/sessionStreamResume';

export type { SSEState, SSEActions };

/** 后端判定该问卷不再等待回答（超时 / 已答 / 用户已用普通消息接管） */
function isAskUserSettled(err: unknown): boolean {
  return /no longer awaiting an answer/i.test(asErrorMessage(err));
}

function timestamp(value?: string): number {
  const parsed = value ? Date.parse(value) : Number.NaN;
  return Number.isFinite(parsed) ? parsed : 0;
}

function messageSnapshotKey(messageItem: MessageItem): string {
  return JSON.stringify([
    messageItem.role,
    messageItem.createdAt ?? '',
    messageItem.runId ?? '',
    messageItem.parts,
  ]);
}

interface OptimisticSessionMessage {
  message: MessageItem;
  executionId?: string;
}

interface UseSSEOptions {
  /** 仅明确支持消息 V3 的 Agent 开启；其他 Agent 继续只通过 text 接收附件 URL。 */
  enableStructuredAttachments?: boolean;
  /** 当前模型允许走消息 V3 attachments 的媒体类型；未提供时默认旧链路。 */
  structuredAttachmentTypes?: readonly SendMessageAttachmentType[];
}

const ALL_STRUCTURED_ATTACHMENT_TYPES: readonly SendMessageAttachmentType[] = [
  'image',
  'video',
  'audio',
];

export function useSSE({
  enableStructuredAttachments = false,
  structuredAttachmentTypes,
}: UseSSEOptions = {}): [SSEState, SSEActions] {
  const h = useSSEState();
  const { state, setState, closeStream, applyRunEvent } = h;
  const { openStream, setStreamCursor, resetStreamBinding } = useStream(h);
  const structuredAttachmentTypeSet = useMemo(
    () =>
      new Set<SendMessageAttachmentType>(
        enableStructuredAttachments
          ? ALL_STRUCTURED_ATTACHMENT_TYPES
          : (structuredAttachmentTypes ?? []),
      ),
    [enableStructuredAttachments, structuredAttachmentTypes],
  );
  const currentSessionIdRef = useRef<string>('');
  /**
   * POST /messages、POST /answers 已返回、但新 execution 尚未落下首个账本事件时，
   * sessions/query 仍可能保留上一个 execution。切走再切回必须优先续订客户端已知的新 ID。
   */
  const pendingExecutionBySessionRef = useRef(
    new Map<
      string,
      { executionId: string; answeredToolCallId?: string; answeredAnswers?: AskUserAnswer[] }
    >(),
  );
  /** user.input 正式事件到达前，保留乐观消息，避免快速切换时被滞后的历史第一页覆盖。 */
  const optimisticMessagesBySessionRef = useRef(new Map<string, OptimisticSessionMessage[]>());
  /** 防止连点 / StrictMode 双调用把取消请求打两次 */
  const cancelInFlightRef = useRef(false);

  /**
   * 打断当前会话正在执行的 round：保留已看到的半截回答，屏蔽该 round 的在途事件，断开 stream。
   * silent 用于「发新消息打断上一轮」，此时不提示「已停止生成」——用户看到的是新一轮开始。
   * @returns 是否真正发出并成功取消；调用方可用它回滚乐观 UI
   */
  const interrupt = useCallback(
    async (opts?: { silent?: boolean }): Promise<boolean> => {
      const sid = currentSessionIdRef.current;
      if (!sid || cancelInFlightRef.current) return false;
      cancelInFlightRef.current = true;
      h.cancelPendingRef.current = true;
      setState((prev) => ({ ...prev, isCancelling: true }));
      try {
        await cancelSession(sid, opts?.silent ? '用户发送新消息打断上一轮' : '用户主动停止生成');
        // 保留用户已经看到的半截回答，但持续屏蔽该 round 的在途事件。
        h.preserveInterruptedOutput();
        // 用户已明确停止：不再跟随后台 job / 唤醒轮，也不要空转 stream
        h.resetFollowState();
        closeStream({ force: true });
        setState((prev) => ({
          ...prev,
          isRunning: false,
          isCancelling: false,
          // 用户停止后未完成的后台任务直接拿掉，执行详情不再挂「进行中/已停止」看板
          jobs: prev.jobs.filter((job) => job.status === 'completed'),
        }));
        if (!opts?.silent) message.success('已停止生成');
        return true;
      } catch (err: unknown) {
        h.cancelPendingRef.current = false;
        setState((prev) => ({
          ...prev,
          isCancelling: false,
          error: `停止失败：${asErrorMessage(err)}`,
        }));
        return false;
      } finally {
        cancelInFlightRef.current = false;
      }
    },
    [closeStream, h, setState],
  );

  // ===== 对外暴露的操作方法 =====

  const send = useCallback(
    async (
      sid: string,
      text: string,
      attachments?: MessageAttachment[],
      options?: SendMessageOptions,
    ) => {
      const trimmed = text.trim();
      const files = attachments?.filter((a) => a.url.trim()) ?? [];
      const userInputFiles = attachmentsFromUserInput(options?.userInput);
      const allFiles = [...files, ...userInputFiles];
      if (!trimmed && allFiles.length === 0) return false;
      const { attachments: optionAttachments = [], ...messageOptions } = options ?? {};
      const outboundAttachments = [
        ...optionAttachments,
        ...partitionSendMessageAttachments(allFiles).attachments,
      ].filter((attachment) => structuredAttachmentTypeSet.has(attachment.type));
      if (outboundAttachments.length > 0) {
        const attachmentError = validateSendMessageAttachments(outboundAttachments);
        if (attachmentError) {
          message.warning(attachmentError);
          return false;
        }
      }
      try {
        await assertOutboundMediaWithinLimit({
          attachments: allFiles,
          payload: { text: trimmed, userInput: options?.userInput },
        });
      } catch (err) {
        message.error(asErrorMessage(err));
        return false;
      }
      // 生成中直接发新消息 = 打断上一轮立刻开新的。口径与 UI 的忙碌态一致：
      // 未闭合的 round 或执行中的同步工具都要先打断；只有后台 job 在跑时不打断（打断会连它一起取消）。
      // 换会话不必打断：startSession 已经清掉了上一会话的 round 状态。
      if (sid === currentSessionIdRef.current && h.computeIsRunning()) {
        const stopped = await interrupt({ silent: true });
        // 停不掉就不抢发，否则两轮会并行往同一会话写
        if (!stopped) return false;
      }
      currentSessionIdRef.current = sid;
      // 上一轮已经被明确加入 ignored 集合，新提问不必等待 interrupted 帧才可开始。
      h.cancelPendingRef.current = false;

      h.streamTextRef.current = '';
      h.streamReasonRef.current = '';
      setState((prev) => ({
        ...prev,
        isCancelling: false,
        streamingText: '',
        streamingReason: '',
        stats: { steps: 0, inTok: 0, outTok: 0, modelMs: 0, tools: 0 },
        ledger: [],
        error: null,
        // 问卷还在时直接发消息 = 接管对话，撤下问卷（之后不能再调 /answers）
        pendingAskUser: null,
      }));

      // 所有 Agent 都在 text 保留 URL 清单；仅发送当前模型明确支持的 V3 attachments 类型。
      // 气泡仍使用 trimmed + allFiles 构建，不向用户展示这段 URL 清单。
      const outboundText = composeMessageWithAttachments(trimmed, files);
      const outboundOptions: SendMessageOptions | undefined =
        Object.keys(messageOptions).length > 0 || outboundAttachments.length > 0
          ? {
              ...messageOptions,
              ...(outboundAttachments.length > 0 ? { attachments: outboundAttachments } : {}),
            }
          : undefined;
      const parts = normalizeUserMessageParts(trimmed, allFiles);
      const optimisticMessage: MessageItem | null =
        parts.length > 0
          ? {
              role: 'user',
              parts,
              createdAt: new Date().toISOString(),
              id: Date.now(),
            }
          : null;
      if (optimisticMessage) {
        const messages = optimisticMessagesBySessionRef.current.get(sid) ?? [];
        optimisticMessagesBySessionRef.current.set(sid, [
          ...messages,
          { message: optimisticMessage },
        ]);
        h.addMessage(optimisticMessage);
      }

      try {
        // 先发送消息，再从当前账本游标开始订阅；每次提问只新建一次 session stream。
        closeStream({ force: true });
        const resp = await sendMessage(sid, outboundText, outboundOptions);
        const executionId = resp?.executionId?.trim();
        const isCurrentSession = currentSessionIdRef.current === sid;
        if (optimisticMessage && executionId) {
          optimisticMessagesBySessionRef.current.set(
            sid,
            (optimisticMessagesBySessionRef.current.get(sid) ?? []).map((item) =>
              item.message.id === optimisticMessage.id ? { ...item, executionId } : item,
            ),
          );
        }
        // demoui：POST 响应带回落库 user.input，立即对齐（与乐观气泡合并）
        if (Array.isArray(resp?.events)) {
          let maxSeq = 0;
          if (isCurrentSession) {
            for (const ev of resp.events) {
              applyRunEvent(
                {
                  seq: ev.seq,
                  kind: ev.kind,
                  payload: resolveEventPayload(ev),
                  createdAt: ev.createdAt || new Date().toISOString(),
                  executionId: ev.executionId,
                  eventId: ev.eventId,
                },
                { runId: ev.roundId, skipDedupe: true },
              );
              maxSeq = Math.max(maxSeq, ev.seq);
            }
            setStreamCursor(maxSeq);
          }
        }
        if (!executionId) {
          if (optimisticMessage) {
            optimisticMessagesBySessionRef.current.set(
              sid,
              (optimisticMessagesBySessionRef.current.get(sid) ?? []).filter(
                (item) => item.message.id !== optimisticMessage.id,
              ),
            );
          }
          if (isCurrentSession) {
            setState((prev) => {
              const messages = [...prev.messages];
              if (messages[messages.length - 1]?.role === 'user') {
                messages.pop();
              }
              return {
                ...prev,
                messages,
                isRunning: false,
                isCancelling: false,
                error: '发送成功但未返回 executionId，无法订阅实时流',
              };
            });
          }
          return false;
        }
        pendingExecutionBySessionRef.current.set(sid, { executionId });
        if (isCurrentSession) {
          openStream(sid, { executionId, after: 0 });
        }
        return true;
      } catch (err: unknown) {
        if (optimisticMessage) {
          optimisticMessagesBySessionRef.current.set(
            sid,
            (optimisticMessagesBySessionRef.current.get(sid) ?? []).filter(
              (item) => item.message.id !== optimisticMessage.id,
            ),
          );
        }
        // 发送失败：撤掉乐观用户气泡，便于重试；输入区附件由 ChatArea 保留
        if (currentSessionIdRef.current === sid) {
          setState((prev) => {
            const messages = [...prev.messages];
            if (messages[messages.length - 1]?.role === 'user') {
              messages.pop();
            }
            return {
              ...prev,
              messages,
              isRunning: false,
              isCancelling: false,
              error: asErrorMessage(err),
            };
          });
        }
        return false;
      }
    },
    [
      applyRunEvent,
      closeStream,
      h,
      interrupt,
      openStream,
      setState,
      setStreamCursor,
      structuredAttachmentTypeSet,
    ],
  );

  /**
   * 会话级取消：不依赖「当前是否有活跃 round」。
   * Agent 把视频丢进后台后 isRunning 会先变 false，这时用户仍应能停掉整次生成；
   * 取消后清掉 job 跟随并断开 stream，避免 204 空转后又被唤醒 round 拉成 200。
   */
  const cancel = useCallback(() => interrupt(), [interrupt]);

  /** 从 ledgerStore 按 seq 全量重放 UI；stream 实时写入的事件也在 store 里，可与分页历史拼接 */
  const replayFromLedgerStore = useCallback((): boolean => {
    if (h.computeIsRunning() || h.streamTextRef.current || h.streamReasonRef.current) {
      return false;
    }

    h.resetSessionStreamState();
    h.lastSeqRef.current = 0;
    setState((prev) => ({
      ...prev,
      messages: [],
      toolCards: [],
      jobs: [],
      ledger: [],
      streamingText: '',
      streamingReason: '',
      isRunning: false,
      isCancelling: false,
      pendingAskUser: null,
      error: null,
      stats: { ...INITIAL_SSE_STATE.stats },
      runSnapshot: null,
      currentRunId: null,
    }));

    const ordered = [...h.ledgerStoreRef.current.values()].sort(compareSessionLedgerEvents);
    for (const event of ordered) {
      applyRunEvent(
        {
          seq: event.seq,
          kind: event.kind,
          payload: resolveEventPayload(event),
          createdAt: event.createdAt,
          executionId: event.executionId,
          eventId: event.eventId,
        },
        {
          runId: event.roundId,
          replay: true,
          skipDedupe: true,
          skipLedgerStore: true,
        },
      );
    }

    const maxSeq = ordered[ordered.length - 1]?.seq ?? 0;
    h.lastSeqRef.current = maxSeq;
    setStreamCursor(maxSeq);
    h.finalizeReplayBusyState();
    return true;
  }, [applyRunEvent, h, setState, setStreamCursor]);

  const startSession = useCallback(
    (
      sessionId: string,
      history: SessionLedgerEvent[] = [],
      options?: { resumeExecutionId?: string },
    ) => {
      const optimisticEntries = optimisticMessagesBySessionRef.current.get(sessionId) ?? [];
      currentSessionIdRef.current = sessionId;
      closeStream();
      h.resetFollowState();
      h.resetSessionStreamState();
      resetStreamBinding();
      h.clearLedgerStore();
      setState((prev) => ({
        ...prev,
        messages: [],
        toolCards: [],
        jobs: [],
        currentRunId: null,
        isRunning: false,
        isCancelling: false,
        streamingText: '',
        streamingReason: '',
        stats: { ...INITIAL_SSE_STATE.stats },
        ledger: [],
        error: null,
        runSnapshot: null,
        pendingAskUser: null,
      }));
      const orderedHistory = [...history].sort(comparePlatformSessionLedgerEvents);
      h.mergeLedgerEvents(orderedHistory);
      h.lastSeqRef.current = 0;
      for (const event of orderedHistory) {
        // data / payload 双字段兼容；空 data 时回退整事件，避免 user.input 丢正文
        applyRunEvent(
          {
            seq: event.seq,
            kind: event.kind,
            payload: resolveEventPayload(event),
            createdAt: event.createdAt,
            executionId: event.executionId,
            eventId: event.eventId,
          },
          {
            runId: event.roundId,
            replay: true,
            skipDedupe: true,
            skipLedgerStore: true,
          },
        );
      }
      const rememberedExecution = pendingExecutionBySessionRef.current.get(sessionId);
      if (
        rememberedExecution &&
        isExecutionFinishedInLedger(orderedHistory, rememberedExecution.executionId)
      ) {
        pendingExecutionBySessionRef.current.delete(sessionId);
      }
      const activeRememberedExecution = pendingExecutionBySessionRef.current.get(sessionId);
      // 本地 POST 响应比可能滞后的 sessions/query 更新；无账本事件窗口优先使用本地 ID。
      const resumeExecutionId =
        activeRememberedExecution?.executionId || options?.resumeExecutionId?.trim();
      // 切回会话后本地 UI 已重置，必须从 execution 起点重放；eventId 会过滤历史已存在事件。
      const streamCursor = 0;
      h.lastSeqRef.current = streamCursor;
      // 切换/创建会话时只准备游标；send 或 resumeExecutionId 会触发订流
      setStreamCursor(streamCursor);
      if (resumeExecutionId) {
        if (activeRememberedExecution) {
          setState((prev) => {
            const pendingExecutionId = prev.pendingAskUser?.executionId?.trim();
            return prev.pendingAskUser &&
              pendingExecutionId !== activeRememberedExecution.executionId
              ? { ...prev, pendingAskUser: null }
              : prev;
          });
        }
        if (activeRememberedExecution?.answeredToolCallId) {
          const answeredToolCallId = activeRememberedExecution.answeredToolCallId;
          // ask_user.responded 尚未落库时，回放会重新挂出旧问卷；本地已成功提交，不能允许二次回答。
          h.settleAskUser(
            answeredToolCallId,
            'submitted',
            activeRememberedExecution.answeredAnswers,
          );
          h.settleToolCall(answeredToolCallId);
        }
        // 续流：保留 tool.exec_started 等忙碌态；长任务期间 stream 可能只有 keepalive
        h.syncReplayBusyStateForResume();
        openStream(sessionId, { executionId: resumeExecutionId, after: streamCursor });
      } else {
        // 静态回放：清掉残留 busy，避免误锁输入
        h.finalizeReplayBusyState();
      }
      if (optimisticEntries.length > 0) {
        const acknowledgedExecutions = new Set(
          orderedHistory
            .filter((event) => event.kind === 'user.input')
            .map((event) => event.executionId?.trim())
            .filter((executionId): executionId is string => Boolean(executionId)),
        );
        const pendingEntries = optimisticEntries.filter(
          (item) => !item.executionId || !acknowledgedExecutions.has(item.executionId),
        );
        optimisticMessagesBySessionRef.current.set(sessionId, pendingEntries);
        const localOnlyMessages = pendingEntries.map((item) => item.message);
        if (localOnlyMessages.length > 0) {
          setState((prev) => {
            const seen = new Set(prev.messages.map(messageSnapshotKey));
            const messages = [...prev.messages];
            for (const messageItem of localOnlyMessages) {
              const key = messageSnapshotKey(messageItem);
              if (seen.has(key)) continue;
              seen.add(key);
              messages.push(messageItem);
            }
            messages.sort((left, right) => timestamp(left.createdAt) - timestamp(right.createdAt));
            return { ...prev, messages };
          });
        }
      }
    },
    [setState, h, closeStream, setStreamCursor, resetStreamBinding, applyRunEvent, openStream],
  );

  const resumeStream = useCallback(
    (sessionId: string, executionId: string) => {
      const trimmed = executionId.trim();
      if (!trimmed || currentSessionIdRef.current !== sessionId) return;
      const after = 0;
      h.lastSeqRef.current = after;
      openStream(sessionId, { executionId: trimmed, after });
    },
    [h, openStream],
  );

  const appendReplayEvents = useCallback(
    (history: SessionLedgerEvent[]) => {
      if (history.length === 0) return;
      h.mergeLedgerEvents(history);
      if (replayFromLedgerStore()) return;
      const orderedHistory = [...history].sort(comparePlatformSessionLedgerEvents);
      for (const event of orderedHistory) {
        applyRunEvent(
          {
            seq: event.seq,
            kind: event.kind,
            payload: resolveEventPayload(event),
            createdAt: event.createdAt,
            executionId: event.executionId,
            eventId: event.eventId,
          },
          {
            runId: event.roundId,
            replay: true,
            skipDedupe: true,
            skipLedgerStore: true,
          },
        );
      }
      if (orderedHistory.length > 0) {
        const maxSeq = orderedHistory[orderedHistory.length - 1]?.seq ?? 0;
        h.lastSeqRef.current = Math.max(h.lastSeqRef.current, maxSeq);
        setStreamCursor(h.lastSeqRef.current);
      }
      h.finalizeReplayBusyState();
    },
    [applyRunEvent, h, replayFromLedgerStore, setStreamCursor],
  );

  /**
   * 上滚加载更早账本：合并进 ledgerStore 后全量重放，避免增量 prepend 污染已完成的对话。
   * stream 续订写入的事件也在 store 中，重放后与游标 after= 自然衔接。
   * @returns false 表示正在生成、本次未加载，调用方应稍后重试
   */
  const prependReplayEvents = useCallback(
    (history: SessionLedgerEvent[]): boolean => {
      if (history.length === 0) return true;
      h.mergeLedgerEvents(history);
      return replayFromLedgerStore();
    },
    [h, replayFromLedgerStore],
  );

  /** 生成中无法全量重放时，先把更早页写入 ledger，结束后再 prepend / 上滚回放 */
  const stageLedgerEvents = useCallback(
    (history: SessionLedgerEvent[]) => {
      if (history.length === 0) return;
      h.mergeLedgerEvents(history);
    },
    [h],
  );

  const reset = useCallback(() => {
    closeStream();
    h.resetFollowState();
    h.resetSessionStreamState();
    h.clearLedgerStore();
    resetStreamBinding();
    h.lastSeqRef.current = 0;
    optimisticMessagesBySessionRef.current.clear();
    pendingExecutionBySessionRef.current.clear();
    setState({ ...INITIAL_SSE_STATE });
  }, [closeStream, setState, h, resetStreamBinding]);

  const loadHistory = useCallback(
    (messages: MessageItem[]) => {
      closeStream();
      h.resetFollowState();
      h.resetSessionStreamState();
      resetStreamBinding();
      // 从历史消息中重建 toolCards（否则历史 tool-call 会一直显示"执行中"）
      const toolCards = buildToolCardsFromMessages(messages);
      setState({ ...INITIAL_SSE_STATE, messages, toolCards });
    },
    [setState, h, closeStream, resetStreamBinding],
  );

  const prependHistory = useCallback(
    (messages: MessageItem[]) => {
      // 向上滚动加载更早历史时，同样重建 toolCards 并合并
      const newToolCards = buildToolCardsFromMessages(messages);
      setState((prev) => {
        // 合并：旧 toolCards 中没有的加入，已有的保留（避免重复）
        const merged = [...prev.toolCards];
        const existingIds = new Set(merged.map((t) => t.toolCallId));
        for (const card of newToolCards) {
          if (!existingIds.has(card.toolCallId)) {
            merged.push(card);
          }
        }
        return {
          ...prev,
          messages: [...messages, ...prev.messages],
          toolCards: merged,
        };
      });
    },
    [setState],
  );

  /** 提交 ask_user 问卷答案：成功后账本追加 ask_user.responded，会话继续执行 */
  const answer = useCallback(
    async (toolCallId: string, answers: AskUserAnswer[]): Promise<boolean> => {
      const sid = currentSessionIdRef.current;
      if (!sid) return false;
      try {
        const resp = await submitAnswers(sid, { toolCallId, answers });
        const executionId = resp?.executionId?.trim();
        const isCurrentSession = currentSessionIdRef.current === sid;
        // 乐观切为只读已提交卡片；若请求返回前已切换会话，不能污染新会话状态。
        if (isCurrentSession) {
          h.settleAskUser(toolCallId, 'submitted', answers);
        }
        if (!executionId) {
          // 回答已经提交成功，但绝不能复用 ask_user 前已 finished 的 execution。
          if (isCurrentSession) {
            closeStream({ force: true });
            resetStreamBinding();
            setState((prev) => ({
              ...prev,
              error: '回答成功但未返回 executionId，无法订阅后续实时流',
            }));
          }
          return true;
        }
        // 回答会启动新的 execution；后续事件必须改绑 answers 返回的 executionId。
        pendingExecutionBySessionRef.current.set(sid, {
          executionId,
          answeredToolCallId: toolCallId,
          answeredAnswers: answers,
        });
        if (isCurrentSession) {
          openStream(sid, { executionId, after: 0 });
        }
        return true;
      } catch (err: unknown) {
        // 问卷已超时 / 已回答 / 被接管：卡片留着也提交不掉，撤下失效问卷。
        // 这轮 ask_user 不会再有 tool.exec_*，需一并放开被它锁住的输入
        if (isAskUserSettled(err)) {
          if (currentSessionIdRef.current === sid) {
            h.settleAskUser(toolCallId, 'dismissed');
            // 无法从错误响应获知新 executionId，不能继续复用旧 execution 订流。
            closeStream({ force: true });
            resetStreamBinding();
          }
          return true;
        }
        // 失败原因已由请求拦截器统一提示，这里只保留失败态供卡片回滚按钮
        return false;
      }
    },
    [closeStream, h, openStream, resetStreamBinding, setState],
  );

  useEffect(
    () => () => {
      closeStream();
    },
    [closeStream],
  );

  return [
    state,
    {
      send,
      cancel,
      startSession,
      resumeStream,
      appendReplayEvents,
      prependReplayEvents,
      stageLedgerEvents,
      reset,
      loadHistory,
      prependHistory,
      answer,
    },
  ];
}
