/**
 * usePreviewChat - 详情页右侧嵌入式对话
 * - 从我的 Agent 进入：按 agentLoopId 加载最近历史会话
 * - 新配置 / 无会话 / 配置有改动：直接建新会话发送，不弹确认窗
 * - 脏配置对话后自动存草稿
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { message } from '@/utils/feedback';
import { createSession } from '@/services/workflow';
import { getSessions } from '@/pages/AgentPlayground/api';
import { useSSE } from '@/pages/AgentPlayground/hooks/useSSE';
import { pickResumeExecutionId } from '@/pages/AgentPlayground/utils/sessionStreamResume';
import type {
  BaseProfile,
  CapabilityCatalog,
  MessageAttachment,
  ModelCatalog,
  Session,
} from '@/pages/AgentPlayground/types';
import type { OfficialToolItem } from '@/types/officialTools';
import { sanitizeProfile, toToolRefs } from '@/utils/profile';
import { structuredAttachmentTypesForModel } from '@/utils/models';
import { hasAgentkLedger } from '../lib/configCompare';
import { shouldStartNewPreviewSession } from '../lib/previewSession';
import { useChatHistoryPaging } from '@/pages/AgentPlayground/hooks/useChatHistoryPaging';
import { SESSIONS_PAGE_SIZE, useSessionListPagination } from './useSessionListPagination';

export interface UsePreviewChatOptions {
  agentId?: number;
  versionId?: number;
  /** 当前版本是否草稿：草稿不可 pin，需直接传 profile 建会话 */
  isDraftVersion: boolean;
  isBuiltin: boolean;
  system: string;
  defaultModel: string;
  modelCatalog?: ModelCatalog | null;
  tools: string[];
  /** official/tools，创建会话时写入 tools[].ref */
  officialTools?: OfficialToolItem[];
  catalog?: CapabilityCatalog | null;
  toolDefaults?: BaseProfile['toolDefaults'];
  rawProfile: BaseProfile;
  dirty: boolean;
  onAutoSaveDraft: () => Promise<boolean>;
}

