import type { KeyboardEvent } from 'react';
import { CustomerServiceOutlined, PictureOutlined, PlayCircleOutlined } from '@ant-design/icons';
import type { MaterialFile } from '../types';
import { formatDateTime, materialCategoryLabel, materialKindLabel } from '../utils';
import styles from '../MaterialLibrary.module.less';

interface MaterialCardProps {
  item: MaterialFile;
  onClick: () => void;
}

const KIND_ICON = {
  image: PictureOutlined,
  audio: CustomerServiceOutlined,
  video: PlayCircleOutlined,
} as const;

/** 素材卡片：按 mediaType 展示图片、音频或视频 */
export function MaterialCard({ item, onClick }: MaterialCardProps) {
  const label = item.category ? materialCategoryLabel(item.category) : materialKindLabel(item.kind);
  const MediaIcon = KIND_ICON[item.kind];

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onClick();
    }
  };

  return (
    <div
      className={styles.card}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
    >
      <div className={styles.cover}>
        {item.kind === 'video' && (
          <video className={styles.coverImage} src={item.url} muted preload="metadata" />
        )}
        {item.kind === 'image' && (
          <img className={styles.coverImage} src={item.url} alt={label} loading="lazy" />
        )}
        {item.kind === 'audio' && (
          <div className={styles.audioCover}>
            <CustomerServiceOutlined className={styles.audioIcon} />
          </div>
        )}
        <span className={styles.mediaBadge} aria-label={label}>
          <MediaIcon />
        </span>
      </div>
      <div className={styles.meta}>
        <span className={styles.metaTitle}>{item.title?.trim() || label}</span>
        <span className={styles.metaTime}>{formatDateTime(item.createdAt).slice(0, 10)}</span>
      </div>
    </div>
  );
}
