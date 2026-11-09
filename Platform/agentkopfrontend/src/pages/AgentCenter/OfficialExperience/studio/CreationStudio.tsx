/**
 * 创作工作台：上屏左表单 + 右过程/记录，下屏生成结果，整页滚动
 */
import { useCallback, useRef, type ReactNode, type RefObject } from 'react';
import { WorkbenchLayout, WorkbenchPane } from '@/components';
import type { Session } from '@/pages/AgentPlayground/types';
import type { SSEActions, SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import type { OfficialUiField } from '@/types/officialCatalog';
import type { OfficialCatalogType } from '../../types';
import ArtifactDock from './pipeline/ArtifactDock';
import CreationForm from './form/CreationForm';
import GenerationHistory from './pipeline/GenerationHistory';
import PipelinePanel from './pipeline/PipelinePanel';
import PipelineRunPanel from './pipeline/PipelineRunPanel';
import TemplatePicker from './form/TemplatePicker';
import { useCreationStudio } from './hooks/useCreationStudio';
import { StudioResultsTopHint } from './StudioResultsTopHint';
import { withExperienceRunStatus } from '../experienceHeaderUtils';
import styles from './CreationStudio.module.less';

interface CreationStudioProps {
  header: ReactNode;
  sessionId: string;
  catalogType: OfficialCatalogType;
  workflowKey?: string;
  pipelineName?: string;
  schemaFields?: OfficialUiField[];
  agentLoopId?: number;
  sessions: Session[];
  historyLoading: boolean;
  onOpenSession: (sessionId: string) => void;
  onRefreshSessions: () => void;
  enableSessionPaging?: boolean;
  sessionsLoading?: boolean;
  isLoadingMoreSessions?: boolean;
  sessionsHasMore?: boolean;
  historyListRef?: RefObject<HTMLDivElement>;
  onHistoryScroll?: () => void;
  sseState: SSEState;
  sseActions: SSEActions;
}

function CreationStudio({
  header,
  sessionId,
  catalogType,
  workflowKey,
  pipelineName,
  schemaFields,
  agentLoopId,
  sessions,
  historyLoading,
  onOpenSession,
  onRefreshSessions,
  enableSessionPaging = false,
  sessionsLoading = false,
  isLoadingMoreSessions = false,
  sessionsHasMore = false,
  historyListRef,
  onHistoryScroll,
  sseState,
  sseActions,
}: CreationStudioProps) {
  const model = useCreationStudio({
    sessionId,
    catalogType,
    workflowKey,
    pipelineName,
    schemaFields,
    agentLoopId,
    sessions,
    historyLoading,
    onOpenSession,
    onRefreshSessions,
    sseState,
    sseActions,
  });
  const resultsRef = useRef<HTMLElement>(null);
  const isPipeline = model.spec.mode === 'pipeline';
  const artifactCount = model.artifacts.length;

  const scrollToResults = useCallback(() => {
    resultsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  const studioHeader = withExperienceRunStatus(header, {
    isRunning: model.isBusy,
    hasPendingJobs: Boolean(model.waitingJobs),
    hasError: model.pipelineRunStatus === 'failed',
  });

  return (
    <div className={styles.studio}>
      <StudioResultsTopHint
        isBusy={model.isBusy}
        count={artifactCount}
        onScrollToResults={scrollToResults}
      />
      <div className={styles.studioTop}>
        <div className={styles.studioWorkbench}>
          <WorkbenchLayout variant="two-pane" className={styles.studioLayout}>
            <WorkbenchPane
              header={studioHeader}
              ariaLabel="工作流创作参数"
              contentClassName={styles.formPaneContent}
            >
              <div className={styles.formColumn}>
                <TemplatePicker model={model} />
                <CreationForm model={model} />
              </div>
            </WorkbenchPane>
            <WorkbenchPane
              header={<h2 className={styles.resultHeaderTitle}>过程与记录</h2>}
              ariaLabel="执行过程与我的生成"
              contentClassName={styles.sidePaneContent}
            >
              <div className={styles.sideColumn}>
                <div className={styles.sideRun}>
                  {isPipeline ? (
                    <PipelineRunPanel
                      detail={model.pipelineDetail}
                      nodes={model.nodes}
                      progress={model.progress}
                      loading={model.pipelineRunsLoading}
                    />
                  ) : (
                    <PipelinePanel
                      nodes={model.nodes}
                      activeKey={null}
                      onSelect={() => undefined}
                      progress={model.progress}
                    />
                  )}
                </div>
                <div className={styles.sideHistory}>
                  <GenerationHistory
                    records={model.records}
                    selectedId={model.selectedRecordId}
                    onSelect={model.selectRecord}
                    statusSessionId={sessionId}
                    enableScrollPaging={enableSessionPaging}
                    sessionsLoading={sessionsLoading}
                    isLoadingMoreSessions={isLoadingMoreSessions}
                    sessionsHasMore={sessionsHasMore}
                    historyListRef={historyListRef}
                    onHistoryScroll={onHistoryScroll}
                  />
                </div>
              </div>
            </WorkbenchPane>
          </WorkbenchLayout>
        </div>
      </div>

      <section ref={resultsRef} className={styles.studioResults} aria-label="生成结果">
        <header className={styles.studioResultsHead}>
          <h2 className={styles.resultHeaderTitle}>生成结果</h2>
          {artifactCount > 0 ? (
            <span className={styles.studioResultsMeta}>{artifactCount} 个成品</span>
          ) : null}
        </header>
        <div className={styles.studioResultsBody}>
          <ArtifactDock artifacts={model.artifacts} isBusy={model.isBusy} />
        </div>
      </section>
    </div>
  );
}

export default CreationStudio;
