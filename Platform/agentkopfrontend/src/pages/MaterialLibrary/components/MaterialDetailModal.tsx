import { Button, Modal, Tag, Typography } from 'antd';
import { LeftOutlined, RightOutlined } from '@ant-design/icons';
import type { MaterialFile } from '../types';
import { formatDateTime, materialKindLabel } from '../utils';
import styles from './MaterialDetailModal.module.less';

interface MaterialDetailModalProps {
  file: MaterialFile | null;
  hasPrev: boolean;
  hasNext: boolean;
  onPrev: () => void;
  onNext: () => void;
  onClose: () => void;
}

/** 素材详情：预览图片 / 音频 / 视频 */
export function MaterialDetailModal({
  file,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onClose,
}: MaterialDetailModalProps) {
  const kindLabel = file ? materialKindLabel(file.kind) : '素材';

  return (
    <Modal
      title={
        <span className={styles.modalTitle}>
          {file && <Tag>{kindLabel}</Tag>}
          <span>{kindLabel}素材</span>
        </span>
      }
      open={!!file}
      onCancel={onClose}
      footer={
        <div className={styles.modalFooter}>
          <Button icon={<LeftOutlined />} disabled={!hasPrev} onClick={onPrev}>
            上一条
          </Button>
          <Button icon={<RightOutlined />} disabled={!hasNext} onClick={onNext} iconPosition="end">
            下一条
          </Button>
        </div>
      }
      width={880}
      destroyOnHidden
    >
      {file && (
        <div className={styles.modalBody}>
          {file.kind === 'video' && (
            <video className={styles.videoPlayer} src={file.url} controls />
          )}
          {file.kind === 'image' && (
            <img className={styles.displayImage} src={file.url} alt={kindLabel} />
          )}
          {file.kind === 'audio' && (
            <audio className={styles.audioPlayer} src={file.url} controls />
          )}
          <div className={styles.metaRow}>
            <span>创建 {formatDateTime(file.createdAt)}</span>
            <Typography.Link href={file.url} target="_blank" rel="noreferrer">
              打开原文件
            </Typography.Link>
          </div>
        </div>
      )}
    </Modal>
  );
}
