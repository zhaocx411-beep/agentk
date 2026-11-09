import { MediaUpload, type MediaAsset } from '@/components';
import { VIRTUAL_TRYON_PRODUCT_IMAGE_MAX } from './constants';
import type { VirtualTryOnComposerModel, VirtualTryOnMediaItem } from './useVirtualTryOnComposer';
import styles from './VirtualTryOnComposerControls.module.less';

interface VirtualTryOnComposerControlsProps {
  model: VirtualTryOnComposerModel;
  disabled?: boolean;
}

function toMediaAssets(items: VirtualTryOnMediaItem[]): MediaAsset[] {
  return items.map((item) => ({
    id: item.id,
    url: item.url,
    name: item.name,
    mediaType: 'image',
    source: 'local',
  }));
}

function fromMediaAssets(items: MediaAsset[]): VirtualTryOnMediaItem[] {
  return items.map((item) => ({
    id: item.id,
    url: item.url,
    name: item.name || item.url,
  }));
}

function VirtualTryOnComposerControls({ model, disabled }: VirtualTryOnComposerControlsProps) {
  return (
    <div className={styles.panel} aria-label="虚拟试穿参数">
      <div className={styles.field}>
        <div className={styles.fieldHead}>
          <span className={styles.label}>
            商品图
            <span className={styles.requiredMark}>*</span>
          </span>
          <span className={styles.meta}>
            {model.productImages.length}/{VIRTUAL_TRYON_PRODUCT_IMAGE_MAX} · 1～4 张
          </span>
        </div>
        <MediaUpload
          value={toMediaAssets(model.productImages)}
          onChange={(items) => model.setProductImages(fromMediaAssets(items))}
          accept="image"
          maxCount={VIRTUAL_TRYON_PRODUCT_IMAGE_MAX}
          title="添加商品图"
          variant="compact"
          disabled={disabled || model.isUploading}
          onUploadingChange={model.setIsUploading}
        />
      </div>

      <div className={styles.field}>
        <div className={styles.fieldHead}>
          <span className={styles.label}>模特图</span>
          <span className={styles.meta}>
            {model.modelImage.length}/1 · 可选
          </span>
        </div>
        <MediaUpload
          value={toMediaAssets(model.modelImage)}
          onChange={(items) => model.setModelImage(fromMediaAssets(items))}
          accept="image"
          maxCount={1}
          title="添加模特图"
          variant="compact"
          disabled={disabled || model.isUploading}
          onUploadingChange={model.setIsUploading}
        />
      </div>
    </div>
  );
}

export { VirtualTryOnComposerControls };
