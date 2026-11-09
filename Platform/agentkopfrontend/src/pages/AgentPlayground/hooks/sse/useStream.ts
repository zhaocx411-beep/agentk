/**
 * Session 级 SSE 连接管理 Hook
 * 每次用户发送消息后建立一次会话流；下一次发送、切换会话或卸载时主动断开。
 * `stream.end` / 连接断开后会 catchUp 账本并无感重连，直到确认无活跃 round/job。
 */
import { useCallback, useRef } from 'react';
import { message } from '@/utils/feedback';
import { getSessionEvents } from '../../api';
import type { RunEvent } from '../../types';
import {
  isSessionDeltaSseEvent,
  parseSSEChunk,
  parseSessionDeltaPayload,
  resolveEventPayload,
} from './helpers';
import type { SSEStateHandle } from './useSSEState';
import { SESSION_EVENTS_PAGE_SIZE } from '../../constants';
import { EXECUTION_BIND_GRACE_MS } from '@/utils/executionGrace';
import { comparePlatformSessionLedgerEvents } from '../../utils/sessionLedgerOrder';

interface SessionStreamEnvelope {
  id?: number;
  eventId?: string;
  executionId?: string;
  seq?: number;
  roundId?: string;
  runId?: string;
  kind?: string;
  data?: unknown;
  createdAt?: string;
}

