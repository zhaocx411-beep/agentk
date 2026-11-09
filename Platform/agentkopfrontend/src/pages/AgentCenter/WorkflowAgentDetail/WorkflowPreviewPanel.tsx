/**
 * 工作流 Agent 右侧预览：展示对齐 Pipeline 运行页 WorkflowRunCard
 */
import { useState } from 'react';
import { Button, Segmented, Select, Spin } from 'antd';
import { PlayCircleOutlined } from '@ant-design/icons';
import { WorkbenchPane } from '@/components';
import WorkflowRunCard from '@/pages/WorkflowRunner/components/WorkflowRunCard';
import type { Session, WorkflowDefinition, WorkflowRun } from '@/pages/AgentPlayground/types';
import SessionHistoryList from '../AgentDetail/components/SessionHistoryList';
import WorkflowRunParams from './WorkflowRunParams';
import WorkflowRunEmptyState from './WorkflowRunEmptyState';
import detailStyles from '../AgentDetail/AgentDetail.module.less';
import styles from './WorkflowAgentDetail.module.less';

type PreviewTab = 'run' | 'history';

interface WorkflowPreviewPanelProps {
  versionMap: Record<number, number>;
  currentVersionId?: number;
  sessionId: string | null;
  isSessionReady: boolean;
  sessions: Session[];
  sessionsLoading: boolean;
  isLoadingMoreSessions?: boolean;
  historyListRef?: React.RefObject<HTMLDivElement>;
  onHistoryScroll?: () => void;
  onSwitchSession: (sid: string) => void;
  onSessionTitleUpdated?: (sessionId: string, title: string) => void;
  availableWorkflows: WorkflowDefinition[];
  selectedWorkflow: WorkflowDefinition | null;
  showPipelineSelect: boolean;
  onSelectWorkflow: (id: string) => void;
  workflowParams: Record<string, string | number>;
  setWorkflowParams: React.Dispatch<React.SetStateAction<Record<string, string | number>>>;
  paramMode: 'form' | 'json';
  setParamMode: (mode: 'form' | 'json') => void;
  rawArgsJson: string;
  setRawArgsJson: (text: string) => void;
  activeRun: WorkflowRun | null;
  isRunning: boolean;
  hasRunningWorkflow: boolean;
  onRun: () => void;
}

function WorkflowPreviewPanel({
  versionMap,
  currentVersionId,
  sessionId,
  isSessionReady,
  sessions,
  sessionsLoading,
  isLoadingMoreSessions,
  historyListRef,
  onHistoryScroll,
  onSwitchSession,
  onSessionTitleUpdated,
  availableWorkflows,
  selectedWorkflow,
  showPipelineSelect,
  onSelectWorkflow,
  workflowParams,
  setWorkflowParams,
  paramMode,
  setParamMode,
  rawArgsJson,
  setRawArgsJson,
  activeRun,
  isRunning,
  hasRunningWorkflow,
  onRun,
}: WorkflowPreviewPanelProps) {
  const [tab, setTab] = useState<PreviewTab>('run');

  return (
    <WorkbenchPane
      tone="muted"
      ariaLabel="工作流预览与调试"
      className={detailStyles.previewPanel}
      contentClassName={detailStyles.previewPaneContent}
      header={
        <div className={detailStyles.previewHeaderContent}>
          <span className={detailStyles.previewTitle}>预览与调试</span>
          <Segmented<PreviewTab>
            size="small"
            value={tab}
            onChange={setTab}
            options={[
              { value: 'run', label: '运行' },
              { value: 'history', label: '历史会话' },
            ]}
          />
        </div>
      }
    >
      {tab === 'history' ? (
        <SessionHistoryList
          versionMap={versionMap}
          currentVersionId={currentVersionId}
          currentSessionId={sessionId}
          sessions={sessions}
          loading={sessionsLoading}
          isLoadingMoreSessions={isLoadingMoreSessions}
          historyListRef={historyListRef}
          onHistoryScroll={onHistoryScroll}
          onOpenSession={(sid) => {
            void onSwitchSession(sid);
            setTab('run');
          }}
          onSessionTitleUpdated={onSessionTitleUpdated}
        />
      ) : (
        <>
          <div className={styles.runBody}>
            {!isSessionReady ? (
              <div className={styles.runLoading}>
                <Spin />
                <span>正在初始化预览会话...</span>
              </div>
            ) : activeRun ? (
              <WorkflowRunCard run={activeRun} />
            ) : (
              <WorkflowRunEmptyState
                workflowName={selectedWorkflow?.name}
                needsPipelineSelect={showPipelineSelect}
              />
            )}
          </div>
          <div className={styles.runFooter}>
            {showPipelineSelect && (
              <Select
                size="small"
                className={styles.pipelineSelect}
                value={selectedWorkflow?.id}
                onChange={onSelectWorkflow}
                options={availableWorkflows.map((wf) => ({
                  label: wf.name,
                  value: wf.id,
                }))}
                placeholder="选择 Pipeline"
              />
            )}
            <WorkflowRunParams
              selectedWorkflow={selectedWorkflow}
              workflowParams={workflowParams}
              setWorkflowParams={setWorkflowParams}
              paramMode={paramMode}
              setParamMode={setParamMode}
              rawArgsJson={rawArgsJson}
              setRawArgsJson={setRawArgsJson}
            />
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              block
              loading={isRunning}
              disabled={!selectedWorkflow || isRunning || hasRunningWorkflow || !isSessionReady}
              onClick={onRun}
            >
              运行 Pipeline
            </Button>
          </div>
        </>
      )}
    </WorkbenchPane>
  );
}

export default WorkflowPreviewPanel;
