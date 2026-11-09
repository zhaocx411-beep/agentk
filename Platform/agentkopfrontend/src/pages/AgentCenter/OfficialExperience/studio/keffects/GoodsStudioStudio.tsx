import type { ReactNode } from 'react';
import { GOODS_STUDIO_TASK_TYPE } from '@/services/keffectsTasks';
import GoodsStudioForm from './GoodsStudioForm';
import GoodsStudioResults from './GoodsStudioResults';
import KeffectsStudioShell from './KeffectsStudioShell';
import KeffectsTaskPanel from './KeffectsTaskPanel';
import { useGoodsStudioTask } from './useGoodsStudioTask';
import { useKeffectsPipelineHistory } from './useKeffectsPipelineHistory';
import { useStudioTemplates } from '../form/useStudioTemplates';
import type { KeffectsStudioHistoryProps } from './keffectsStudioSessionProps';

interface GoodsStudioStudioProps extends KeffectsStudioHistoryProps {
  header: ReactNode;
}

function GoodsStudioStudio({ header, pipelineName }: GoodsStudioStudioProps) {
  const history = useKeffectsPipelineHistory({ pipelineName });
  const model = useGoodsStudioTask({
    onRefreshHistory: history.refresh,
    onFocusTask: history.focusTask,
  });
  const templates = useStudioTemplates(
    pipelineName?.trim() || GOODS_STUDIO_TASK_TYPE,
    model.applyTemplateExtra,
  );

  return (
    <KeffectsStudioShell
      header={header}
      formAria="电商商品广告成片参数"
      processAria="电商商品广告成片执行过程"
      form={<GoodsStudioForm model={model} templates={templates} />}
      process={
        <KeffectsTaskPanel
          task={model.task}
          emptyDescription="点击生成后，将在此展示本次广告成片任务的执行过程"
          onCancel={model.cancel}
          cancelling={model.cancelling}
        />
      }
      results={<GoodsStudioResults model={model} />}
      isBusy={model.isBusy}
      outputCount={model.outputCount}
      pipelineHistory={{
        history,
        task: model.task,
        activePrompt: model.title,
        onSelectExec: model.restoreFromExec,
      }}
    />
  );
}

export default GoodsStudioStudio;
