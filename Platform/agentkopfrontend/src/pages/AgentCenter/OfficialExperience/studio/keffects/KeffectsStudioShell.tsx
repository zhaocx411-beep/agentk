/**
 * keffects 任务工作台外壳：左表单 + 右过程，下方生成结果
 */
import { useCallback, useRef, type ReactNode } from 'react';
import { WorkbenchLayout, WorkbenchPane } from '@/components';
import studio from '../CreationStudio.module.less';
import { StudioResultsTopHint } from '../StudioResultsTopHint';
import { withExperienceRunStatus } from '../../experienceHeaderUtils';
import KeffectsPipelineHistory, {
  type KeffectsPipelineHistoryProps,
} from './KeffectsPipelineHistory';

interface KeffectsStudioShellProps {
  header: ReactNode;
  formAria: string;
  processAria: string;
  form: ReactNode;
  process: ReactNode;
  results: ReactNode;
  isBusy: boolean;
  outputCount: number;
  foldBusyText?: string;
  pipelineHistory: KeffectsPipelineHistoryProps;
}

function KeffectsStudioShell({
  header,
  formAria,
  processAria,
  form,
  process,
  results,
  isBusy,
  outputCount,
  foldBusyText = '正在生成，成品在下方',
  pipelineHistory,
}: KeffectsStudioShellProps) {
  const resultsRef = useRef<HTMLElement>(null);

  const scrollToResults = useCallback(() => {
    resultsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  return (
    <div className={studio.studio}>
      <StudioResultsTopHint
        isBusy={isBusy}
        count={outputCount}
        busyText={foldBusyText}
        countPrefix="已返回"
        countUnit="产物"
        onScrollToResults={scrollToResults}
      />
      <div className={studio.studioTop}>
        <div className={studio.studioWorkbench}>
          <WorkbenchLayout variant="two-pane" className={studio.studioLayout}>
            <WorkbenchPane
              header={withExperienceRunStatus(header, {
                isRunning: isBusy,
                hasError: false,
              })}
              ariaLabel={formAria}
              contentClassName={studio.formPaneContent}
            >
              {form}
            </WorkbenchPane>
            <WorkbenchPane
              header={<h2 className={studio.resultHeaderTitle}>过程与记录</h2>}
              ariaLabel={processAria}
              contentClassName={studio.sidePaneContent}
            >
              <div className={studio.sideColumn}>
                <div className={studio.sideRun}>{process}</div>
                <KeffectsPipelineHistory {...pipelineHistory} />
              </div>
            </WorkbenchPane>
          </WorkbenchLayout>
        </div>
      </div>

      <section ref={resultsRef} className={studio.studioResults} aria-label="生成结果">
        <header className={studio.studioResultsHead}>
          <h2 className={studio.resultHeaderTitle}>生成结果</h2>
          {outputCount > 0 ? (
            <span className={studio.studioResultsMeta}>{outputCount} 个产物</span>
          ) : null}
        </header>
        <div className={studio.studioResultsBody}>{results}</div>
      </section>
    </div>
  );
}

export default KeffectsStudioShell;
