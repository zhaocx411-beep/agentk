/**
 * 会话列表管理 + 切换 + 翻页
 */
import { useState, useEffect, useRef, useCallback } from 'react';
import { message } from '@/utils/feedback';
import { getSessions, createSession } from '../api';
import { pickResumeExecutionId } from '../utils/sessionStreamResume';
import { profileYamlToSessionProfile, tabsToSpaces } from '@/utils/profileYaml';
import { buildSessionProfile } from '@/utils/profile';
import type {
  Session,
  MessageItem,
  CreateSessionRequest,
  CapabilityCatalog,
  BaseProfile,
  SessionLedgerEvent,
} from '../types';
import { SESSIONS_PAGE_SIZE } from '../constants';
import { useChatHistoryPaging } from './useChatHistoryPaging';
import type { StartSessionOptions } from './sse/types';

interface UseSessionsOptions {
  selectedModel: string;
  selectedTools: string[];
  /** 各工具选中的模型（写入 toolDefaults.<tool>.model） */
  selectedToolModels: Record<string, string>;
  catalog: CapabilityCatalog | null;
  /** 当前生效的 system prompt */
  systemPrompt: string;
  /** 会话级 Profile 文档原文（可被 workflow 导入或 OverrideModal 覆盖） */
  overrideYaml: string;
  sseActions: {
    startSession: (sid: string, history?: SessionLedgerEvent[], options?: StartSessionOptions) => void;
    prependReplayEvents: (history: SessionLedgerEvent[]) => boolean;
    reset: () => void;
    loadHistory: (messages: MessageItem[]) => void;
    prependHistory: (messages: MessageItem[]) => void;
  };
  /** 当前是否正在生成：生成中不插入更早历史，避免打断流式现场 */
  isChatBusy?: boolean;
}

