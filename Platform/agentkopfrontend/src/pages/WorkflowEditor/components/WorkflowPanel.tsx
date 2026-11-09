/**
 * PipelinePanel - Pipeline 编辑器右侧面板
 * 三个 Tab：选择 Pipeline / 调试运行 / 运行历史
 * 对齐 Dify / Coze / n8n 的编辑器内调试体验
 */
import { useState, useCallback, useEffect, useRef } from 'react';
import { Select, Input, InputNumber, Button, Tag, Empty, Spin } from 'antd';
import { message } from '@/utils/feedback';
import {
  ApartmentOutlined,
  PlayCircleOutlined,
  HistoryOutlined,
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  LoadingOutlined,
  RightOutlined,
  LeftOutlined,
  PlusOutlined,
} from '@ant-design/icons';
import { WorkflowParamSelect } from '@/components';
import {
  getWorkflows,
  runWorkflow,
  getWorkflowRunDetail,
  getWorkflowRuns,
  createSession,
} from '@/services/workflow';
import { NODE_STATUS_META } from '@/constants/workflowNodeStatus';
import { useCatalog } from '../hooks/useCatalog';
import { useWorkflowStore, type NodeRunStatus } from '../store';
import { buildSessionProfile } from '@/utils/profile';
import { mapNodeStatus, mapPipelineRunStatus } from '@/utils/workflowRun';
import type { WorkflowDefinition, WorkflowRun } from '@/pages/AgentPlayground/types';
import { adaptWorkflowInfo } from '@/pages/AgentPlayground/types';
import { findCanvasNodeId, toNodeRunStatus, workflowToCanvas } from '../utils';
import styles from './WorkflowPanel.module.less';

type TabKey = 'select' | 'debug' | 'history';

/** 根据后端返回的节点列表同步画布节点运行状态到 store */
function syncNodeStatusesToStore(backendNodes: { nodeId: string; status: string }[]) {
  const { nodes: canvasNodes } = useWorkflowStore.getState();
  const statuses: Record<string, NodeRunStatus> = {};
  for (const bn of backendNodes) {
    const canvasId = findCanvasNodeId(bn.nodeId, canvasNodes);
    if (canvasId) {
      statuses[canvasId] = toNodeRunStatus(mapNodeStatus(bn.status));
    }
  }
  useWorkflowStore.getState().setNodeRunStatuses(statuses);
}

