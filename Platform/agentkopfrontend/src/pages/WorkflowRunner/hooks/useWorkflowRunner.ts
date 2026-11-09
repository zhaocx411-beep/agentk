/**
 * PipelineRunner 核心逻辑 Hook
 * 负责：调试会话的创建/复用、历史会话列表与切换（Pipeline 运行记录挂在会话维度）、
 * Pipeline 列表拉取、运行历史、运行状态轮询
 */
import { useState, useEffect, useRef, useCallback } from 'react';
import { App } from 'antd';
import {
  getWorkflows,
  getWorkflowRuns,
  getWorkflowRunDetail,
  runWorkflow,
} from '@/services/workflow';
import { getSessions } from '@/pages/AgentPlayground/api';
import type {
  Session,
  SessionFilter,
  WorkflowDefinition,
  WorkflowRun,
} from '@/pages/AgentPlayground/types';
import { adaptWorkflowInfo } from '@/pages/AgentPlayground/types';
import type { WorkflowVersion } from '@/types/workflowPersistence';
import { workflowDetailsFromJson } from '@/utils/workflowDefinition';
import { createPipelineSession, detailToPatch, mapPipelineRunStatus } from '@/utils/workflowRun';

/** 当前查看会话 ID 的本地缓存 key：刷新后保持上下文 */
const SESSION_STORAGE_KEY = 'workflow-runner-session-id';

