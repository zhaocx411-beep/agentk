import styles from './WorkDetailModal.module.less';

interface ImagePreviewProps {
  url: string | null;
  onClose: () => void;
}

/** 全屏图片预览浮层 */
function ImagePreview({ url, onClose }: ImagePreviewProps) {
  if (!url) return null;

  return (
    <div className={styles.previewOverlay} onClick={onClose}>
      <img className={styles.previewImage} src={url} alt="预览" />
    </div>
  );
}

export default ImagePreview;