export function useSessions({
  selectedModel,
  selectedTools,
  selectedToolModels,
  catalog,
  systemPrompt,
  overrideYaml,
  sseActions,
  isChatBusy = false,
}: UseSessionsOptions) {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsHasMore, setSessionsHasMore] = useState(false);
  const [isLoadingMoreSessions, setIsLoadingMoreSessions] = useState(false);
  const historyListRef = useRef<HTMLDivElement>(null);
  /** 已加载页码：页码分页用 */
  const sessionsPageRef = useRef(0);

  // 对话区历史：进会话只拉最新一页，更早的对话由上滚分页补齐
  const historyRequestRef = useRef(0);
  const {
    isLoadingHistory: isLoadingMoreItems,
    setIsLoadingHistory: setIsLoadingMoreItems,
    loadLatest,
    reset: resetEventsPaging,
    handleChatScroll,
  } = useChatHistoryPaging({
    prependReplayEvents: sseActions.prependReplayEvents,
    isBusy: isChatBusy,
  });

  // 页面加载时获取历史会话第一页（agent 类会话；ref 去重防 StrictMode 双调）
  const hasFetchedSessionsRef = useRef(false);
  useEffect(() => {
    if (hasFetchedSessionsRef.current) return;
    hasFetchedSessionsRef.current = true;

    getSessions({ page: 1, pageSize: SESSIONS_PAGE_SIZE, sessionKind: 'agent_loop' })
      .then((res) => {
        setSessions(res.sessions);
        sessionsPageRef.current = res.page;
        setSessionsHasMore(res.hasMore);
      })
      .catch(() => {
        // 静默失败
      });
  }, []);

  // 滚动加载更多会话
  const handleHistoryScroll = useCallback(() => {
    const el = historyListRef.current;
    if (!el || isLoadingMoreSessions || !sessionsHasMore) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 20) {
      setIsLoadingMoreSessions(true);
      getSessions({
        page: sessionsPageRef.current + 1,
        pageSize: SESSIONS_PAGE_SIZE,
        sessionKind: 'agent_loop',
      })
        .then((res) => {
          if (res.sessions.length) {
            setSessions((prev) => [...prev, ...res.sessions]);
            sessionsPageRef.current = res.page;
          }
          setSessionsHasMore(res.hasMore && res.sessions.length > 0);
        })
        .catch(() => {
          // 静默失败
        })
        .finally(() => {
          setIsLoadingMoreSessions(false);
        });
    }
  }, [isLoadingMoreSessions, sessionsHasMore]);

  // 创建新会话
  // explicit：定制配置（override）应用时传入的显式选择，优先于左栏勾选状态——
  // 应用定制配置即创建会话时 React 状态尚未落地，必须以定制配置里的值为准
  const handleNewSession = useCallback(
    async (explicit?: {
      tools?: string[];
      model?: string;
      toolModels?: Record<string, string>;
      /** 显式 profile YAML：版本「加载调试」后立即建会话时 overrideYaml 状态尚未落地 */
      yaml?: string;
      /** 直接传入的 Agent-k Profile object（AgentLoop 草稿调试） */
      profile?: BaseProfile;
      /** 已发布 AgentLoop 版本 pin */
      agentLoopVersionId?: number;
      /** Workflow 版本 pin */
      workflowVersionId?: number;
    }) => {
      try {
        const body: CreateSessionRequest = {
          sessionKind: 'agent_loop',
          ...(explicit?.agentLoopVersionId
            ? { agentLoopVersionId: explicit.agentLoopVersionId }
            : {}),
          ...(explicit?.workflowVersionId ? { workflowVersionId: explicit.workflowVersionId } : {}),
        };

        // pin 模式不重复发送完整 profile；草稿或自由配置则发送严格的 Agent-k Profile object。
        const shouldSendProfile =
          !!explicit?.profile || (!explicit?.agentLoopVersionId && !explicit?.workflowVersionId);
        if (shouldSendProfile) {
          const yamlToSend = tabsToSpaces(explicit?.yaml ?? overrideYaml).trim();
          let base: BaseProfile | null = null;
          if (explicit?.profile) {
            base = explicit.profile;
          } else if (yamlToSend) {
            base = profileYamlToSessionProfile(yamlToSend, catalog);
          }
          // 传入完整 profile（草稿调试）时以该文档自身的 system / tools / 模型为准，
          // 不拿左栏状态去覆盖它
          const usingExplicitProfile = !!explicit?.profile;
          body.profile = buildSessionProfile({
            catalog,
            base,
            system: usingExplicitProfile ? undefined : systemPrompt,
            defaultModel: usingExplicitProfile
              ? undefined
              : (explicit?.model ?? selectedModel),
            toolNames: explicit?.tools ?? (usingExplicitProfile ? undefined : selectedTools),
            toolModels: usingExplicitProfile
              ? undefined
              : (explicit?.toolModels ?? selectedToolModels),
          });
        }

        const sess = await createSession(body);
        historyRequestRef.current += 1;
        resetEventsPaging(sess.id);
        setSessionId(sess.id);
        setSessions((prev) => [{ id: sess.id, title: sess.title || '新对话' }, ...prev]);
        sseActions.startSession(sess.id);
        return sess.id;
      } catch (error: unknown) {
        message.error(error instanceof Error ? error.message : '创建会话失败');
        return null;
      }
    },
    [
      selectedModel,
      selectedTools,
      selectedToolModels,
      catalog,
      systemPrompt,
      overrideYaml,
      sseActions,
      resetEventsPaging,
    ],
  );

  // 切换到历史会话：只回放最新一页，更早的对话等用户上滚再补
  const handleSwitchSession = useCallback(
    async (sid: string, meta?: Session) => {
      if (sid === sessionId) return;
      const requestId = historyRequestRef.current + 1;
      historyRequestRef.current = requestId;
      setSessionId(sid);
      setIsLoadingMoreItems(true);
      const sessionMeta = meta ?? sessions.find((item) => item.id === sid);
      try {
        const bootstrap = await loadLatest(sid);
        if (historyRequestRef.current !== requestId) return;
        const { events } = bootstrap;
        const resumeExecutionId = pickResumeExecutionId(sessionMeta, events);
        sseActions.startSession(sid, events, { resumeExecutionId: resumeExecutionId || undefined });
        message.info('已切换到历史会话');
      } catch {
        if (historyRequestRef.current !== requestId) return;
        // 历史接口异常时仍尝试续流或空会话回放，避免整个会话不可用。
        const resumeExecutionId = pickResumeExecutionId(sessionMeta);
        sseActions.startSession(sid, undefined, { resumeExecutionId: resumeExecutionId || undefined });
        message.warning('历史账本加载失败，已切换为流式回放');
      } finally {
        if (historyRequestRef.current === requestId) setIsLoadingMoreItems(false);
      }
    },
    [sessionId, sessions, sseActions, loadLatest, setIsLoadingMoreItems],
  );

  const updateSessionTitle = useCallback((sessionId: string, title: string) => {
    setSessions((prev) =>
      prev.map((item) => (item.id === sessionId ? { ...item, title } : item)),
    );
  }, []);

  return {
    sessionId,
    setSessionId,
    sessions,
    sessionsHasMore,
    isLoadingMoreSessions,
    historyListRef,
    isLoadingMoreItems,
    handleHistoryScroll,
    handleNewSession,
    handleSwitchSession,
    updateSessionTitle,
    handleChatScroll,
  };
}
