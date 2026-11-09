import { Button, Empty, Progress, Tag } from 'antd';
import { StopOutlined } from '@ant-design/icons';
import { isKeffectsTaskActive, type KeffectsTask } from '@/services/keffectsTasks';
import { formatTaskTime, resolveTaskStatusMeta, toTaskProgressStatus } from './constants';
import { resolveTaskProgressPercent, shouldShowTaskProgress } from './keffectsTaskRuntime';
import studio from '../CreationStudio.module.less';

interface KeffectsTaskPanelProps {
  task: KeffectsTask | null;
  emptyDescription: string;
  cancelling?: boolean;
  onCancel?: () => void;
}

function KeffectsTaskPanel({
  task,
  emptyDescription,
  cancelling = false,
  onCancel,
}: KeffectsTaskPanelProps) {
  const status = resolveTaskStatusMeta(task?.status);
  const active = isKeffectsTaskActive(task?.status);
  const canCancel = Boolean(onCancel && (active || cancelling || task?.status === 'CANCELLING'));
  const showProgress = shouldShowTaskProgress(task?.status);

  return (
    <section className={studio.runPanel}>
      <div className={studio.columnHead}>
        <span className={studio.columnEyebrow}>KEFFECTS</span>
        <h2 className={studio.columnTitle}>执行过程</h2>
        {status ? (
          <Tag color={status.color} className={studio.runStatusTag}>
            {status.text}
          </Tag>
        ) : null}
      </div>

      {!task ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyDescription} />
      ) : (
        <>
          {showProgress ? (
            <Progress
              percent={resolveTaskProgressPercent(task)}
              status={toTaskProgressStatus(task.status)}
            />
          ) : null}

          <dl className={studio.runMeta}>
            <div>
              <dt>taskId</dt>
              <dd className={studio.runMono}>{task.taskId}</dd>
            </div>
            <div>
              <dt>requestId</dt>
              <dd className={studio.runMono}>{task.requestId || '—'}</dd>
            </div>
            <div>
              <dt>当前阶段</dt>
              <dd>{task.stage || '—'}</dd>
            </div>
            <div>
              <dt>更新</dt>
              <dd>{formatTaskTime(task.updateTime)}</dd>
            </div>
          </dl>

          {task.error ? (
            <pre className={studio.runError}>
              {task.error.code ? `${task.error.code}: ` : ''}
              {task.error.message || '任务执行失败'}
            </pre>
          ) : null}

          {canCancel ? (
            <div>
              <Button
                danger
                icon={<StopOutlined />}
                loading={cancelling}
                onClick={() => void onCancel?.()}
              >
                取消任务
              </Button>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

export default KeffectsTaskPanel;
