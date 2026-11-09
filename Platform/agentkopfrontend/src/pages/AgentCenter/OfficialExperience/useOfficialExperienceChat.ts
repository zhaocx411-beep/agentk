/**
 * 官方体验对话：复用 Playground SSE / 账本，按自主规划或工作流过滤历史
 * 占位 session（立即使用先进页）：首次发消息时才创建 quick session
 */
import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { message } from '@/utils/feedback';
import { useSSE } from '@/pages/AgentPlayground/hooks/useSSE';
import type { MessageAttachment, Session } from '@/pages/AgentPlayground/types';
import type { SessionLedgerBootstrap } from '@/pages/AgentPlayground/utils';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { hasAgentkLedger } from '../AgentDetail/lib/configCompare';
import { useChatHistoryPaging } from '@/pages/AgentPlayground/hooks/useChatHistoryPaging';
import { useSessionListPagination } from '../AgentDetail/hooks/useSessionListPagination';
import { assertOutboundMediaWithinLimit } from '@/services/mediaSizeGuard';
import { createOfficialLoopQuickSession, createOfficialWorkflowQuickSession } from '../api';
import {
  buildOfficialChatPath,
  buildOfficialLaunchChatPath,
  isOfficialDocLoop,
  isOfficialPendingSessionId,
  isOfficialTtsLoop,
  isOfficialVirtualTryOnLoop,
} from '../utils';
import { fetchOfficialSessionsPage } from './officialSessions';
import { savePendingFirstMessage, takePendingFirstMessage } from './pendingFirstMessage';
import { pickResumeExecutionId } from '@/pages/AgentPlayground/utils/sessionStreamResume';
import {
  commitSessionLedgerBootstrap,
  isLedgerComplete,
  pickOlderEventsFromLedgerCache,
  readOfficialSessionLedgerCache,
  seedOfficialSessionLedgerCache,
} from './OfficialFreeChat/manga/useOfficialSessionLedger';
import type { SessionLedgerEvent } from '@/pages/AgentPlayground/types';
import { withDocParseLoopSendUrl } from '@/services/media';
import { useTtsComposer } from './OfficialFreeChat/tts/useTtsComposer';
import { useVirtualTryOnComposer } from './OfficialFreeChat/virtualTryOn/useVirtualTryOnComposer';
import { supportsStructuredMessageAttachments } from './structuredAttachments';

interface UseOfficialExperienceChatOptions {
  sessionId: string;
  item: OfficialCatalogCardData | null;
  /** 是否拉取历史会话列表；创作工作台不展示历史，可关掉省一次请求 */
  loadSessions?: boolean;
}

function sessionsCacheKey(item: OfficialCatalogCardData) {
  return `${item.variant}:${item.agentLoopId ?? ''}:${item.workflowId ?? ''}`;
}

/** 跨组件 remount 递增，避免 StrictMode 下过期 loadSession 清空已发送的用户气泡 */
let officialSessionLoadSeq = 0;

