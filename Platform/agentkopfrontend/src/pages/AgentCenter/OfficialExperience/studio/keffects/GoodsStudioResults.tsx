import { Alert } from 'antd';
import ArtifactDock from '../pipeline/ArtifactDock';
import type { ArtifactItem } from '../lib/types';
import type { GoodsStudioModel } from './useGoodsStudioTask';
import styles from '../clothingDupe/ClothingDupeStudio.module.less';

interface GoodsStudioResultsProps {
  model: GoodsStudioModel;
}

function GoodsStudioResults({ model }: GoodsStudioResultsProps) {
  const output = model.task?.output;
  if (model.videoUrl) {
    const artifacts: ArtifactItem[] = [{ url: model.videoUrl, kind: 'video', from: '成片' }];
    return <ArtifactDock artifacts={artifacts} isBusy={model.isBusy} />;
  }

  if (!output?.bucket && !output?.key) {
    return <ArtifactDock artifacts={[]} isBusy={model.isBusy} />;
  }

  const path = output.bucket && output.key ? `${output.bucket}/${output.key}` : output.key || '—';
  return (
    <div className={styles.outputFallback}>
      <Alert
        type="success"
        showIcon
        message="任务已完成"
        description="接口返回的是对象存储 bucket/key，暂时无法直接预览；等服务端下发 videoUrl 后会自动播放。"
      />
      <ul className={styles.outputList}>
        <li>
          <span>成片</span>
          <code title={path}>{path}</code>
        </li>
      </ul>
    </div>
  );
}

export default GoodsStudioResults;
