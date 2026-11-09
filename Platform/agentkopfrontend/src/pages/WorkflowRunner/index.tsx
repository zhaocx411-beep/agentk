/**
 * PipelineRunner - Pipeline 独立运行页面
 * 左侧：选择/配置/运行 + 持久化（新建/存草稿/版本管理）+ 运行历史 + 历史会话；
 * 右侧：纯输出区（无对话形态）
 */
import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Empty, Spin } from 'antd';
import { ApartmentOutlined } from '@ant-design/icons';
import { useWorkflowRunner } from './hooks/useWorkflowRunner';
import ConfigPanel from './components/ConfigPanel';
import WorkflowRunCard from './components/WorkflowRunCard';
import NewWorkflowModal from './components/NewWorkflowModal';
import SaveWorkflowDraftModal from './components/SaveWorkflowDraftModal';
import WorkflowVersionDrawer from './components/WorkflowVersionDrawer';
import { CAPABILITIES_QUERY_KEY, fetchCapabilities } from '@/services/workflow';
import { buildDefinitionSkeleton } from '@/utils/workflowDefinition';
import styles from './WorkflowRunner.module.less';

function WorkflowRunner() {
  const {
    sessionId,
    isSessionReady,
    sessions,
    isLoadingMoreSessions,
    historyListRef,
    handleHistoryScroll,
    handleSwitchSession,
    updateSessionTitle,
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
    hasRunningWorkflow,
    handleSelectWorkflow,
    handleRunWorkflow,
    handleViewRun,
    handleRunPersistedVersion,
    sessionFilterLabel,
    clearSessionFilter,
  } = useWorkflowRunner();

  // ===== 持久化弹窗状态 =====
  const [newWorkflowOpen, setNewWorkflowOpen] = useState(false);
  const [saveDraftOpen, setSaveDraftOpen] = useState(false);
  const [versionManagerOpen, setVersionManagerOpen] = useState(false);

  /** 骨架兜底：由当前选中的 workflow 生成（inputs 重建，nodes 留空） */
  const definitionSkeleton = useMemo(
    () => buildDefinitionSkeleton(selectedWorkflow),
    [selectedWorkflow],
  );

  /** 实例能力目录：新建弹窗的 Pipeline 下拉数据，弹窗打开时才拉取。
   *  能力目录只给契约不给完整定义，definitionJson 一律由骨架起步 */
  const { data: catalog } = useQuery({
    queryKey: CAPABILITIES_QUERY_KEY,
    queryFn: fetchCapabilities,
    enabled: newWorkflowOpen || saveDraftOpen,
  });

  return (
    <div className={styles.page}>
      <ConfigPanel
        sessionId={sessionId}
        sessionReady={isSessionReady}
        sessions={sessions}
        isLoadingMoreSessions={isLoadingMoreSessions}
        historyListRef={historyListRef}
        onHistoryScroll={handleHistoryScroll}
        onSwitchSession={handleSwitchSession}
        onSessionTitleUpdated={updateSessionTitle}
        sessionFilterLabel={sessionFilterLabel}
        onClearSessionFilter={clearSessionFilter}
        availableWorkflows={availableWorkflows}
        isLoadingWorkflows={isLoadingWorkflows}
        selectedWorkflow={selectedWorkflow}
        workflowParams={workflowParams}
        setWorkflowParams={setWorkflowParams}
        paramMode={paramMode}
        setParamMode={setParamMode}
        rawArgsJson={rawArgsJson}
        setRawArgsJson={setRawArgsJson}
        onSelectWorkflow={handleSelectWorkflow}
        onRunWorkflow={handleRunWorkflow}
        isRunning={isRunning}
        hasRunningWorkflow={hasRunningWorkflow}
        workflowRuns={workflowRuns}
        activeRunId={activeRun?.runId ?? null}
        onViewRun={handleViewRun}
        onOpenSaveDraft={() => setSaveDraftOpen(true)}
        onOpenVersionManager={() => setVersionManagerOpen(true)}
        onOpenNewWorkflow={() => setNewWorkflowOpen(true)}
      />

      {/* 输出区：只展示运行结果，无对话形态 */}
      <main className={styles.output}>
        {!isSessionReady ? (
          <div className={styles.outputLoading}>
            <Spin size="large" />
            <span className={styles.outputLoadingText}>正在初始化会话...</span>
          </div>
        ) : activeRun ? (
          <div className={styles.outputBody}>
            <WorkflowRunCard run={activeRun} />
          </div>
        ) : (
          <Empty
            image={<ApartmentOutlined style={{ fontSize: 48, color: '#d9d9d9' }} />}
            description={
              workflowRuns.length > 0
                ? '从左侧运行历史中选择一条记录查看输出'
                : '选择 Pipeline 并运行，输出结果将展示在这里'
            }
            className={styles.outputEmpty}
          />
        )}
      </main>

      {/* 持久化：新建 Pipeline / 存为草稿 / 版本管理 */}
      <NewWorkflowModal
        open={newWorkflowOpen}
        defaultDefinitionJson={definitionSkeleton}
        pipelines={catalog?.pipelines ?? []}
        onClose={() => setNewWorkflowOpen(false)}
      />
      <SaveWorkflowDraftModal
        open={saveDraftOpen}
        defaultDefinitionJson={definitionSkeleton}
        onClose={() => setSaveDraftOpen(false)}
      />
      <WorkflowVersionDrawer
        open={versionManagerOpen}
        onClose={() => setVersionManagerOpen(false)}
        onRunVersion={handleRunPersistedVersion}
      />
    </div>
  );
}

export default WorkflowRunner;