export function usePreviewChat({
  agentId,
  versionId,
  isDraftVersion,
  isBuiltin,
  system,
  defaultModel,
  modelCatalog,
  tools,
  officialTools = [],
  catalog = null,
  toolDefaults,
  rawProfile,
  dirty,
  onAutoSaveDraft,
}: UsePreviewChatOptions) {
  const structuredAttachmentTypes = useMemo(
    () => structuredAttachmentTypesForModel(modelCatalog, defaultModel),
    [defaultModel, modelCatalog],
  );
  const [sseState, sseActions] = useSSE({ structuredAttachmentTypes });
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [activeSessionVersionId, setActiveSessionVersionId] = useState<number | undefined>();
  const [activeSessionProfile, setActiveSessionProfile] = useState<
    BaseProfile | Session['profileYaml']
  >();
  const [inputText, setInputText] = useState('');
  const [isSending, setIsSending] = useState(false);
  const sendingRef = useRef(false);
  const chatRef = useRef<HTMLDivElement>(null!);
  const historyRequestRef = useRef(0);
  // 进会话只回放最新一页，更早的对话由上滚分页补齐
  const {
    isLoadingHistory,
    setIsLoadingHistory,
    loadLatest,
    reset: resetHistoryPaging,
    handleChatScroll,
  } = useChatHistoryPaging({
    prependReplayEvents: sseActions.prependReplayEvents,
    isBusy: sseState.isRunning,
  });
  const bootstrappedAgentRef = useRef<number | 'builtin' | null>(null);

  const sessionsResetKey = isBuiltin ? 'builtin' : (agentId ?? 'none');
  const fetchSessionsPage = useCallback(
    (page: number) =>
      getSessions({
        page,
        pageSize: SESSIONS_PAGE_SIZE,
        sessionKind: 'agent_loop',
        // 我的 Agent：只拉该 agent-loop 下的会话，便于进入时恢复最近一次对话
        ...(agentId != null ? { agentLoopId: agentId } : {}),
      }),
    [agentId],
  );
  const {
    sessions: rawSessions,
    sessionsLoading,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    refreshSessions,
    updateSessionTitle,
  } = useSessionListPagination({
    fetchPage: fetchSessionsPage,
    resetKey: sessionsResetKey,
    // agentId 未就绪 / 官方模版未落库：不拉历史（新配置直接空态）
    enabled: isBuiltin || agentId != null,
  });
  const sessions = useMemo(() => rawSessions.filter(hasAgentkLedger), [rawSessions]);

  // 切换 Agent 时清空对话态，允许重新引导最新会话
  useEffect(() => {
    bootstrappedAgentRef.current = null;
    setSessionId(null);
    setActiveSessionVersionId(undefined);
    setActiveSessionProfile(undefined);
    setInputText('');
    resetHistoryPaging(null);
    sseActions.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, isBuiltin]);

  const switchSession = useCallback(
    async (sid: string, meta?: Session) => {
      if (sid === sessionId) return;
      if (!sid.startsWith('ses_')) {
        message.warning('该会话无有效对话记录，已跳过');
        return;
      }
      const requestId = historyRequestRef.current + 1;
      historyRequestRef.current = requestId;
      setSessionId(sid);
      const fromList = meta ?? sessions.find((s) => s.id === sid) ?? null;
      setActiveSessionVersionId(fromList?.agentVersionId);
      setActiveSessionProfile(fromList?.profileYaml);
      setIsLoadingHistory(true);
      try {
        const bootstrap = await loadLatest(sid);
        if (historyRequestRef.current !== requestId) return;
        const { events } = bootstrap;
        const resumeExecutionId = pickResumeExecutionId(fromList ?? undefined, events);
        sseActions.startSession(sid, events, { resumeExecutionId: resumeExecutionId || undefined });
      } catch {
        if (historyRequestRef.current !== requestId) return;
        const resumeExecutionId = pickResumeExecutionId(fromList ?? undefined);
        if (resumeExecutionId) {
          sseActions.startSession(sid, [], { resumeExecutionId });
          return;
        }
        sseActions.reset();
        setSessionId(null);
        setActiveSessionVersionId(undefined);
        setActiveSessionProfile(undefined);
        message.error('历史账本加载失败，请换一个有对话记录的会话');
      } finally {
        if (historyRequestRef.current === requestId) setIsLoadingHistory(false);
      }
    },
    [sessionId, sessions, sseActions, loadLatest, setIsLoadingHistory],
  );

  // 从我的 Agent 进入：默认打开该 Agent 最近一次历史会话
  useEffect(() => {
    const key = isBuiltin ? 'builtin' : (agentId ?? null);
    if (key == null || bootstrappedAgentRef.current === key) return;
    if (isBuiltin) {
      bootstrappedAgentRef.current = key;
      return;
    }
    if (sessionsLoading) return;
    bootstrappedAgentRef.current = key;
    if (sessions.length > 0) void switchSession(sessions[0].id, sessions[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, isBuiltin, sessionsLoading, sessions]);

  const buildCurrentProfile = useCallback((): BaseProfile => {
    const profile = sanitizeProfile(
      {
        ...rawProfile,
        system,
        ...(defaultModel ? { defaultModel } : {}),
        tools: toToolRefs(tools, officialTools, catalog),
        ...(toolDefaults && Object.keys(toolDefaults).length > 0
          ? { toolDefaults }
          : { toolDefaults: undefined }),
      },
      catalog,
      officialTools,
    );
    return profile;
  }, [rawProfile, system, defaultModel, tools, officialTools, catalog, toolDefaults]);

  const createWithCurrentConfig = useCallback(
    async (customTitle?: string): Promise<string | null> => {
      try {
        const profile = buildCurrentProfile();
        // 草稿 / 脏配置 / 内置：后端拒绝 pin 未发布版本，直接下发 profile（与自由配置建会话一致）
        // 已发布且未改动：pin agentLoopVersionId
        const useProfileBody = dirty || isBuiltin || isDraftVersion || !versionId;
        const body: Parameters<typeof createSession>[0] = {
          sessionKind: 'agent_loop',
          ...(useProfileBody ? { profile } : { agentLoopVersionId: versionId }),
        };
        const trimmedTitle = customTitle?.trim();
        if (trimmedTitle) body.title = trimmedTitle;
        const sess = await createSession(body);
        historyRequestRef.current += 1;
        resetHistoryPaging(sess.id);
        setSessionId(sess.id);
        setActiveSessionVersionId(versionId);
        setActiveSessionProfile(profile);
        sseActions.startSession(sess.id);
        refreshSessions();
        return sess.id;
      } catch (error: unknown) {
        message.error(error instanceof Error ? error.message : '创建会话失败');
        return null;
      }
    },
    [
      dirty,
      isBuiltin,
      isDraftVersion,
      versionId,
      buildCurrentProfile,
      sseActions,
      refreshSessions,
      resetHistoryPaging,
    ],
  );

  const sendOnSession = useCallback(
    async (sid: string, text: string, attachments?: MessageAttachment[]): Promise<boolean> => {
      const ok = await sseActions.send(sid, text, attachments);
      if (ok) refreshSessions();
      return ok;
    },
    [sseActions, refreshSessions],
  );

  const startNewAndSend = useCallback(
    async (text: string, attachments?: MessageAttachment[]): Promise<boolean> => {
      const shouldAutoSave = dirty && !isBuiltin;
      const sid = await createWithCurrentConfig();
      if (!sid) return false;
      const sent = await sendOnSession(sid, text, attachments);
      if (!sent) return false;
      if (shouldAutoSave) {
        const ok = await onAutoSaveDraft();
        if (!ok) message.warning('对话已开始，但自动保存草稿失败，请手动保存');
      }
      return true;
    },
    [dirty, isBuiltin, createWithCurrentConfig, sendOnSession, onAutoSaveDraft],
  );

  const handleSend = useCallback(
    async (attachments?: MessageAttachment[]): Promise<boolean> => {
      const text = inputText.trim();
      const files = attachments?.filter((a) => a.url.trim()) ?? [];
      if (!text && files.length === 0) return false;
      if (sendingRef.current) return false;
      sendingRef.current = true;
      setIsSending(true);
      try {
        const payload = files.length > 0 ? files : undefined;
        // 比较完整 Profile 语义；后端缺少快照时才回退版本 ID。
        if (
          shouldStartNewPreviewSession({
            sessionId,
            dirty,
            currentProfile: buildCurrentProfile(),
            sessionProfile: activeSessionProfile,
            sessionVersionId: activeSessionVersionId,
            currentVersionId: versionId,
          })
        ) {
          return await startNewAndSend(text, payload);
        }
        if (!sessionId) return false;
        return await sendOnSession(sessionId, text, payload);
      } finally {
        sendingRef.current = false;
        setIsSending(false);
      }
    },
    [
      inputText,
      sessionId,
      activeSessionVersionId,
      activeSessionProfile,
      versionId,
      dirty,
      buildCurrentProfile,
      startNewAndSend,
      sendOnSession,
    ],
  );

  return {
    sseState,
    sseActions,
    inputText,
    setInputText,
    handleSend,
    sessionId,
    sessions,
    sessionsLoading,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    switchSession,
    updateSessionTitle,
    chatRef,
    isLoadingHistory,
    handleChatScroll,
    isSending,
  };
}