export function useStream(h: SSEStateHandle) {
  const {
    setState,
    lastSeqRef,
    abortRef,
    streamRetryTimerRef,
    streamGenerationRef,
    bindGraceUntilRef,
    pendingJobsRef,
    pendingToolCallIdsRef,
    cancelPendingRef,
    closeStream,
    addLedger,
    applyRunEvent,
    applySessionDelta,
    finalizeStreamEnd,
    beginExecutionFollow,
  } = h;
  const sessionSeqRef = useRef(0);
  const executionIdRef = useRef('');

  const setStreamCursor = useCallback((after: number) => {
    sessionSeqRef.current = Math.max(0, after);
  }, []);

  const resetStreamBinding = useCallback(() => {
    sessionSeqRef.current = 0;
    executionIdRef.current = '';
  }, []);

  const buildStreamUrl = useCallback((sessionId: string, executionId: string, after: number) => {
    const apiPrefix = import.meta.env.DEV ? '/api' : '';
    const base = `${apiPrefix}/agent/operation/backend/agentk/sessions/${sessionId}/stream`;
    const params = new URLSearchParams({ executionId });
    if (after > 0) params.set('after', String(after));
    return `${base}?${params.toString()}`;
  }, []);

  /** 为当前用户消息建立 SSE；异常断开后按需重连，主动 close / 空闲 204 时停 */
  const openStream = useCallback(
    (sessionId: string, options?: { after?: number; executionId?: string }) => {
      closeStream();
      const streamGeneration = streamGenerationRef.current;
      const isActiveStream = () => streamGenerationRef.current === streamGeneration;
      const nextExecutionId = options?.executionId?.trim();
      const executionChanged = Boolean(
        nextExecutionId && nextExecutionId !== executionIdRef.current,
      );
      const cursor = Math.max(0, options?.after ?? (executionChanged ? 0 : lastSeqRef.current));
      setStreamCursor(cursor);
      // seq 仅在 execution 内递增；新 execution 从 1 重新开始，不能沿用上一轮游标去重。
      if (executionChanged || options?.after !== undefined) {
        lastSeqRef.current = cursor;
      }
      if (nextExecutionId) {
        executionIdRef.current = nextExecutionId;
        bindGraceUntilRef.current = Date.now() + EXECUTION_BIND_GRACE_MS;
        beginExecutionFollow();
      }
      if (!executionIdRef.current.trim()) return;

      const retryInterval = 1000;
      /** 等后台任务唤醒时的慢速间隔 */
      const idleInterval = 5000;
      /** 204 / 断线重连上限，避免终态丢失后永久刷屏 */
      const maxWaitingJobAttempts = 120; // 120 * 5s ≈ 10 分钟
      /**
       * 网关 / 上游常在 30–60s 无数据后掐 SSE，TCP 不一定立刻断开。
       * 客户端空闲看门狗主动 abort，才能走进重连；取值略低于常见 60s 超时。
       */
      const streamIdleMs = 35_000;
      /** 单次订流生命周期内，无新账本进度的断流重连上限，防止 stream.end 抖死循环 */
      const maxQuietReconnects = 60;
      let quietReconnects = 0;
      let lastProgressSeq = sessionSeqRef.current;
      let executionFinished = false;
      const seenLedgerSeq = new Set<number>();

      const markExecutionFinished = () => {
        executionFinished = true;
        bindGraceUntilRef.current = 0;
      };

      /** execution 未 finished 前持续订流（含 tool 间隙、模型思考、keepalive 空窗） */
      const shouldReconnect = () =>
        isActiveStream() && !executionFinished && !cancelPendingRef.current;

      const markStreamProgress = () => {
        if (sessionSeqRef.current > lastProgressSeq) {
          lastProgressSeq = sessionSeqRef.current;
          quietReconnects = 0;
        }
      };

      const rememberLedgerSeq = (seq: number) => {
        if (seq <= 0) return false;
        if (seenLedgerSeq.has(seq)) return true;
        seenLedgerSeq.add(seq);
        sessionSeqRef.current = Math.max(sessionSeqRef.current, seq);
        lastSeqRef.current = Math.max(lastSeqRef.current, seq);
        markStreamProgress();
        return false;
      };

      const finishStreamFollow = () => {
        if (!isActiveStream() || cancelPendingRef.current) return;
        settleFollow();
      };

      /**
       * 对齐 demoui catchUpEvents：流断开后先补齐 /events 最终事实，
       * 再决定是无感重连续流，还是确认空闲后收尾。
       */
      const catchUpSessionEvents = async () => {
        if (!isActiveStream()) return;
        const cursor = sessionSeqRef.current;
        const collected: Array<{
          seq: number;
          kind: string;
          payload: unknown;
          createdAt: string;
          roundId?: string;
          eventId?: string;
          executionId?: string;
        }> = [];
        // 接口按 seq 倒序：从第 1 页（最新）往更早翻，直到盖过当前游标
        // 最多翻几页，避免异常 hasMore 把重连拖死
        for (let page = 1; page <= 160; page += 1) {
          if (!isActiveStream() || cancelPendingRef.current) return;
          const res = await getSessionEvents(sessionId, {
            page,
            pageSize: SESSION_EVENTS_PAGE_SIZE,
          });
          const events = res.events || [];
          if (events.length === 0) break;
          const boundExecutionId = executionIdRef.current.trim();
          const executionEvents = events.filter(
            (event) => !event.executionId || event.executionId.trim() === boundExecutionId,
          );

          for (const ev of executionEvents) {
            if (ev.seq <= cursor) continue;
            collected.push({
              seq: ev.seq,
              kind: ev.kind,
              payload: resolveEventPayload(ev),
              createdAt: ev.createdAt || new Date().toISOString(),
              roundId: ev.roundId,
              eventId: ev.eventId,
              executionId: ev.executionId || boundExecutionId,
            });
          }

          const minSeq =
            executionEvents.length > 0
              ? Math.min(...executionEvents.map((ev) => ev.seq))
              : Number.POSITIVE_INFINITY;
          // 本页最旧事件已不新于游标，后面更旧，无需再翻
          if (minSeq <= cursor || !res.hasMore) break;
        }

        collected.sort(comparePlatformSessionLedgerEvents);
        for (const ev of collected) {
          if (!isActiveStream() || cancelPendingRef.current) return;
          if (rememberLedgerSeq(ev.seq)) continue;
          applyRunEvent(
            {
              seq: ev.seq,
              kind: ev.kind,
              payload: ev.payload,
              createdAt: ev.createdAt,
              executionId: ev.executionId,
              eventId: ev.eventId,
            },
            { runId: ev.roundId, skipDedupe: true },
          );
        }
      };

      /** 确认跟随时段结束：落盘残留流式内容并放开输入（含 stuck round 强收尾） */
      const settleFollow = () => {
        if (!isActiveStream() || cancelPendingRef.current) return;
        finalizeStreamEnd(sessionSeqRef.current);
      };

      const scheduleReconnect = (attempt: number, delay = retryInterval) => {
        if (!isActiveStream()) return;
        if (!shouldReconnect()) {
          finishStreamFollow();
          return;
        }
        if (sessionSeqRef.current <= lastProgressSeq) {
          quietReconnects += 1;
          if (quietReconnects > maxQuietReconnects) {
            settleFollow();
            return;
          }
        } else {
          markStreamProgress();
        }
        // stream.end / 断线后即使 runningRound 暂未清掉也要续连；
        // 真正空闲由 204 耗尽 + settleFollow 收尾。
        streamRetryTimerRef.current = setTimeout(() => void connect(attempt + 1), delay);
      };

      const connect = async (attempt: number, idleAttempt = 0): Promise<void> => {
        if (!isActiveStream()) return;
        const executionId = executionIdRef.current.trim();
        if (!executionId) {
          abortRef.current = null;
          setState((prev) => ({
            ...prev,
            error: '缺少 executionId，无法订阅实时流',
          }));
          settleFollow();
          return;
        }

        const ac = new AbortController();
        abortRef.current = ac;
        // fetch 不像 EventSource 会自动发送 Last-Event-ID，重连时必须显式携带账本游标。
        // delta 没有账本 seq，不参与断线续传；最终内容由 assistant.* 账本事件兜底。
        const streamUrl = buildStreamUrl(sessionId, executionId, sessionSeqRef.current);

        let response: Response;
        try {
          response = await fetch(streamUrl, {
            method: 'GET',
            headers: { Accept: 'text/event-stream' },
            credentials: 'include',
            signal: ac.signal,
          });
        } catch {
          if (!isActiveStream()) return;
          if (!ac.signal.aborted && !cancelPendingRef.current) {
            try {
              await catchUpSessionEvents();
            } catch {
              // 瞬时失败，交给重连
            }
            if (shouldReconnect()) scheduleReconnect(attempt);
            else finishStreamFollow();
          }
          return;
        }

        // 204 = 当前 execution 暂无 body；execution 未结束时可短轮询，结束后直接收尾
        if (response.status === 204) {
          if (!isActiveStream()) return;
          if (abortRef.current === ac) abortRef.current = null;
          if (cancelPendingRef.current) return;
          try {
            await catchUpSessionEvents();
          } catch {
            // 账本补齐失败不阻断空闲探测
          }
          if (cancelPendingRef.current) return;
          if (executionFinished) {
            finishStreamFollow();
            return;
          }
          const delay =
            pendingJobsRef.current.size > 0 || pendingToolCallIdsRef.current.size > 0
              ? idleInterval
              : retryInterval;
          if (idleAttempt >= maxWaitingJobAttempts) {
            finishStreamFollow();
            return;
          }
          streamRetryTimerRef.current = setTimeout(
            () => void connect(attempt, idleAttempt + 1),
            delay,
          );
          return;
        }

        if (!response.ok) {
          if (!isActiveStream()) return;
          // 5xx：服务端瞬时异常，仅在仍需跟随时重试
          if (response.status >= 500) {
            scheduleReconnect(attempt);
            return;
          }
          // 404：新建会话的 stream 端点就绪前有短暂 404（queued → running 延迟），
          // 只保留有限次重试；会话不存在（如缺失 agentkSessionId 的历史会话）
          // 时避免无限轮询
          if (response.status === 404 && attempt < 5) {
            scheduleReconnect(attempt);
            return;
          }
          abortRef.current = null;
          const error =
            response.status === 404
              ? '会话不存在或已过期，无法建立流连接'
              : `SSE 连接失败：HTTP ${response.status}`;
          setState((prev) => ({ ...prev, error }));
          message.error(error);
          settleFollow();
          return;
        }

        // 续连成功不刷用户可见提示；仅记入执行详情，便于排查
        if (attempt > 0) addLedger(0, 'stream', `会话流已续连（第 ${attempt + 1} 次）`);
        const reader = response.body?.getReader();
        if (!reader) {
          if (abortRef.current === ac) abortRef.current = null;
          scheduleReconnect(attempt);
          return;
        }

        const decoder = new TextDecoder();
        let buffer = '';

        const handleEvent = (id: string, sseEventName: string, rawData: string) => {
          if (!isActiveStream()) return;
          try {
            // delta 是独立的瞬态帧：其 kind 是块类型，而不是 SSE 事件名。
            // 不能把 delta.id 当成 ledger seq，否则重连去重会误丢正式事件。
            if (sseEventName === 'ping' || sseEventName === 'heartbeat') {
              quietReconnects = 0;
              return;
            }

            if (isSessionDeltaSseEvent(sseEventName)) {
              const delta = parseSessionDeltaPayload(sseEventName, rawData);
              if (delta) {
                quietReconnects = 0;
                applySessionDelta(delta);
              }
              return;
            }

            if (sseEventName === 'execution.finished') {
              markExecutionFinished();
            }

            // stream.end：连接生命周期信号，不推进账本游标、不收尾 UI
            if (sseEventName === 'stream.end') {
              addLedger(0, 'stream.end', '实时流关闭，准备续连');
              return;
            }

            const envelope = JSON.parse(rawData) as SessionStreamEnvelope;
            const eventName = envelope.kind || sseEventName;
            const envelopeExecutionId = envelope.executionId?.trim();
            // URL 已绑定 execution；服务端若返回其他 execution 的正式事件，不能混入当前时间线。
            if (envelopeExecutionId && envelopeExecutionId !== executionId) return;

            // 部分网关把 delta 包在 message 帧里，kind 在 payload；勿把 delta.id 当账本 seq
            if (eventName === 'reasoning.delta' || eventName === 'text.delta') {
              const delta = parseSessionDeltaPayload(eventName, rawData, envelope.roundId);
              if (delta) {
                quietReconnects = 0;
                applySessionDelta(delta);
              }
              return;
            }

            const seq = envelope.seq ?? envelope.id ?? (Number.parseInt(id, 10) || 0);
            // after 在部分网关不生效，可能重新返回低 id；正式事件依靠 eventId 去重。
            // 不能按 lastSeqRef 跳过，否则切回会话时会漏掉用于清除旧问卷的 user.input。
            if (rememberLedgerSeq(seq)) return;

            if (eventName === 'execution.finished') {
              markExecutionFinished();
            }

            if (eventName === 'stream.end') {
              addLedger(seq, 'stream.end', '实时流关闭，准备续连');
              return;
            }

            const payload = resolveEventPayload(envelope);
            const payloadRecord =
              payload && typeof payload === 'object' && !Array.isArray(payload)
                ? (payload as Record<string, unknown>)
                : {};

            const createdAt =
              envelope.createdAt ||
              (typeof payloadRecord.ts === 'number'
                ? new Date(payloadRecord.ts).toISOString()
                : new Date().toISOString());
            const event: RunEvent = {
              seq,
              kind: eventName,
              payload,
              createdAt,
              executionId: envelopeExecutionId || executionId,
              eventId: envelope.eventId,
            };
            applyRunEvent(event, {
              runId: envelope.runId || envelope.roundId,
              skipDedupe: true,
            });
          } catch {
            // ping/comment 或非 JSON 数据无需进入业务状态机
          }
        };

        let idleExpired = false;
        let idleTimer: ReturnType<typeof setTimeout> | undefined;
        const clearIdleWatch = () => {
          if (idleTimer !== undefined) {
            clearTimeout(idleTimer);
            idleTimer = undefined;
          }
        };
        const bumpIdleWatch = () => {
          clearIdleWatch();
          idleTimer = setTimeout(() => {
            if (!isActiveStream()) return;
            idleExpired = true;
            addLedger(0, 'stream', '实时流空闲超时，准备续连');
            ac.abort();
          }, streamIdleMs);
        };

        try {
          bumpIdleWatch();
          // session stream 是长连接；stream.end、空闲超时或对端关连接后进入续连
          // eslint-disable-next-line no-constant-condition
          while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            bumpIdleWatch();
            buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
            const parts = buffer.split('\n\n');
            buffer = parts.pop() || '';
            for (const part of parts) parseSSEChunk(part, handleEvent);
          }
          // 流自动关闭：execution 已结束则收尾，否则仅在仍需跟随时续连
          if (!isActiveStream()) return;
          if (!cancelPendingRef.current && (!ac.signal.aborted || idleExpired)) {
            if (!executionFinished) {
              try {
                await catchUpSessionEvents();
              } catch {
                // 补齐失败仍尝试续连
              }
            }
            if (shouldReconnect()) scheduleReconnect(attempt, retryInterval);
            else finishStreamFollow();
          }
        } catch {
          if (!isActiveStream()) return;
          if (!cancelPendingRef.current && (!ac.signal.aborted || idleExpired)) {
            if (!executionFinished) {
              try {
                await catchUpSessionEvents();
              } catch {
                // ignore
              }
            }
            if (shouldReconnect()) scheduleReconnect(attempt);
            else finishStreamFollow();
          }
        } finally {
          clearIdleWatch();
          if (abortRef.current === ac) abortRef.current = null;
        }
      };

      void connect(0);
    },
    [
      abortRef,
      addLedger,
      applyRunEvent,
      applySessionDelta,
      bindGraceUntilRef,
      buildStreamUrl,
      cancelPendingRef,
      closeStream,
      finalizeStreamEnd,
      pendingToolCallIdsRef,
      beginExecutionFollow,
      lastSeqRef,
      pendingJobsRef,
      setStreamCursor,
      setState,
      streamGenerationRef,
      streamRetryTimerRef,
    ],
  );

  return { openStream, setStreamCursor, resetStreamBinding };
}
