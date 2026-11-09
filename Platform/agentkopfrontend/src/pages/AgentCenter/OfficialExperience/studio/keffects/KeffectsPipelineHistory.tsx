/**
 * keffects 工作台「我的生成」：pipeline-execs 历史列表
 */
import { useCallback, useMemo } from 'react';
import type { KeffectsTask } from '@/services/keffectsTasks';
import type { PipelineExecListRecord } from '@/services/pipelineExecs';
import GenerationHistory from '../pipeline/GenerationHistory';
import { statusFromKeffectsTask } from './keffectsTaskStatus';
import {
  recordMatchesTaskId,
  type useKeffectsPipelineHistory,
} from './useKeffectsPipelineHistory';
import studio from '../CreationStudio.module.less';

type PipelineHistoryModel = ReturnType<typeof useKeffectsPipelineHistory>;

export interface KeffectsPipelineHistoryProps {
  history: PipelineHistoryModel;
  task: KeffectsTask | null;
  /** 当前任务摘要（如 URL / 标题），列表尚未回时占位展示 */
  activePrompt?: string;
  onSelectExec: (exec: PipelineExecListRecord) => void;
}

function KeffectsPipelineHistory({
  history,
  task,
  activePrompt,
  onSelectExec,
}: KeffectsPipelineHistoryProps) {
  const records = useMemo(() => {
    const list = history.records.map((record) => {
      if (!task?.taskId || !recordMatchesTaskId(record, task.taskId)) return record;
      const liveStatus = statusFromKeffectsTask(task);
      if (liveStatus == null) return record;
      return { ...record, status: liveStatus };
    });

    if (!task?.taskId || !statusFromKeffectsTask(task)) return list;
    if (list.some((record) => recordMatchesTaskId(record, task.taskId))) return list;

    const prompt = activePrompt?.trim() || `任务 ${task.taskId.slice(-8)}`;
    list.unshift({
      id: task.taskId,
      runId: task.taskId,
      templateName: prompt,
      scene: '当前',
      prompt,
      intentPreview: '当前正在执行的任务。',
      payloadPreview: prompt,
      status: statusFromKeffectsTask(task),
      createdAt: task.createTime ?? Date.now(),
    });
    return list;
  }, [activePrompt, history.records, task]);

  const handleSelect = useCallback(
    (id: string) => {
      history.selectRecord(id);
      const exec = history.findExec(id);
      if (exec) onSelectExec(exec);
    },
    [history, onSelectExec],
  );

  return (
    <div className={studio.sideHistory}>
      <GenerationHistory
        records={records}
        selectedId={history.selectedId}
        onSelect={handleSelect}
        enableScrollPaging
        sessionsLoading={history.loading}
        isLoadingMoreSessions={history.isLoadingMore}
        sessionsHasMore={history.hasMore}
        historyListRef={history.historyListRef}
        onHistoryScroll={history.handleHistoryScroll}
      />
    </div>
  );
}

export default KeffectsPipelineHistory;