function WorkflowPanel() {
  // ===== 面板折叠状态 =====
  const [collapsed, setCollapsed] = useState(false);
  const [activeTab, setActiveTab] = useState<TabKey>('select');

  // ===== 能力目录 =====
  const { catalog } = useCatalog();

  // ===== Session =====
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isCreatingSession, setIsCreatingSession] = useState(false);

  // ===== Pipeline 列表 =====
  const [availableWorkflows, setAvailableWorkflows] = useState<WorkflowDefinition[]>([]);
  const [isLoadingWorkflows, setIsLoadingWorkflows] = useState(false);
  const [selectedWorkflow, setSelectedWorkflow] = useState<WorkflowDefinition | null>(null);

  // ===== 调试参数 =====
  const [workflowParams, setWorkflowParams] = useState<Record<string, string | number>>({});
  const [isRunning, setIsRunning] = useState(false);
  const [currentRun, setCurrentRun] = useState<WorkflowRun | null>(null);

  // ===== 运行历史 =====
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRun[]>([]);
  const [isLoadingRuns, setIsLoadingRuns] = useState(false);

  // ===== 轮询管理 =====
  const pollingTimersRef = useRef<Record<string, NodeJS.Timeout>>({});

  const agentKConfig = useWorkflowStore((s) => s.agentKConfig);

  // ===== 确保 Session 存在 =====
  const ensureSession = useCallback(async (): Promise<string | null> => {
    if (sessionId) return sessionId;

    setIsCreatingSession(true);
    try {
      const sessionProfile = buildSessionProfile({
        catalog,
        system: agentKConfig.systemPrompt,
        toolNames: agentKConfig.tools.length > 0 ? agentKConfig.tools : (catalog?.toolNames ?? []),
      });

      const body: Parameters<typeof createSession>[0] = {
        title: `Pipeline 调试 - ${new Date().toLocaleTimeString()}`,
        sessionKind: 'pipeline',
        profile: sessionProfile,
      };

      const session = await createSession(body);

      if (session?.id) {
        setSessionId(session.id);
        message.success('会话已创建');
        return session.id;
      }
      return null;
    } catch (err) {
      message.error('创建会话失败');
      console.error(err);
      return null;
    } finally {
      setIsCreatingSession(false);
    }
  }, [sessionId, agentKConfig, catalog]);

  // ===== 获取 Pipeline 列表 =====
  const fetchWorkflows = useCallback(async (sid: string) => {
    setIsLoadingWorkflows(true);
    try {
      const res = await getWorkflows(sid);
      const adapted = (res?.pipelines || []).map(adaptWorkflowInfo);
      setAvailableWorkflows(adapted);
    } catch (err) {
      message.error('获取 Pipeline 列表失败');
      console.error(err);
    } finally {
      setIsLoadingWorkflows(false);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // ===== 获取运行历史 =====
  const fetchWorkflowRuns = useCallback(async (sid: string) => {
    setIsLoadingRuns(true);
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
      }));
      setWorkflowRuns(runs);

      // 逐条拉详情补充节点信息
      for (const r of res.pipelineRuns) {
        try {
          const detail = await getWorkflowRunDetail(sid, r.id);
          if (detail) {
            const detailNodes = detail.nodes ?? [];
            setWorkflowRuns((prev) => {
              const idx = prev.findIndex((run) => run.runId === r.id);
              if (idx === -1) return prev;
              const updated = [...prev];
              updated[idx] = {
                ...updated[idx],
                nodes: detailNodes.map((n) => ({
                  name: n.nodeId,
                  label: n.nodeId,
                  status: mapNodeStatus(n.status),
                  output: n.output,
                })),
                output: detail.output,
              };
              return updated;
            });
          }
          // running 状态的启动轮询
          if (r.status === 'running') {
            startPolling(sid, r.id);
          }
        } catch {
          // 详情拉取失败不影响整体
        }
      }
    } catch (err) {
      console.error('获取 Pipeline 运行历史失败:', err);
    } finally {
      setIsLoadingRuns(false);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- startPolling 在下方声明且引用稳定

  // ===== 轮询 Pipeline 运行状态 =====
  const stopPolling = useCallback((runId: string) => {
    if (pollingTimersRef.current[runId]) {
      clearInterval(pollingTimersRef.current[runId]);
      delete pollingTimersRef.current[runId];
    }
  }, []);

  const startPolling = useCallback(
    (sid: string, runId: string) => {
      stopPolling(runId);
      const timer = setInterval(async () => {
        try {
          const detail = await getWorkflowRunDetail(sid, runId);
          if (!detail) return;

          const detailNodes = detail.nodes ?? [];
          const newNodes = detailNodes.map((n) => ({
            name: n.nodeId,
            label: n.nodeId,
            status: mapNodeStatus(n.status),
            output: n.output,
          }));

          const updatedRun: WorkflowRun = {
            runId: detail.id,
            workflowId: detail.name,
            workflowName: detail.name,
            params: (detail.inputs as Record<string, string | number>) ?? {},
            nodes: newNodes,
            status:
              detail.status === 'completed'
                ? 'completed'
                : detail.status === 'failed'
                  ? 'failed'
                  : 'running',
            output: detail.output,
            error: detail.status === 'failed' ? detail.error || '执行失败' : undefined,
          };

          setCurrentRun((prev) => (prev?.runId === runId ? updatedRun : prev));
          setWorkflowRuns((prev) => {
            const idx = prev.findIndex((r) => r.runId === runId);
            if (idx === -1) return prev;
            const updated = [...prev];
            updated[idx] = updatedRun;
            return updated;
          });

          // 同步节点运行状态到画布
          syncNodeStatusesToStore(detailNodes);

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

  // 清理轮询
  useEffect(() => {
    return () => {
      for (const timer of Object.values(pollingTimersRef.current)) {
        clearInterval(timer);
      }
      pollingTimersRef.current = {};
    };
  }, []);

  // ===== Session 就绪后拉取数据 =====
  useEffect(() => {
    if (sessionId) {
      fetchWorkflows(sessionId);
      fetchWorkflowRuns(sessionId);
    }
  }, [sessionId, fetchWorkflows, fetchWorkflowRuns]);

  // ===== 选择 Pipeline =====
  const handleSelectWorkflow = useCallback(
    (wfId: string) => {
      const wf = availableWorkflows.find((w) => w.id === wfId) ?? null;
      setSelectedWorkflow(wf);
      if (wf) {
        const defaults: Record<string, string | number> = {};
        for (const p of wf.params) {
          if (p.defaultValue !== undefined) defaults[p.name] = p.defaultValue;
          else if (p.type === 'number') defaults[p.name] = p.min ?? '';
          else defaults[p.name] = '';
        }
        setWorkflowParams(defaults);

        // 将 Pipeline 节点同步到画布
        const { nodes, edges } = workflowToCanvas(wf);
        useWorkflowStore.getState().loadTemplate(nodes, edges);
      } else {
        setWorkflowParams({});
      }
      // 选中后自动切换到调试 Tab
      if (wf) setActiveTab('debug');
    },
    [availableWorkflows],
  );

  // ===== 调试运行 =====
  const handleRunWorkflow = useCallback(async () => {
    if (!selectedWorkflow) {
      message.warning('请先选择一个 Pipeline');
      return;
    }
    // 校验必填参数
    for (const p of selectedWorkflow.params) {
      if (p.required && !workflowParams[p.name] && workflowParams[p.name] !== 0) {
        message.warning(`请填写「${p.label}」`);
        return;
      }
    }

    const sid = await ensureSession();
    if (!sid) return;

    setIsRunning(true);
    setCurrentRun(null);
    // 重置画布节点运行状态
    useWorkflowStore.getState().resetNodeRunStatuses();

    try {
      const data = await runWorkflow(sid, {
        workflow: selectedWorkflow.name,
        pipeline: selectedWorkflow.name,
        inputs: workflowParams,
      });
      if (!data) {
        message.error('启动 Pipeline 失败：无返回数据');
        setIsRunning(false);
        return;
      }

      const run: WorkflowRun = {
        runId: data.id,
        workflowId: selectedWorkflow.id,
        workflowName: data.name,
        params: { ...workflowParams },
        nodes: [],
        status: data.status === 'running' ? 'running' : 'completed',
      };

      setCurrentRun(run);
      setWorkflowRuns((prev) => [run, ...prev]);
      message.success(`Pipeline ${data.name} 已启动`);

      // 启动轮询
      if (data.status === 'running') {
        startPolling(sid, data.id);
      } else {
        setIsRunning(false);
        // 已完成，拉详情
        try {
          const detail = await getWorkflowRunDetail(sid, data.id);
          if (detail) {
            const detailNodes = detail.nodes ?? [];
            const updatedRun: WorkflowRun = {
              ...run,
              nodes: detailNodes.map((n) => ({
                name: n.nodeId,
                label: n.nodeId,
                status: mapNodeStatus(n.status),
                output: n.output,
              })),
              output: detail.output,
              status: detail.status === 'completed' ? 'completed' : 'failed',
            };
            setCurrentRun(updatedRun);
            setWorkflowRuns((prev) => {
              const idx = prev.findIndex((r) => r.runId === data.id);
              if (idx === -1) return prev;
              const updated = [...prev];
              updated[idx] = updatedRun;
              return updated;
            });
            // 同步节点运行状态到画布
            syncNodeStatusesToStore(detailNodes);
          }
        } catch {
          // 详情拉取失败不影响
        }
      }
    } catch (err) {
      message.error('启动 Pipeline 失败');
      console.error(err);
      setIsRunning(false);
    }
  }, [selectedWorkflow, workflowParams, ensureSession, startPolling]);

  // ===== 点击历史记录查看详情 =====
  const handleHistoryClick = useCallback((run: WorkflowRun) => {
    setCurrentRun(run);
    setActiveTab('debug');
    // 同步节点运行状态到画布
    if (run.nodes.length > 0) {
      syncNodeStatusesToStore(run.nodes.map((n) => ({ nodeId: n.name, status: n.status })));
    }
  }, []);

  // ===== 折叠面板 =====
  if (collapsed) {
    return (
      <div className={styles.panelCollapsed}>
        <button className={styles.expandBtn} onClick={() => setCollapsed(false)} title="展开面板">
          <LeftOutlined />
        </button>
      </div>
    );
  }

  return (
    <div className={styles.panel}>
      {/* 头部 */}
      <div className={styles.panelHeader}>
        <div className={styles.panelTitle}>
          <ApartmentOutlined className={styles.panelIcon} />
          Pipeline
        </div>
        <button className={styles.collapseBtn} onClick={() => setCollapsed(true)} title="折叠面板">
          <RightOutlined />
        </button>
      </div>

      {/* Tab 切换 */}
      <div className={styles.tabBar}>
        <button
          className={`${styles.tabItem} ${activeTab === 'select' ? styles.tabActive : ''}`}
          onClick={() => setActiveTab('select')}
        >
          <ApartmentOutlined className={styles.tabIcon} />
          选择
        </button>
        <button
          className={`${styles.tabItem} ${activeTab === 'debug' ? styles.tabActive : ''}`}
          onClick={() => setActiveTab('debug')}
        >
          <PlayCircleOutlined className={styles.tabIcon} />
          调试
        </button>
        <button
          className={`${styles.tabItem} ${activeTab === 'history' ? styles.tabActive : ''}`}
          onClick={() => setActiveTab('history')}
        >
          <HistoryOutlined className={styles.tabIcon} />
          历史
        </button>
      </div>

      {/* 面板内容 */}
      <div className={styles.panelBody}>
        {/* ====== Tab: 选择 Pipeline ====== */}
        {activeTab === 'select' && (
          <>
            {/* Session 状态 */}
            <div className={styles.section}>
              <div className={styles.sectionTitle}>会话</div>
              {sessionId ? (
                <div className={styles.sessionHint}>
                  <CheckCircleOutlined style={{ color: '#10b981', fontSize: 12 }} />
                  <span>已连接会话 {sessionId.slice(-8)}</span>
                </div>
              ) : (
                <Button
                  type="default"
                  size="small"
                  icon={<PlusOutlined />}
                  loading={isCreatingSession}
                  onClick={ensureSession}
                  block
                >
                  创建调试会话
                </Button>
              )}
            </div>

            {/* Pipeline 选择 */}
            <div className={styles.section}>
              <div className={styles.sectionTitle}>选择 Pipeline</div>
              <Select
                value={selectedWorkflow?.id}
                onChange={handleSelectWorkflow}
                options={availableWorkflows.map((wf) => ({
                  label: `${wf.name} — ${wf.description}`,
                  value: wf.id,
                }))}
                loading={isLoadingWorkflows}
                allowClear
                placeholder={sessionId ? '选择 Pipeline' : '请先创建会话'}
                disabled={!sessionId}
                className={styles.select}
              />
            </div>

            {/* Pipeline 列表概览 */}
            {!sessionId && (
              <div className={styles.emptyState}>
                <div className={styles.emptyIcon}>
                  <ApartmentOutlined />
                </div>
                <div className={styles.emptyText}>
                  请先创建调试会话，
                  <br />
                  即可查看和运行 Pipeline
                </div>
              </div>
            )}

            {sessionId && availableWorkflows.length === 0 && !isLoadingWorkflows && (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="当前会话无可用 Pipeline"
                style={{ margin: '24px 0' }}
              />
            )}
          </>
        )}

        {/* ====== Tab: 调试运行 ====== */}
        {activeTab === 'debug' && (
          <>
            {/* 已选 Pipeline 显示 */}
            {!selectedWorkflow ? (
              <div className={styles.emptyState}>
                <div className={styles.emptyIcon}>
                  <PlayCircleOutlined />
                </div>
                <div className={styles.emptyText}>
                  请先在「选择」Tab 中选择一个 Pipeline，
                  <br />
                  或从历史记录中点击查看
                </div>
              </div>
            ) : (
              <>
                {/* Pipeline 名称 */}
                <div className={styles.section}>
                  <div className={styles.sectionTitle}>
                    <ApartmentOutlined style={{ fontSize: 12, color: '#4f46e5' }} />
                    {selectedWorkflow.name}
                  </div>
                  <div style={{ color: 'rgba(0,0,0,0.45)', fontSize: 12 }}>
                    {selectedWorkflow.description}
                  </div>
                </div>

                {/* 参数配置 */}
                {selectedWorkflow.params.length > 0 && (
                  <div className={styles.section}>
                    <div className={styles.sectionTitle}>输入参数</div>
                    <div className={styles.params}>
                      {selectedWorkflow.params.map((p) => (
                        <div key={p.name} className={styles.paramItem}>
                          <label className={styles.paramLabel}>
                            {p.label}
                            {p.required && <span className={styles.paramRequired}>*</span>}
                          </label>
                          {p.type === 'number' ? (
                            <InputNumber
                              value={workflowParams[p.name] as number}
                              onChange={(v) =>
                                setWorkflowParams((prev) => ({ ...prev, [p.name]: v ?? 0 }))
                              }
                              min={p.min}
                              max={p.max}
                              placeholder={p.placeholder}
                              className={styles.paramInput}
                              size="small"
                            />
                          ) : p.type === 'select' && (p.optionsFrom || p.options) ? (
                            <WorkflowParamSelect
                              param={p}
                              value={workflowParams[p.name] as string}
                              onChange={(v) =>
                                setWorkflowParams((prev) => ({ ...prev, [p.name]: v }))
                              }
                              className={styles.select}
                              size="small"
                            />
                          ) : (
                            <Input
                              value={workflowParams[p.name] as string}
                              onChange={(e) =>
                                setWorkflowParams((prev) => ({
                                  ...prev,
                                  [p.name]: e.target.value,
                                }))
                              }
                              placeholder={p.placeholder}
                              className={styles.paramInput}
                              size="small"
                            />
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* 运行按钮 */}
                <div className={styles.runSection}>
                  <Button
                    type="primary"
                    icon={<PlayCircleOutlined />}
                    onClick={handleRunWorkflow}
                    loading={isRunning}
                    disabled={!selectedWorkflow}
                    className={styles.runBtn}
                  >
                    {isRunning ? '运行中...' : '运行 Pipeline'}
                  </Button>
                </div>

                {/* 运行结果 */}
                {currentRun && (
                  <div className={styles.resultSection}>
                    <div className={styles.resultHeader}>
                      <div className={styles.resultTitle}>运行结果</div>
                      <div
                        className={`${styles.resultStatus} ${
                          currentRun.status === 'running'
                            ? styles.resultStatusRunning
                            : currentRun.status === 'completed'
                              ? styles.resultStatusOk
                              : styles.resultStatusErr
                        }`}
                      >
                        {currentRun.status === 'running' && <LoadingOutlined />}
                        {currentRun.status === 'completed' && <CheckCircleOutlined />}
                        {currentRun.status === 'failed' && <ExclamationCircleOutlined />}
                        {currentRun.status === 'running'
                          ? '运行中'
                          : currentRun.status === 'completed'
                            ? '完成'
                            : '失败'}
                      </div>
                    </div>

                    {/* 节点状态列表 */}
                    {currentRun.nodes.length > 0 && (
                      <div className={styles.nodeList}>
                        {currentRun.nodes.map((node) => {
                          const cfg = NODE_STATUS_META[node.status];
                          return (
                            <div key={node.name} className={styles.nodeRow}>
                              <span
                                className={styles.nodeIcon}
                                style={{
                                  color:
                                    cfg.color === 'default'
                                      ? '#999'
                                      : cfg.color === 'processing'
                                        ? '#1677ff'
                                        : cfg.color === 'success'
                                          ? '#10b981'
                                          : '#ef4444',
                                }}
                              >
                                {cfg.icon}
                              </span>
                              <span className={styles.nodeName}>{node.label || node.name}</span>
                              <span
                                className={`${styles.nodeStatus} ${
                                  styles[`nodeStatus_${node.status}`]
                                }`}
                              >
                                {cfg.label}
                              </span>
                              {node.duration != null && (
                                <span className={styles.nodeDuration}>{node.duration}ms</span>
                              )}
                              {node.error && <span className={styles.nodeError}>{node.error}</span>}
                            </div>
                          );
                        })}
                      </div>
                    )}

                    {/* 输出结果 */}
                    {currentRun.status === 'completed' && currentRun.output != null && (
                      <div className={styles.outputSection}>
                        <div className={styles.outputTitle}>输出</div>
                        <pre className={styles.outputPre}>
                          {JSON.stringify(currentRun.output, null, 2)}
                        </pre>
                      </div>
                    )}

                    {/* 错误信息 */}
                    {currentRun.error && <div className={styles.errorBar}>{currentRun.error}</div>}
                  </div>
                )}
              </>
            )}
          </>
        )}

        {/* ====== Tab: 运行历史 ====== */}
        {activeTab === 'history' && (
          <>
            {isLoadingRuns && (
              <Spin size="small" style={{ display: 'block', margin: '16px auto' }} />
            )}
            {!isLoadingRuns && workflowRuns.length === 0 && (
              <div className={styles.historyEmpty}>暂无运行记录</div>
            )}
            <div className={styles.historyList}>
              {workflowRuns.map((run) => (
                <div
                  key={run.runId}
                  className={`${styles.historyItem} ${
                    currentRun?.runId === run.runId ? styles.historyItemActive : ''
                  }`}
                  onClick={() => handleHistoryClick(run)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleHistoryClick(run);
                  }}
                >
                  <div className={styles.historyHead}>
                    <span className={styles.historyName}>{run.workflowName}</span>
                    <Tag
                      color={
                        run.status === 'completed'
                          ? 'success'
                          : run.status === 'failed'
                            ? 'error'
                            : 'processing'
                      }
                      style={{ fontSize: 10, height: 16, lineHeight: '14px', margin: 0 }}
                    >
                      {run.status === 'running'
                        ? '运行中'
                        : run.status === 'completed'
                          ? '完成'
                          : '失败'}
                    </Tag>
                  </div>
                  <div className={styles.historyRunId}>ID: {run.runId.slice(-8)}</div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

export default WorkflowPanel;
