import { Alert, Button, Empty, Progress, Tag } from 'antd';
import { StopOutlined } from '@ant-design/icons';
import type { ClothingDupeOutputAsset } from '@/services/clothingDupe';
import { resolveClothingDupeAssetUrl } from '@/services/clothingDupe';
import ArtifactDock from '../pipeline/ArtifactDock';
import {
  formatTaskTime,
  resolveTaskStatusMeta,
  toTaskProgressStatus,
} from '../keffects/constants';
import type { ArtifactItem } from '../lib/types';
import type { ClothingDupeModel } from './useClothingDupeTask';
import studio from '../CreationStudio.module.less';
import styles from './ClothingDupeStudio.module.less';

interface ClothingDupeTaskPanelProps {
  model: ClothingDupeModel;
}

function assetInfo(value: ClothingDupeOutputAsset | string | null) {
  if (typeof value === 'string') {
    const url = resolveClothingDupeAssetUrl(value);
    return { url, path: value };
  }
  if (!value) return { url: '', path: '—' };
  const path = value.bucket && value.key ? `${value.bucket}/${value.key}` : value.key || '—';
  return {
    url: resolveClothingDupeAssetUrl(value),
    path,
  };
}

function ClothingDupeTaskPanel({ model }: ClothingDupeTaskPanelProps) {
  const task = model.task;
  const status = resolveTaskStatusMeta(task?.status);
  const canCancel = Boolean(
    task && (model.active || task.status === 'CANCELLING' || model.cancelling),
  );

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
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="点击生成后，将在此展示本次换装任务的执行过程"
        />
      ) : (
        <>
          <Progress
            percent={Math.max(0, Math.min(100, task.progress ?? 0))}
            status={toTaskProgressStatus(task.status)}
          />

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
                loading={model.cancelling}
                onClick={() => void model.cancel()}
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

interface ClothingDupeResultsProps {
  model: ClothingDupeModel;
}

export function ClothingDupeResults({ model }: ClothingDupeResultsProps) {
  const outputs = model.outputEntries.map(([name, value]) => ({ name, ...assetInfo(value) }));
  const artifacts: ArtifactItem[] = outputs
    .filter((item) => item.url)
    .map((item) => ({ url: item.url, kind: 'video', from: item.name }));

  if (artifacts.length > 0) {
    return <ArtifactDock artifacts={artifacts} isBusy={model.isBusy} />;
  }

  if (outputs.length === 0) {
    return <ArtifactDock artifacts={[]} isBusy={model.isBusy} />;
  }

  return (
    <div className={styles.outputFallback}>
      <Alert
        type="success"
        showIcon
        message="任务已完成"
        description="接口返回的是对象存储 bucket/key，暂时无法直接预览；等服务端下发可访问地址后会自动播放。"
      />
      <ul className={styles.outputList}>
        {outputs.map((item) => (
          <li key={item.name}>
            <span>{item.name}</span>
            <code title={item.path}>{item.path}</code>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default ClothingDupeTaskPanel;
