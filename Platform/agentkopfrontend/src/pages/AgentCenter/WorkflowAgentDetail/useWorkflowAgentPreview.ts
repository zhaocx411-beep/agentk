/**
 * 工作流 Agent 右侧预览：
 * - 左侧有自定义 workflows → 用当前定义生成参数表单
 * - 左侧为空对象 → 走默认 profile 的 Pipeline 下拉
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { App } from 'antd';
import { getSessions } from '@/pages/AgentPlayground/api';
import { getCapabilities, getWorkflows } from '@/services/workflow';
import type { Session, WorkflowDefinition } from '@/pages/AgentPlayground/types';
import type { WorkflowVersion } from '@/types/workflowPersistence';
import { adaptWorkflowInfo } from '@/pages/AgentPlayground/types';
import { createPipelineSession } from '@/utils/workflowRun';
import { buildDefaultParams, definitionsFromDraft } from './pipelineRuntime';
import { useWorkflowRuns } from './useWorkflowRuns';
import { SESSIONS_PAGE_SIZE } from '../AgentDetail/hooks/useSessionListPagination';

const PROFILE_SESSION_MARK = '__profile__';

function tryParseDraft(definitionJson: string): { valid: boolean; defs: WorkflowDefinition[] } {
  try {
    JSON.parse(definitionJson);
  } catch {
    return { valid: false, defs: [] };
  }
  return { valid: true, defs: definitionsFromDraft(definitionJson) };
}

export function useWorkflowAgentPreview(
  entityName: string,
  version: WorkflowVersion | null,
  definitionJson: string,
) {
  const { message: messageApi } = App.useApp();
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isSessionReady, setIsSessionReady] = useState(false);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsHasMore, setSessionsHasMore] = useState(false);
  const [isLoadingMoreSessions, setIsLoadingMoreSessions] = useState(false);
  const historyListRef = useRef<HTMLDivElement>(null);
  const sessionsPageRef = useRef(0);
  const [profileWorkflows, setProfileWorkflows] = useState<WorkflowDefinition[]>([]);
  const [pickedName, setPickedName] = useState<string | null>(null);
  const [paramDraft, setParamDraft] = useState<Record<string, string | number>>({});
  const [paramMode, setParamMode] = useState<'form' | 'json'>('form');
  const [rawArgsJson, setRawArgsJson] = useState('{}');
  const appliedDefinitionRef = useRef<string | null>(null);
  const lastValidDraftRef = useRef<WorkflowDefinition[] | null>(null);
  const prevVersionIdRef = useRef<number | undefined>(version?.id);
  const wasUsingDraftRef = useRef(false);
  // 实体名与版本来自两个独立请求，名字后到不应触发第二次建会话，只用于标题文案
  const entityNameRef = useRef(entityName);
  entityNameRef.current = entityName;
  const runs = useWorkflowRuns(
    (text) => messageApi.error(text),
    (text) => messageApi.success(text),
  );

  if (version?.id !== prevVersionIdRef.current) {
    prevVersionIdRef.current = version?.id;
    lastValidDraftRef.current = null;
    wasUsingDraftRef.current = false;
    if (pickedName !== null) setPickedName(null);
    if (Object.keys(paramDraft).length > 0) setParamDraft({});
    setRawArgsJson('{}');
  }

  const parsed = useMemo(() => tryParseDraft(definitionJson), [definitionJson]);
  if (parsed.valid) lastValidDraftRef.current = parsed.defs;

  const draftDefs = parsed.valid ? parsed.defs : (lastValidDraftRef.current ?? []);
  const usingDraft = draftDefs.length > 0;

  if (parsed.valid && !usingDraft && wasUsingDraftRef.current) {
    wasUsingDraftRef.current = false;
    if (pickedName !== null) setPickedName(null);
    if (Object.keys(paramDraft).length > 0) setParamDraft({});
    setRawArgsJson('{}');
  } else if (usingDraft) {
    wasUsingDraftRef.current = true;
  }

  const availableWorkflows = usingDraft ? draftDefs : profileWorkflows;
  const selectedWorkflow = usingDraft
    ? (availableWorkflows.find((item) => item.name === pickedName) ?? availableWorkflows[0] ?? null)
    : (availableWorkflows.find((item) => item.name === pickedName || item.id === pickedName) ??
      null);
  const showPipelineSelect = !usingDraft || availableWorkflows.length > 1;
  const resolvedParamMode: 'form' | 'json' =
    !selectedWorkflow || selectedWorkflow.params.length === 0 ? 'json' : paramMode;

  const workflowParams = useMemo(() => {
    if (!selectedWorkflow) return {};
    const defaults = buildDefaultParams(selectedWorkflow);
    return {
      ...defaults,
      ...Object.fromEntries(Object.entries(paramDraft).filter(([key]) => key in defaults)),
    };
  }, [selectedWorkflow, paramDraft]);

  const setWorkflowParams: Dispatch<SetStateAction<Record<string, string | number>>> = useCallback(
    (value) => {
      setParamDraft((prev) => (typeof value === 'function' ? value(prev) : value));
    },
    [],
  );

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const catalog = await getCapabilities();
        if (cancelled) return;
        setProfileWorkflows((prev) =>
          prev.length > 0
            ? prev
            : catalog.pipelines.map((item) => ({
                id: item.name,
                name: item.name,
                description: item.description || '',
                nodes: [],
                params: [],
              })),
        );
      } catch {
        /* 会话就绪后仍会用 /pipelines 回填 */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!version) return;
    let cancelled = false;
    (async () => {
      setIsSessionReady(false);
      runs.resetRuns();
      setSessionsLoading(true);
      try {
        const [session, history] = await Promise.all([
          // 版本定义由平台按 workflowVersionId 绑定，前端不再随 profile 下发 workflows
          createPipelineSession({
            title: `${entityNameRef.current || '工作流'} v${version.version}`,
            workflowVersionId: version.id,
          }),
          getSessions({ page: 1, pageSize: SESSIONS_PAGE_SIZE, sessionKind: 'pipeline' }).catch(
            () => null,
          ),
        ]);
        if (cancelled || !session?.id) return;
        appliedDefinitionRef.current = String(version.id);
        setSessionId(session.id);
        const fromHistory = history?.sessions ?? [];
        setSessions(
          fromHistory.some((item) => item.id === session.id)
            ? fromHistory
            : [{ id: session.id, title: session.title }, ...fromHistory],
        );
        sessionsPageRef.current = history?.page ?? 1;
        setSessionsHasMore(history?.hasMore ?? false);
        const pipelines = await getWorkflows(session.id);
        if (cancelled) return;
        setProfileWorkflows((pipelines?.pipelines || []).map(adaptWorkflowInfo));
        await runs.fetchRuns(session.id);
        setIsSessionReady(true);
      } catch {
        messageApi.error('初始化工作流预览会话失败');
      } finally {
        if (!cancelled) setSessionsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version?.id]);

  const ensureSessionWithLatestDefinition = useCallback(async (): Promise<{
    sid: string;
    workflow: WorkflowDefinition;
  } | null> => {
    try {
      JSON.parse(definitionJson);
    } catch {
      messageApi.error('左侧 definitionJson 不是合法 JSON');
      return null;
    }
    const liveDefs = definitionsFromDraft(definitionJson);
    const liveDraft = liveDefs.length > 0;
    const workflow = liveDraft
      ? (liveDefs.find((item) => item.name === selectedWorkflow?.name) ?? liveDefs[0])
      : selectedWorkflow;
    if (!workflow) {
      messageApi.error(liveDraft ? '当前定义未包含可运行的 Pipeline' : '请先选择 Pipeline');
      return null;
    }

    // 未保存的改动无法随会话下发，只能跑已保存版本，这里明确告知而不是静默降级
    if (liveDraft && version && definitionJson !== String(version.definitionJson)) {
      messageApi.warning('当前定义尚未保存，本次运行使用的是已保存版本');
    }

    const signature = version ? String(version.id) : PROFILE_SESSION_MARK;
    let sid = sessionId;
    if (!sid || appliedDefinitionRef.current !== signature) {
      const session = await createPipelineSession({
        title: `${entityName || '工作流'} 预览 ${new Date().toLocaleTimeString()}`,
        ...(version ? { workflowVersionId: version.id } : {}),
      });
      if (!session?.id) {
        messageApi.error('创建预览会话失败');
        return null;
      }
      sid = session.id;
      appliedDefinitionRef.current = signature;
      setSessionId(sid);
      setSessions((prev) =>
        prev.some((item) => item.id === sid)
          ? prev
          : [{ id: session.id, title: session.title }, ...prev],
      );
      if (!liveDraft) {
        const pipelines = await getWorkflows(sid);
        setProfileWorkflows((pipelines?.pipelines || []).map(adaptWorkflowInfo));
      }
    }
    return { sid, workflow };
  }, [definitionJson, sessionId, entityName, messageApi, selectedWorkflow, version]);

  const handleSwitchSession = useCallback(
    async (sid: string) => {
      if (sid === sessionId) return;
      runs.resetRuns();
      setSessionId(sid);
      setIsSessionReady(false);
      try {
        const pipelines = await getWorkflows(sid);
        setProfileWorkflows((pipelines?.pipelines || []).map(adaptWorkflowInfo));
        await runs.fetchRuns(sid);
        setIsSessionReady(true);
      } catch {
        messageApi.error('切换历史会话失败');
      }
    },
    [sessionId, runs, messageApi],
  );

  const handleHistoryScroll = useCallback(() => {
    const el = historyListRef.current;
    if (!el || isLoadingMoreSessions || !sessionsHasMore || sessionsLoading) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight >= 20) return;

    setIsLoadingMoreSessions(true);
    getSessions({
      page: sessionsPageRef.current + 1,
      pageSize: SESSIONS_PAGE_SIZE,
      sessionKind: 'pipeline',
    })
      .then((res) => {
        if (res.sessions.length) {
          setSessions((prev) => {
            const ids = new Set(prev.map((item) => item.id));
            const appended = res.sessions.filter((item) => !ids.has(item.id));
            return appended.length ? [...prev, ...appended] : prev;
          });
          sessionsPageRef.current = res.page;
        }
        setSessionsHasMore(res.hasMore && res.sessions.length > 0);
      })
      .catch(() => undefined)
      .finally(() => setIsLoadingMoreSessions(false));
  }, [isLoadingMoreSessions, sessionsHasMore, sessionsLoading]);

  const updateSessionTitle = useCallback((sessionId: string, title: string) => {
    setSessions((prev) =>
      prev.map((item) => (item.id === sessionId ? { ...item, title } : item)),
    );
  }, []);

  return {
    sessionId,
    isSessionReady,
    sessions,
    sessionsLoading,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    updateSessionTitle,
    selectedWorkflow,
    availableWorkflows,
    showPipelineSelect,
    workflowParams,
    setWorkflowParams,
    paramMode: resolvedParamMode,
    setParamMode,
    rawArgsJson,
    setRawArgsJson,
    activeRun: runs.activeRun,
    isRunning: runs.isRunning,
    hasRunningWorkflow: runs.hasRunningWorkflow,
    handleSelectWorkflow: (id: string) => {
      const next = availableWorkflows.find((item) => item.id === id) ?? null;
      setPickedName(next?.name ?? null);
      if (next) {
        setParamDraft(buildDefaultParams(next));
        setParamMode(next.params.length === 0 ? 'json' : 'form');
        const jsonDefaults = Object.fromEntries(
          next.params
            .filter((p) => p.defaultValue !== undefined)
            .map((p) => [p.name, p.defaultValue]),
        );
        setRawArgsJson(JSON.stringify(jsonDefaults, null, 2));
      } else {
        setParamDraft({});
        setRawArgsJson('{}');
      }
    },
    handleRunWorkflow: async () => {
      const ready = await ensureSessionWithLatestDefinition();
      if (!ready) return;
      await runs.startRun(
        ready.sid,
        ready.workflow,
        resolvedParamMode,
        rawArgsJson,
        workflowParams,
      );
    },
    handleSwitchSession,
  };
}
