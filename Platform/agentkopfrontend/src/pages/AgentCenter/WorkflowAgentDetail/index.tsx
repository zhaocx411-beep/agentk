/**
 * 工作流 Agent 详情：布局对齐自主规划详情，右侧展示对齐 Pipeline 运行页
 */
import { Empty, Spin } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { WorkbenchLayout, WorkbenchPane } from '@/components';
import WorkflowDetailHeader from './WorkflowDetailHeader';
import WorkflowConfigPanel from './WorkflowConfigPanel';
import WorkflowPreviewPanel from './WorkflowPreviewPanel';
import { useWorkflowAgentModel } from './useWorkflowAgentModel';
import { useWorkflowAgentPreview } from './useWorkflowAgentPreview';
import styles from '../AgentDetail/AgentDetail.module.less';

function WorkflowAgentDetail() {
  const { workflowId = '' } = useParams();
  const navigate = useNavigate();
  const numericId = Number(workflowId);
  const model = useWorkflowAgentModel(numericId);
  const preview = useWorkflowAgentPreview(
    model.entity?.name ?? '',
    model.selectedVersion,
    model.definitionJson,
  );

  if (!Number.isFinite(numericId)) {
    return (
      <div className={styles.loading}>
        <Empty description="无效的工作流 Agent">
          <a onClick={() => navigate('/agents/mine')}>返回列表</a>
        </Empty>
      </div>
    );
  }

  if (model.loading && !model.entity) {
    return (
      <div className={styles.loading}>
        <Spin size="large" />
      </div>
    );
  }

  if (!model.entity) {
    return (
      <div className={styles.loading}>
        <Empty description="未找到该工作流 Agent">
          <a onClick={() => navigate('/agents/mine')}>返回列表</a>
        </Empty>
      </div>
    );
  }

  return (
    <WorkbenchLayout variant="two-pane" className={styles.page}>
      <WorkbenchPane
        ariaLabel="工作流配置"
        header={
          <WorkflowDetailHeader
            name={model.entity.name}
            description={model.entity.description}
            status={model.status}
            versions={model.versions}
            selectedVersionId={model.selectedVersion?.id}
            dirty={model.dirty}
            saving={model.saveMutation.isPending}
            publishing={model.publishMutation.isPending}
            autoSavedAt={model.autoSavedAt}
            onVersionChange={model.handleVersionChange}
            onSaveDraft={() => model.saveMutation.mutate()}
            onPublish={model.handlePublish}
          />
        }
      >
        <WorkflowConfigPanel
          definitionJson={model.definitionJson}
          onChange={model.setDefinitionJson}
        />
      </WorkbenchPane>
      <WorkflowPreviewPanel
        versionMap={model.versionMap}
        currentVersionId={model.selectedVersion?.id}
        sessionId={preview.sessionId}
        isSessionReady={preview.isSessionReady}
        sessions={preview.sessions}
        sessionsLoading={preview.sessionsLoading}
        isLoadingMoreSessions={preview.isLoadingMoreSessions}
        historyListRef={preview.historyListRef}
        onHistoryScroll={preview.handleHistoryScroll}
        onSwitchSession={(sid) => void preview.handleSwitchSession(sid)}
        onSessionTitleUpdated={preview.updateSessionTitle}
        availableWorkflows={preview.availableWorkflows}
        selectedWorkflow={preview.selectedWorkflow}
        showPipelineSelect={preview.showPipelineSelect}
        onSelectWorkflow={preview.handleSelectWorkflow}
        workflowParams={preview.workflowParams}
        setWorkflowParams={preview.setWorkflowParams}
        paramMode={preview.paramMode}
        setParamMode={preview.setParamMode}
        rawArgsJson={preview.rawArgsJson}
        setRawArgsJson={preview.setRawArgsJson}
        activeRun={preview.activeRun}
        isRunning={preview.isRunning}
        hasRunningWorkflow={preview.hasRunningWorkflow}
        onRun={() => void preview.handleRunWorkflow()}
      />
    </WorkbenchLayout>
  );
}

export default WorkflowAgentDetail;
