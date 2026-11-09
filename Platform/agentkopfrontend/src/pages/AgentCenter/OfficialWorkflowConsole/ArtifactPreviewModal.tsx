/**
 * 产物预览：视频 / 图片
 */
import { Empty, Modal } from 'antd';
import type { ArtifactItem } from '../OfficialExperience/studio/lib/types';
import styles from './OfficialWorkflowConsole.module.less';

interface ArtifactPreviewModalProps {
  open: boolean;
  artifacts: ArtifactItem[];
  title?: string;
  onClose: () => void;
}

function ArtifactPreviewModal({ open, artifacts, title, onClose }: ArtifactPreviewModalProps) {
  return (
    <Modal
      open={open}
      title={title || '查看产物'}
      onCancel={onClose}
      footer={null}
      width={720}
      destroyOnHidden
    >
      {artifacts.length === 0 ? (
        <Empty description="暂无可预览产物" />
      ) : (
        <div className={styles.previewList}>
          {artifacts.map((item) => (
            <figure key={item.url} className={styles.previewItem}>
              {item.kind === 'video' ? (
                <video className={styles.previewMedia} src={item.url} controls playsInline />
              ) : (
                <img className={styles.previewMedia} src={item.url} alt={item.from} />
              )}
              <figcaption className={styles.previewCaption}>
                {item.kind === 'video' ? '视频' : '图片'} · {item.from}
              </figcaption>
            </figure>
          ))}
        </div>
      )}
    </Modal>
  );
}

export default ArtifactPreviewModal;