export function useWorkflowRunner() {
  const { message: messageApi } = App.useApp();
  // ===== 会话（当前查看 + 历史列表） =====
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isSessionReady, setIsSessionReady] = useState(false);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsHasMore, setSessionsHasMore] = useState(false);
  const [isLoadingMoreSessions, setIsLoadingMoreSessions] = useState(false);
  const historyListRef = useRef<HTMLDivElement>(null);
  /** 已加载页码：页码分页用 */
  const sessionsPageRef = useRef(0);

  // ===== 历史会话过滤：运行持久化版本后按 workflowId 过滤；未绑定时查询全部 pipeline 会话 =====
  const [sessionFilter, setSessionFilter] = useState<SessionFilter | null>(null);
  /** 过滤提示文案（如「ops-assistant」） */
  const [sessionFilterLabel, setSessionFilterLabel] = useState<string | null>(null);
  const sessionFilterRef = useRef<SessionFilter | null>(null);
  sessionFilterRef.current = sessionFilter;

  // ===== Pipeline 列表与选择 =====
  const [availableWorkflows, setAvailableWorkflows] = useState<WorkflowDefinition[]>([]);
  const [isLoadingWorkflows, setIsLoadingWorkflows] = useState(false);
  const [selectedWorkflow, setSelectedWorkflow] = useState<WorkflowDefinition | null>(null);
  const [workflowParams, setWorkflowParams] = useState<Record<string, string | number>>({});
  /** 参数输入模式：form = schema 表单；json = 整体原始 args（对齐 agent-k demoui） */
  const [paramMode, setParamMode] = useState<'form' | 'json'>('form');
  /** JSON 模式下的 args 原文 */
  const [rawArgsJson, setRawArgsJson] = useState('{}');

  // ===== 运行实例 =====
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRun[]>([]);
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  /** 加载持久化版本后期望自动选中的 workflow 名（列表回填后消费） */
  const pendingWorkflowNameRef = useRef<string | null>(null);

  // ===== 轮询管理 =====
  const pollingTimersRef = useRef<Record<string, NodeJS.Timeout>>({});

  const stopPolling = useCallback((runId: string) => {
    if (pollingTimersRef.current[runId]) {
      clearInterval(pollingTimersRef.current[runId]);
      delete pollingTimersRef.current[runId];
    }
  }, []);

  const stopAllPolling = useCallback(() => {
    for (const timer of Object.values(pollingTimersRef.current)) clearInterval(timer);
    pollingTimersRef.current = {};
  }, []);

  const startPolling = useCallback(
    (sid: string, runId: string) => {
      stopPolling(runId);
      const timer = setInterval(async () => {
        try {
          const detail = await getWorkflowRunDetail(sid, runId);
          if (!detail) return;
          const patch = detailToPatch(detail);
          setWorkflowRuns((prev) => prev.map((r) => (r.runId === runId ? { ...r, ...patch } : r)));
          if (detail.status === 'completed' || detail.status === 'failed') {
            stopPolling(runId);
            setIsRunning(false);
          }
        } catch (err) {
          console.error(`轮询 Pipeline ${runId} 详情失败:`, err);
        }
      }, 2000);
      pollingTimersRef.current[runId] = timer;
    },
    [stopPolling],
  );

  // 卸载时清理所有轮询
  useEffect(() => stopAllPolling, [stopAllPolling]);

  // ===== 拉取运行历史（列表初始化 + 逐条详情补节点 + running 续轮询）=====
  const fetchWorkflowRuns = useCallback(
    async (sid: string) => {
      try {
        const res = await getWorkflowRuns(sid);
        if (!res?.pipelineRuns) return;
        const runs: WorkflowRun[] = res.pipelineRuns.map((r) => ({
          runId: r.id,
          workflowId: r.name,
          workflowName: r.name,
          params: (r.inputs as Record<string, string | number>) ?? {},
          nodes: [],
          status: mapPipelineRunStatus(r.status),
          output: r.output ?? undefined,
          createdAt: r.createdAt,
        }));
        setWorkflowRuns(runs);

        for (const r of res.pipelineRuns) {
          try {
            const detail = await getWorkflowRunDetail(sid, r.id);
            if (detail) {
              const patch = detailToPatch(detail);
              setWorkflowRuns((prev) =>
                prev.map((run) => (run.runId === r.id ? { ...run, ...patch } : run)),
              );
            }
            if (r.status === 'running') startPolling(sid, r.id);
          } catch {
            // 单条详情失败不影响整体
          }
        }
      } catch (err) {
        console.error('获取 Pipeline 运行历史失败:', err);
      }
    },
    [startPolling],
  );

  /** 已成功拉取过 Pipeline 列表的会话 id：避免同会话重复拉取 */
  const workflowsLoadedSidRef = useRef<string | null>(null);

  // ===== 拉取 Pipeline 列表 =====
  const fetchWorkflows = useCallback(
    async (sid: string) => {
      setIsLoadingWorkflows(true);
      try {
        const res = await getWorkflows(sid);
        setAvailableWorkflows((res?.pipelines || []).map(adaptWorkflowInfo));
        workflowsLoadedSidRef.current = sid;
      } catch (err) {
        messageApi.error('获取 Pipeline 列表失败');
        console.error(err);
      } finally {
        setIsLoadingWorkflows(false);
      }
    },
    [messageApi],
  );

  // ===== 初始化：使用实例默认 Profile 创建当前会话 =====
  // initRef 防止 StrictMode double-mount 导致接口双调。
  // 注意不能用 cancelled 标记：StrictMode 会先跑一遍 cleanup，会把唯一有效的初始化标记取消
  const initRef = useRef(false);
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;
    (async () => {
      // 历史会话与 Playground 同一接口，仅入参不同（sessionKind: 'pipeline'）；
      // 记录当前过滤 key，避免下方过滤 effect 重复请求
      lastFilterKeyRef.current = JSON.stringify(sessionFilterRef.current ?? null);
      getSessions({ page: 1, pageSize: 20, sessionKind: 'pipeline' })
        .then((res) => {
          // 合并而非覆盖：保留刚创建但列表接口尚未可见的新会话
          setSessions((prev) => {
            const ids = new Set(res.sessions.map((s) => s.id));
            return [...prev.filter((s) => !ids.has(s.id)), ...res.sessions];
          });
          sessionsPageRef.current = res.page;
          setSessionsHasMore(res.hasMore);
        })
        .catch(() => {
          // 静默失败
        });

      // 页面进入时固定先取 /agentk/capabilities，再把据此拼出的 Profile 传给创建会话接口。
      try {
        const session = await createPipelineSession({
          title: `Pipeline 运行 - ${new Date().toLocaleString()}`,
        });
        if (session?.id) {
          localStorage.setItem(SESSION_STORAGE_KEY, session.id);
          setSessionId(session.id);
          setIsSessionReady(true);
          setSessions((prev) =>
            prev.some((s) => s.id === session.id)
              ? prev
              : [{ id: session.id, title: session.title }, ...prev],
          );
        }
      } catch (err) {
        messageApi.error('初始化 Pipeline 会话失败');
        console.error(err);
      }
    })();
  }, [messageApi]);

  // ===== 会话就绪/切换后拉取列表与历史 =====
  useEffect(() => {
    if (!sessionId) return;
    // 初始化验证缓存会话时已拉过列表，不重复请求
    if (workflowsLoadedSidRef.current !== sessionId) {
      fetchWorkflows(sessionId);
    }
    fetchWorkflowRuns(sessionId);
  }, [sessionId, fetchWorkflows, fetchWorkflowRuns]);

  // ===== 切换历史会话：查看该会话下的 Pipeline 与运行记录 =====
  const handleSwitchSession = useCallback(
    (sid: string) => {
      if (sid === sessionId) return;
      stopAllPolling();
      setSessionId(sid);
      localStorage.setItem(SESSION_STORAGE_KEY, sid);
      // 清空上一个会话的 Pipeline 面板状态，等待新会话数据回填
      setAvailableWorkflows([]);
      setSelectedWorkflow(null);
      setWorkflowParams({});
      setWorkflowRuns([]);
      setActiveRunId(null);
      setIsRunning(false);
    },
    [sessionId, stopAllPolling],
  );

  // 过滤条件变化时重拉第一页（初始不过滤不拉取，跳过）
  const lastFilterKeyRef = useRef<string | null>(null);
  useEffect(() => {
    const key = JSON.stringify(sessionFilter ?? null);
    if (lastFilterKeyRef.current === null || lastFilterKeyRef.current === key) return;
    lastFilterKeyRef.current = key;

    // 未绑定 Pipeline（清除过滤）时查询全部 pipeline 会话；有版本过滤时叠加 workflowId
    getSessions({ page: 1, pageSize: 20, sessionKind: 'pipeline', ...(sessionFilter ?? {}) })
      .then((res) => {
        setSessions(res.sessions);
        sessionsPageRef.current = res.page;
        setSessionsHasMore(res.hasMore);
      })
      .catch(() => {});
  }, [sessionFilter]);

  /** 清除历史会话过滤：回到未绑定状态，列表回退为全部 pipeline 会话 */
  const clearSessionFilter = useCallback(() => {
    setSessionFilter(null);
    setSessionFilterLabel(null);
  }, []);

  // ===== 会话列表滚动加载更多 =====
  const handleHistoryScroll = useCallback(() => {
    const el = historyListRef.current;
    if (!el || isLoadingMoreSessions || !sessionsHasMore) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 20) {
      setIsLoadingMoreSessions(true);
      getSessions({
        page: sessionsPageRef.current + 1,
        pageSize: 20,
        sessionKind: 'pipeline',
        ...(sessionFilterRef.current ?? {}),
      })
        .then((res) => {
          if (res.sessions.length) {
            setSessions((prev) => [...prev, ...res.sessions]);
            sessionsPageRef.current = res.page;
          }
          setSessionsHasMore(res.hasMore && res.sessions.length > 0);
        })
        .catch(() => {})
        .finally(() => setIsLoadingMoreSessions(false));
    }
  }, [isLoadingMoreSessions, sessionsHasMore]);

  // ===== 选择 Pipeline（默认值回填，array/object 序列化为 JSON 文本）=====
  const handleSelectWorkflow = useCallback(
    (wfId: string) => {
      const wf = availableWorkflows.find((w) => w.id === wfId) ?? null;
      setSelectedWorkflow(wf);
      if (!wf) {
        setWorkflowParams({});
        setParamMode('form');
        setRawArgsJson('{}');
        return;
      }
      const defaults: Record<string, string | number> = {};
      for (const p of wf.params) {
        if (p.defaultValue !== undefined) {
          defaults[p.name] =
            (p.type === 'array' || p.type === 'object') && typeof p.defaultValue !== 'string'
              ? JSON.stringify(p.defaultValue)
              : p.defaultValue;
        } else if (p.type === 'number') {
          defaults[p.name] = p.min ?? '';
        } else {
          defaults[p.name] = '';
        }
      }
      setWorkflowParams(defaults);
      // 无 inputs schema 的 Pipeline 降级为 JSON 模式（原始 args 输入）；
      // JSON 模式预填 schema 默认值，便于在默认值基础上改
      setParamMode(wf.params.length === 0 ? 'json' : 'form');
      const jsonDefaults = Object.fromEntries(
        wf.params.filter((p) => p.defaultValue !== undefined).map((p) => [p.name, p.defaultValue]),
      );
      setRawArgsJson(JSON.stringify(jsonDefaults, null, 2));
    },
    [availableWorkflows],
  );

  // ===== 运行 Pipeline（运行在当前查看的会话下，与 playground 语义一致）=====
  const handleRunWorkflow = useCallback(async () => {
    if (!selectedWorkflow || !sessionId) return;
    if (workflowRuns.some((r) => r.status === 'running')) {
      messageApi.warning('已有 Pipeline 正在运行，请等待完成');
      return;
    }
    // JSON 模式：整体 args 原文提交（对齐 agent-k demoui 的原始 JSON 输入，可传 schema 外字段）；
    // 表单模式：array/object 参数在表单里是 JSON 文本，提交前解析为对应类型
    let submitInputs: Record<string, unknown>;
    if (paramMode === 'json') {
      try {
        const parsed: unknown = JSON.parse(rawArgsJson.trim() || '{}');
        if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
          messageApi.error('args 必须是 JSON object');
          return;
        }
        submitInputs = parsed as Record<string, unknown>;
      } catch {
        messageApi.error('args 不是合法 JSON，请检查格式');
        return;
      }
    } else {
      submitInputs = {};
      for (const p of selectedWorkflow.params) {
        const raw = workflowParams[p.name];
        if (p.required && !raw) {
          messageApi.warning(`请填写「${p.label}」`);
          return;
        }
        if ((p.type === 'array' || p.type === 'object') && typeof raw === 'string' && raw.trim()) {
          try {
            const parsed: unknown = JSON.parse(raw);
            if (p.type === 'array' && !Array.isArray(parsed)) {
              messageApi.error(`「${p.label}」需要 JSON 数组，如 ["https://...", "https://..."]`);
              return;
            }
            submitInputs[p.name] = parsed;
          } catch {
            messageApi.error(`「${p.label}」不是合法 JSON，请检查格式`);
            return;
          }
        } else if (raw !== '' && raw !== undefined) {
          submitInputs[p.name] = raw;
        }
      }
    }

    setIsRunning(true);
    try {
      const data = await runWorkflow(sessionId, {
        workflow: selectedWorkflow.name,
        pipeline: selectedWorkflow.name,
        inputs: submitInputs,
      });
      if (!data) {
        messageApi.error('启动 Pipeline 失败：无返回数据');
        setIsRunning(false);
        return;
      }
      const run: WorkflowRun = {
        runId: data.id,
        workflowId: selectedWorkflow.id,
        workflowName: data.name,
        params: submitInputs,
        nodes: [],
        status: data.status === 'running' ? 'running' : 'completed',
        createdAt: new Date().toISOString(),
      };
      setWorkflowRuns((prev) => [...prev, run]);
      setActiveRunId(data.id);
      messageApi.success(`Pipeline ${data.name} 已启动`);
      startPolling(sessionId, data.id);
    } catch (err) {
      messageApi.error('启动 Pipeline 失败');
      console.error(err);
      setIsRunning(false);
    }
  }, [
    selectedWorkflow,
    sessionId,
    workflowParams,
    workflowRuns,
    paramMode,
    rawArgsJson,
    startPolling,
    messageApi,
  ]);

  // ===== 加载持久化版本运行：由平台 pin workflowVersionId 并创建会话 =====
  const handleRunPersistedVersion = useCallback(
    async (version: WorkflowVersion, entityName: string) => {
      // 前置校验：Pipeline 未声明 nodes 时后端会 400（no nodes declared），提前拦截并给出可操作提示。
      // 常见场景：新建/存草稿时用骨架预填，nodes 留空未补全
      const details = workflowDetailsFromJson(version.definitionJson);
      const noNodes = details.filter((d) => d.nodes.length === 0);
      if (details.length === 0 || noNodes.length > 0) {
        messageApi.error(
          noNodes.length > 0
            ? `pipeline「${noNodes[0].name}」未定义 nodes（草稿骨架未补全），无法运行。请先在「版本管理 → 详情」确认并补全定义`
            : '该版本未包含任何 Pipeline 定义',
        );
        return;
      }
      try {
        const session = await createPipelineSession({
          title: `Pipeline调试 ${entityName} v${version.version}`,
          workflowVersionId: version.id,
        });
        if (!session?.id) return;
        // 历史会话按 workflowId + workflowVersionId 隔离，与建会话时的版本 pin 对齐
        setSessionFilter({ workflowId: version.workflowId, workflowVersionId: version.id });
        setSessionFilterLabel(`${entityName} v${version.version}`);
        // 切换到新会话（与 handleSwitchSession 同样的面板清理）
        stopAllPolling();
        localStorage.setItem(SESSION_STORAGE_KEY, session.id);
        setSessions((prev) => [{ id: session.id, title: session.title }, ...prev]);
        setAvailableWorkflows([]);
        setSelectedWorkflow(null);
        setWorkflowParams({});
        setWorkflowRuns([]);
        setActiveRunId(null);
        setIsRunning(false);
        // 列表回填后自动选中该 Pipeline
        pendingWorkflowNameRef.current =
          workflowDetailsFromJson(version.definitionJson)[0]?.name ?? null;
        setSessionId(session.id);
        messageApi.success(`已把「${entityName} v${version.version}」加载到新会话，可直接运行`);
      } catch (err) {
        console.error(err);
        messageApi.error('加载版本到新会话失败');
      }
    },
    [messageApi, stopAllPolling],
  );

  // 加载持久化版本后：Pipeline 列表回填完成即自动选中目标 Pipeline
  useEffect(() => {
    const name = pendingWorkflowNameRef.current;
    if (!name || availableWorkflows.length === 0) return;
    const wf = availableWorkflows.find((w) => w.name === name);
    if (wf) {
      pendingWorkflowNameRef.current = null;
      handleSelectWorkflow(wf.id);
    }
  }, [availableWorkflows, handleSelectWorkflow]);

  const activeRun = workflowRuns.find((r) => r.runId === activeRunId) ?? null;

  const updateSessionTitle = useCallback((sessionId: string, title: string) => {
    setSessions((prev) =>
      prev.map((item) => (item.id === sessionId ? { ...item, title } : item)),
    );
  }, []);

  return {
    sessionId,
    isSessionReady,
    sessions,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    handleSwitchSession,
    updateSessionTitle,
    sessionFilterLabel,
    clearSessionFilter,
    availableWorkflows,
    isLoadingWorkflows,
    selectedWorkflow,
    workflowParams,
    setWorkflowParams,
    paramMode,
    setParamMode,
    rawArgsJson,
    setRawArgsJson,
    workflowRuns,
    activeRun,
    isRunning,
    hasRunningWorkflow: workflowRuns.some((r) => r.status === 'running'),
    handleSelectWorkflow,
    handleRunWorkflow,
    handleViewRun: setActiveRunId,
    handleRunPersistedVersion,
  };
}
