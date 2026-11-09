import type { ReactNode } from 'react';
import { GOODS_SCRAPER_TASK_TYPE } from '@/services/keffectsTasks';
import GoodsScraperForm from './GoodsScraperForm';
import GoodsScraperResults from './GoodsScraperResults';
import KeffectsStudioShell from './KeffectsStudioShell';
import KeffectsTaskPanel from './KeffectsTaskPanel';
import { useGoodsScraperTask } from './useGoodsScraperTask';
import { useKeffectsPipelineHistory } from './useKeffectsPipelineHistory';
import { useStudioTemplates } from '../form/useStudioTemplates';
import type { KeffectsStudioHistoryProps } from './keffectsStudioSessionProps';

interface GoodsScraperStudioProps extends KeffectsStudioHistoryProps {
  header: ReactNode;
}

function GoodsScraperStudio({ header, pipelineName }: GoodsScraperStudioProps) {
  const history = useKeffectsPipelineHistory({ pipelineName });
  const model = useGoodsScraperTask({
    onRefreshHistory: history.refresh,
    onFocusTask: history.focusTask,
  });
  const templates = useStudioTemplates(
    pipelineName?.trim() || GOODS_SCRAPER_TASK_TYPE,
    model.applyTemplateExtra,
  );

  return (
    <KeffectsStudioShell
      header={header}
      formAria="商品详情抓取参数"
      processAria="商品详情抓取执行过程"
      form={<GoodsScraperForm model={model} templates={templates} />}
      process={
        <KeffectsTaskPanel
          task={model.task}
          emptyDescription="点击开始抓取后，将在此展示本次任务的执行过程"
          onCancel={model.cancel}
          cancelling={model.cancelling}
        />
      }
      results={<GoodsScraperResults model={model} />}
      isBusy={model.isBusy}
      outputCount={model.outputCount}
      foldBusyText="正在抓取，结果在下方"
      pipelineHistory={{
        history,
        task: model.task,
        activePrompt: model.url,
        onSelectExec: model.restoreFromExec,
      }}
    />
  );
}

export default GoodsScraperStudio;
