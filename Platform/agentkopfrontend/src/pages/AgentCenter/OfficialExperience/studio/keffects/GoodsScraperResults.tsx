import { Alert } from 'antd';
import ArtifactDock from '../pipeline/ArtifactDock';
import type { ArtifactItem } from '../lib/types';
import type { GoodsScraperModel } from './useGoodsScraperTask';
import styles from '../clothingDupe/ClothingDupeStudio.module.less';
import studio from '../CreationStudio.module.less';

interface GoodsScraperResultsProps {
  model: GoodsScraperModel;
}

function toHttps(url: string): string {
  const trimmed = url.trim();
  if (trimmed.startsWith('//')) return `https:${trimmed}`;
  return trimmed;
}

function GoodsText({ title, description }: { title: string; description: string }) {
  if (!title && !description) return null;
  return (
    <div className={studio.card}>
      {title ? <h3 className={studio.cardTitle}>{title}</h3> : null}
      {description ? <p className={studio.fieldTip}>{description}</p> : null}
    </div>
  );
}

function GoodsScraperResults({ model }: GoodsScraperResultsProps) {
  const artifacts: ArtifactItem[] = model.images.map((url, index) => ({
    url: toHttps(url),
    kind: 'image',
    from: index === 0 ? '主图' : `参考图 ${index + 1}`,
  }));
  const title = model.output?.goodsTitle?.trim() || '';
  const description = model.output?.goodsDescription?.trim() || '';
  const hasText = Boolean(title || description);

  if (!hasText && artifacts.length === 0) {
    return <ArtifactDock artifacts={[]} isBusy={model.isBusy} />;
  }

  let media = null;
  if (artifacts.length > 0) {
    media = <ArtifactDock artifacts={artifacts} isBusy={model.isBusy} />;
  } else if (model.task?.status === 'SUCCEEDED') {
    media = <Alert type="warning" showIcon message="未返回商品参考图" />;
  }

  return (
    <div className={styles.outputFallback}>
      <GoodsText title={title} description={description} />
      {media}
    </div>
  );
}

export default GoodsScraperResults;
