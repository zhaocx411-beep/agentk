/**
 * 一键换装工作台：布局对齐官方工作流 CreationStudio，
 * 但执行链路走 keffects clothing-dupe 任务接口（create / query / cancel）
 */
import { useCallback, useRef, type ReactNode } from 'react';
import { WorkbenchLayout, WorkbenchPane } from '@/components';
import { CLOTHING_DUPE_TASK_TYPE } from '@/services/clothingDupe';
import ClothingDupeForm from './ClothingDupeForm';
import ClothingDupeTaskPanel, { ClothingDupeResults } from './ClothingDupeTaskPanel';
import { useClothingDupeTask } from './useClothingDupeTask';
import { useStudioTemplates } from '../form/useStudioTemplates';
import KeffectsPipelineHistory from '../keffects/KeffectsPipelineHistory';
import { StudioResultsTopHint } from '../StudioResultsTopHint';
import { useKeffectsPipelineHistory } from '../keffects/useKeffectsPipelineHistory';
import type { KeffectsStudioHistoryProps } from '../keffects/keffectsStudioSessionProps';
import studio from '../CreationStudio.module.less';

interface ClothingDupeStudioProps extends KeffectsStudioHistoryProps {
  header: ReactNode;
}

function ClothingDupeStudio({ header, pipelineName }: ClothingDupeStudioProps) {
  const history = useKeffectsPipelineHistory({ pipelineName });
  const model = useClothingDupeTask({
    onRefreshHistory: history.refresh,
    onFocusTask: history.focusTask,
  });
  const templates = useStudioTemplates(
    pipelineName?.trim() || CLOTHING_DUPE_TASK_TYPE,
    model.applyTemplateExtra,
  );
  const resultsRef = useRef<HTMLElement>(null);
  const outputCount = model.outputEntries.length;

  const scrollToResults = useCallback(() => {
    resultsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  return (
    <div className={studio.studio}>
      <StudioResultsTopHint
        isBusy={model.isBusy}
        count={outputCount}
        countPrefix="已返回"
        countUnit="产物"
        onScrollToResults={scrollToResults}
      />
      <div className={studio.studioTop}>
        <div className={studio.studioWorkbench}>
          <WorkbenchLayout variant="two-pane" className={studio.studioLayout}>
            <WorkbenchPane
              header={header}
              ariaLabel="一键换装创作参数"
              contentClassName={studio.formPaneContent}
            >
              <ClothingDupeForm model={model} templates={templates} />
            </WorkbenchPane>
            <WorkbenchPane
              header={<h2 className={studio.resultHeaderTitle}>过程与记录</h2>}
              ariaLabel="一键换装执行过程"
              contentClassName={studio.sidePaneContent}
            >
              <div className={studio.sideColumn}>
                <div className={studio.sideRun}>
                  <ClothingDupeTaskPanel model={model} />
                </div>
                <KeffectsPipelineHistory
                  history={history}
                  task={model.task}
                  activePrompt={model.prompt || model.sourceVideo[0]?.name}
                  onSelectExec={model.restoreFromExec}
                />
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
        <div className={studio.studioResultsBody}>
          <ClothingDupeResults model={model} />
        </div>
      </section>
    </div>
  );
}

export default ClothingDupeStudio;
