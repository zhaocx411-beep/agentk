import { useMemo } from 'react';
import { Image } from 'antd';
import { collectMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import styles from './TraceMediaList.module.less';

interface TraceMediaListProps {
  source: unknown;
  align?: 'left' | 'right';
}

/** 从 Trace 消息或工具结果中识别并直接预览媒体产物。 */
export function TraceMediaList({ source, align = 'left' }: TraceMediaListProps) {
  const media = useMemo(() => collectMediaUrls(source), [source]);
  if (media.videos.length === 0 && media.audios.length === 0 && media.images.length === 0) {
    return null;
  }

  return (
    <div
      className={`${styles.mediaList} ${align === 'right' ? styles.mediaListRight : ''}`}
      aria-label="媒体产物"
    >
      {media.videos.map((url) => (
        <video
          key={url}
          className={styles.mediaVideo}
          controls
          playsInline
          preload="metadata"
          src={url}
        />
      ))}
      {media.audios.map((url) => (
        <audio key={url} className={styles.mediaAudio} controls preload="metadata" src={url} />
      ))}
      {media.images.map((url) => (
        <Image
          key={url}
          className={styles.mediaImage}
          src={url}
          alt="Trace 图片产物"
          preview={{ mask: '预览' }}
        />
      ))}
    </div>
  );
}
