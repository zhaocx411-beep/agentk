import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Empty, Pagination, Skeleton } from 'antd';
import { MediaPreviewModal, resolvePreviewKind } from '@/components/MediaPreview';
import type { MediaAsset } from './types';
import { MediaPickerTile } from './MediaPickerTile';
import styles from './MediaUpload.module.less';

interface MediaPickerGridProps {
  items: MediaAsset[];
  selectedIds: Set<string>;
  existingUrls: Set<string>;
  remaining: number;
  loading: boolean;
  fetching?: boolean;
  error: boolean;
  page: number;
  pageSize: number;
  total: number;
  onRetry: () => void;
  onToggle: (item: MediaAsset) => void;
  onPageChange: (page: number) => void;
}

export function MediaPickerGrid({
  items,
  selectedIds,
  existingUrls,
  remaining,
  loading,
  fetching = false,
  error,
  page,
  pageSize,
  total,
  onRetry,
  onToggle,
  onPageChange,
}: MediaPickerGridProps) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const [playingId, setPlayingId] = useState<string | null>(null);
  const [preview, setPreview] = useState<MediaAsset | null>(null);

  const stopPlayback = useCallback(() => {
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.currentTime = 0;
      audio.removeAttribute('src');
      audio.load();
    }
    setPlayingId(null);
  }, []);

  useEffect(() => {
    stopPlayback();
    setPreview(null);
  }, [page, items, stopPlayback]);

  const handlePlayToggle = useCallback(
    (item: MediaAsset) => {
      const audio = audioRef.current;
      if (!audio) return;

      if (playingId === item.id) {
        stopPlayback();
        return;
      }

      audio.pause();
      audio.currentTime = 0;
      audio.src = item.url;
      setPlayingId(item.id);
      void audio.play().catch(() => {
        stopPlayback();
      });
    },
    [playingId, stopPlayback],
  );

  if (error) {
    return (
      <div className={styles.pickerPane}>
        <div className={styles.pickerScroll}>
          <Alert
            type="error"
            showIcon
            message="素材加载失败"
            action={<Button onClick={onRetry}>重新加载</Button>}
          />
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className={styles.pickerPane}>
        <div className={styles.pickerScroll}>
          <div className={styles.grid} role="status" aria-busy aria-label="内容加载中">
            {Array.from({ length: 8 }, (_, index) => (
              <Skeleton.Button key={index} active className={styles.tileSkeleton} />
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className={styles.pickerPane}>
        <div className={styles.pickerScroll}>
          <div className={styles.empty}>
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有找到符合条件的内容" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.pickerPane}>
      <div className={styles.pickerScroll}>
        <div
          className={styles.grid}
          aria-busy={fetching || undefined}
          data-fetching={fetching ? 'true' : undefined}
        >
          {items.map((item) => {
            const selected = selectedIds.has(item.id);
            const already = existingUrls.has(item.url);
            const full = !selected && remaining <= 0;
            return (
              <MediaPickerTile
                key={item.id}
                item={item}
                selected={selected || already}
                disabled={already || full}
                playing={playingId === item.id}
                onPlayToggle={() => handlePlayToggle(item)}
                onPreview={() => setPreview(item)}
                onToggle={() => onToggle(item)}
              />
            );
          })}
        </div>
        <audio
          ref={audioRef}
          preload="none"
          className={styles.tileAudioElement}
          onEnded={stopPlayback}
        />
      </div>
      <Pagination
        className={styles.pager}
        current={page}
        pageSize={pageSize}
        total={total}
        onChange={onPageChange}
        size="small"
        hideOnSinglePage
        showSizeChanger={false}
      />
      {preview ? (
        <MediaPreviewModal
          open
          url={preview.url}
          name={preview.name}
          kind={resolvePreviewKind(preview.url, preview.mediaType, preview.name)}
          onClose={() => setPreview(null)}
        />
      ) : null}
    </div>
  );
}