export function useOfficialExperienceChat({
  sessionId,
  item,
  loadSessions = true,
}: UseOfficialExperienceChatOptions) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [sseState, sseActions] = useSSE({
    enableStructuredAttachments: supportsStructuredMessageAttachments(item),
  });
  const [inputText, setInputText] = useState('');
  const [isSending, setIsSending] = useState(false);
  const inputTextRef = useRef(inputText);
  inputTextRef.current = inputText;
  const chatRef = useRef<HTMLDivElement>(null!);
  const sendingRef = useRef(false);
  const streamScrollRef = useRef<HTMLDivElement | null>(
    null,
  ) as MutableRefObject<HTMLDivElement | null>;
  // 进会话只回放最新一页，更早的对话由上滚分页补齐
  const resolveOlderEvents = useCallback(
    (sid: string, seenEventKeys: ReadonlySet<string>) => {
      const cached = readOfficialSessionLedgerCache(queryClient, sid);
      return pickOlderEventsFromLedgerCache(cached, seenEventKeys);
    },
    [queryClient],
  );

  const syncLedgerPage = useCallback(
    (sid: string, partial: SessionLedgerBootstrap) => {
      seedOfficialSessionLedgerCache(queryClient, sid, partial);
    },
    [queryClient],
  );

  const {
    isLoadingHistory,
    setIsLoadingHistory,
    loadLatest,
    reset: resetHistoryPaging,
    handleChatScroll,
  } = useChatHistoryPaging({
    prependReplayEvents: sseActions.prependReplayEvents,
    isBusy: sseState.isRunning,
    resolveOlderEvents,
    onEventsPageLoaded: syncLedgerPage,
  });
  const historyRequestRef = useRef(0);
  const loadedSessionRef = useRef<string | null>(null);
  const pendingSendLockRef = useRef(false);
  const loadGenRef = useRef(0);
  const pendingSessionMetaRef = useRef<Session | undefined>(undefined);
  const pending = isOfficialPendingSessionId(sessionId);
  const needsTts = item != null && isOfficialTtsLoop(item);
  const needsDoc = item != null && isOfficialDocLoop(item);
  const needsVirtualTryOn = item != null && isOfficialVirtualTryOnLoop(item);
  const ttsComposer = useTtsComposer(needsTts);
  const virtualTryOnComposer = useVirtualTryOnComposer(needsVirtualTryOn);

  const fetchPage = useCallback(
    (page: number) => {
      if (!item) {
        return Promise.resolve({ sessions: [], hasMore: false, page: 1, pageSize: 20 });
      }
      return fetchOfficialSessionsPage(item, page);
    },
    [item],
  );

  const {
    sessions: sessionRecords,
    sessionsLoading,
    sessionsHasMore,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    refreshSessions,
    invalidateLoadedKey,
    updateSessionTitle,
  } = useSessionListPagination({
    fetchPage,
    resetKey: item ? sessionsCacheKey(item) : null,
    enabled: !!item && loadSessions,
  });

  const sessions = useMemo(() => {
    const list = sessionRecords.filter(hasAgentkLedger);
    if (
      pending ||
      !sessionId.startsWith('ses_') ||
      list.some((session) => session.id === sessionId)
    ) {
      return list;
    }
    return [{ id: sessionId, title: '当前对话' }, ...list];
  }, [sessionRecords, sessionId, pending]);

  const loadSession = useCallback(
    async (sid: string, meta?: Session) => {
      if (isOfficialPendingSessionId(sid)) {
        sseActions.reset();
        resetHistoryPaging(null);
        setIsLoadingHistory(false);
        return;
      }
      if (!sid.startsWith('ses_')) {
        message.warning('该会话无有效对话记录');
        return;
      }
      const requestId = historyRequestRef.current + 1;
      historyRequestRef.current = requestId;
      // 全局序号：组件 remount 后 ref 会重置，仅靠 requestId 无法识别过期请求
      const loadGen = ++officialSessionLoadSeq;
      loadGenRef.current = loadGen;
      setIsLoadingHistory(true);
      const isStale = () =>
        historyRequestRef.current !== requestId || loadGenRef.current !== loadGen;
      const sessionMeta = meta ?? sessionRecords.find((item) => item.id === sid);
      try {
        // 工作流体验走 pipeline-runs，不依赖会话账本；进页/切换都不必拉 events
        if (item?.variant === 'workflow') {
          if (isStale()) return;
          resetHistoryPaging(null);
          // 等账本/会话就绪再清空，避免 fetch 窗口把已发出的用户气泡冲掉
          sseActions.startSession(sid);
          return;
        }
        const firstPageSeqRef = { current: new Set<number>() };
        let latestPageEventsRef: SessionLedgerEvent[] = [];
        let painted = false;

        const paintSessionUi = (
          replay: SessionLedgerBootstrap,
          resumeEvents: SessionLedgerEvent[] = replay.events,
        ) => {
          if (isStale() || painted) return;
          painted = true;
          firstPageSeqRef.current = new Set(replay.events.map((event) => event.seq));
          // 续订 stream 只看第 1 页（最新），判断是否 execution.finished
          const resumeExecutionId = pickResumeExecutionId(sessionMeta, resumeEvents);
          sseActions.startSession(sid, replay.events, {
            resumeExecutionId: resumeExecutionId || undefined,
          });
          setIsLoadingHistory(false);
        };
        const warmLedger = readOfficialSessionLedgerCache(queryClient, sid);
        const warmComplete = isLedgerComplete(warmLedger);

        const bootstrap = await loadLatest(sid, {
          maxBootstrapPages: warmComplete ? 1 : undefined,
          adoptWarmLedger: warmComplete ? warmLedger : undefined,
          onFirstPage: (partial) => {
            latestPageEventsRef = partial.events;
            seedOfficialSessionLedgerCache(queryClient, sid, partial);
            if (!warmComplete) paintSessionUi(partial, partial.events);
          },
          onPageLoaded: warmComplete
            ? undefined
            : (partial) => {
                seedOfficialSessionLedgerCache(queryClient, sid, partial);
              },
        });

        latestPageEventsRef = warmComplete
          ? bootstrap.events
          : latestPageEventsRef.length > 0
            ? latestPageEventsRef
            : bootstrap.events;

        if (bootstrap.pagesLoaded > 0) {
          commitSessionLedgerBootstrap(queryClient, sid, bootstrap, {
            skipBackfill: warmComplete,
          });
        }
        if (isStale()) return;

        if (warmComplete) {
          const full = readOfficialSessionLedgerCache(queryClient, sid);
          if (full) {
            paintSessionUi(
              { events: full.events, total: full.total, pagesLoaded: full.pagesLoaded },
              latestPageEventsRef,
            );
          }
        } else if (!painted) {
          paintSessionUi(bootstrap);
        } else {
          const older = bootstrap.events.filter((event) => !firstPageSeqRef.current.has(event.seq));
          if (older.length > 0) {
            const merged = sseActions.prependReplayEvents(older);
            if (!merged) sseActions.stageLedgerEvents(older);
          }
        }
      } catch {
        if (isStale()) return;
        const resumeExecutionId = pickResumeExecutionId(sessionMeta);
        sseActions.startSession(sid, undefined, {
          resumeExecutionId: resumeExecutionId || undefined,
        });
      } finally {
        if (!isStale()) setIsLoadingHistory(false);
      }
    },
    [
      sseActions,
      item?.variant,
      loadLatest,
      resetHistoryPaging,
      setIsLoadingHistory,
      sessionRecords,
      queryClient,
    ],
  );

  useEffect(() => {
    if (loadedSessionRef.current === sessionId) return;
    loadedSessionRef.current = sessionId;
    setInputText('');
    if (needsVirtualTryOn) virtualTryOnComposer.clear();
    const meta =
      pendingSessionMetaRef.current ?? sessionRecords.find((item) => item.id === sessionId);
    pendingSessionMetaRef.current = undefined;
    void loadSession(sessionId, meta);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  // 跳转后的首条待发消息：等账本 / startSession 完成再 send
  useEffect(() => {
    if (pending || isLoadingHistory || pendingSendLockRef.current) return;
    if (!sessionId.startsWith('ses_')) return;
    const pendingMsg = takePendingFirstMessage(sessionId);
    if (!pendingMsg) return;
    pendingSendLockRef.current = true;
    setIsSending(true);
    void (async () => {
      try {
        await sseActions.send(
          sessionId,
          pendingMsg.text,
          pendingMsg.attachments?.map((file) => (needsDoc ? withDocParseLoopSendUrl(file) : file)),
          {
            ...(pendingMsg.tts ? { tts: pendingMsg.tts } : {}),
            ...(pendingMsg.userInput ? { userInput: pendingMsg.userInput } : {}),
          },
        );
        refreshSessions();
      } catch (error) {
        message.error(error instanceof Error ? error.message : '发送失败，请稍后重试');
      } finally {
        pendingSendLockRef.current = false;
        setIsSending(false);
      }
    })();
  }, [pending, isLoadingHistory, sessionId, sseActions, refreshSessions, needsDoc]);

  const handleSend = useCallback(
    async (attachments?: MessageAttachment[]): Promise<boolean> => {
      const text = inputTextRef.current.trim();
      const files = (attachments?.filter((a) => a.url.trim()) ?? []).map((file) =>
        needsDoc ? withDocParseLoopSendUrl(file) : file,
      );
      // 生成中不再拦截：sseActions.send 会先打断上一轮再开新的
      if (!text && files.length === 0 && !needsVirtualTryOn) return false;

      if (needsTts && !ttsComposer.ttsOptions) {
        message.warning('请选择语音引擎与音色');
        return false;
      }
      if (needsVirtualTryOn) {
        if (!text) {
          message.warning('请填写试穿描述');
          return false;
        }
        if (!virtualTryOnComposer.isReady) {
          message.warning('请至少上传 1 张商品图');
          return false;
        }
        if (virtualTryOnComposer.isUploading) {
          message.warning('图片上传中，请稍候');
          return false;
        }
      }
      if (sendingRef.current) return false;

      try {
        await assertOutboundMediaWithinLimit({
          attachments: files,
          payload: virtualTryOnComposer.userInput,
        });
      } catch (error) {
        message.error(error instanceof Error ? error.message : '素材超过 500MB 上限');
        return false;
      }

      const sendOptions = {
        ...(ttsComposer.ttsOptions ? { tts: ttsComposer.ttsOptions } : {}),
        ...(virtualTryOnComposer.sendOptions ?? {}),
      };
      const hasSendOptions = Object.keys(sendOptions).length > 0 ? sendOptions : undefined;
      sendingRef.current = true;
      setIsSending(true);

      try {
        // 占位进页：首次发言时再建会话
        if (pending) {
          if (!item) {
            message.error('未找到该官方 Agent');
            return false;
          }
          if (item.variant === 'workflow') {
            if (!item.workflowKey) {
              message.error('缺少 workflowKey，无法开启体验');
              return false;
            }
            setInputText('');
            try {
              const session = await createOfficialWorkflowQuickSession(item.workflowKey);
              savePendingFirstMessage(
                session.id,
                text,
                files,
                ttsComposer.ttsOptions,
                virtualTryOnComposer.userInput,
              );
              loadedSessionRef.current = null;
              navigate(buildOfficialChatPath(session.id, item), { replace: true });
              return true;
            } catch (error) {
              setInputText(text);
              message.error(error instanceof Error ? error.message : '开启体验失败，请稍后重试');
              return false;
            }
          }
          if (item.agentLoopId == null) {
            message.error('缺少 agentLoopId，无法开启体验');
            return false;
          }
          setInputText('');
          try {
            const session = await createOfficialLoopQuickSession(item.agentLoopId);
            savePendingFirstMessage(
              session.id,
              text,
              files,
              ttsComposer.ttsOptions,
              virtualTryOnComposer.userInput,
            );
            loadedSessionRef.current = null;
            navigate(buildOfficialChatPath(session.id, item), { replace: true });
            return true;
          } catch (error) {
            setInputText(text);
            message.error(error instanceof Error ? error.message : '开启体验失败，请稍后重试');
            return false;
          }
        }

        const ok = await sseActions.send(
          sessionId,
          text,
          files.length > 0 ? files : undefined,
          hasSendOptions,
        );
        if (ok) {
          setInputText('');
          refreshSessions();
        }
        return ok;
      } finally {
        sendingRef.current = false;
        setIsSending(false);
      }
    },
    [
      sseActions,
      sessionId,
      refreshSessions,
      pending,
      item,
      navigate,
      needsTts,
      needsDoc,
      needsVirtualTryOn,
      ttsComposer.ttsOptions,
      virtualTryOnComposer,
    ],
  );

  /** 新建对话：回到占位页，等真正发言再创建会话 */
  const handleNewChat = useCallback(() => {
    if (!item) return;
    invalidateLoadedKey();
    setInputText('');
    if (needsVirtualTryOn) virtualTryOnComposer.clear();
    loadedSessionRef.current = null;
    resetHistoryPaging(null);
    sseActions.reset();
    navigate(buildOfficialLaunchChatPath(item), { replace: true });
  }, [
    item,
    navigate,
    invalidateLoadedKey,
    sseActions,
    resetHistoryPaging,
    needsVirtualTryOn,
    virtualTryOnComposer,
  ]);

  const handleSwitchSession = useCallback(
    (sid: string, meta?: Session) => {
      if (!item || sid === sessionId) return;
      setInputText('');
      if (needsVirtualTryOn) virtualTryOnComposer.clear();
      pendingSessionMetaRef.current = meta;
      // 先进入加载态，避免路由已变但账本未回时用空 SSE 误标「成功」
      setIsLoadingHistory(true);
      loadedSessionRef.current = null;
      navigate(buildOfficialChatPath(sid, item), { replace: true });
    },
    [item, navigate, sessionId, setIsLoadingHistory, needsVirtualTryOn, virtualTryOnComposer],
  );

  return {
    sseState,
    sseActions,
    inputText,
    setInputText,
    handleSend,
    isSending,
    handleNewChat,
    handleSwitchSession,
    sessions,
    sessionsLoading,
    sessionsHasMore,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    refreshSessions,
    updateSessionTitle,
    isLoadingHistory,
    chatRef,
    handleChatScroll,
    /** composer 模式下对话时间线隐藏，历史翻页绑在右侧「生成过程」滚动区 */
    streamScrollRef,
    handleStreamScroll: () => handleChatScroll(streamScrollRef),
    needsTts,
    needsDoc,
    needsVirtualTryOn,
    ttsComposer,
    virtualTryOnComposer,
  };
}
