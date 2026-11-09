import { Avatar, Tag } from 'antd';
import { PictureOutlined, PlayCircleOutlined, UserOutlined } from '@ant-design/icons';
import type { PortfolioRound } from '../types';
import styles from '../WorksGallery.module.less';

interface WorkCardProps {
  item: PortfolioRound;
  onClick: () => void;
}

/** 按创作轮次展示的作品卡片 */
export function WorkCard({ item, onClick }: WorkCardProps) {
  const isVideo = item.cover.assetType === 'video';
  const MediaIcon = isVideo ? PlayCircleOutlined : PictureOutlined;
  const assetCount = item.intermediateAssets.length + item.resultAssets.length;

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
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
        {isVideo ? (
          <video className={styles.coverImage} src={item.cover.assetUrl} muted preload="metadata" />
        ) : (
          <img
            className={styles.coverImage}
            src={item.cover.assetUrl}
            alt={item.promptText}
            loading="lazy"
          />
        )}
        <div className={styles.coverGradient} />
        {item.agentName && <Tag className={styles.categoryTag}>{item.agentName}</Tag>}
        <span className={styles.mediaBadge} aria-label={isVideo ? '视频' : '图片'}>
          <MediaIcon />
        </span>
        <div className={styles.prompt}>{item.promptText || '未填写创作指令'}</div>
      </div>

      <div className={styles.meta}>
        <Avatar size={24} icon={<UserOutlined />} className={styles.avatar}>
          {item.userId.charAt(0)}
        </Avatar>
        <span className={styles.author}>{item.userId}</span>
        <span className={styles.metaRight}>
          <span>{assetCount} 项</span>
          <span className={styles.metaTime}>{item.createdAt.slice(0, 10)}</span>
        </span>
      </div>
    </div>
  );
}
