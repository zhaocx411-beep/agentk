import type { KeyboardEvent } from 'react';
import {
  CheckOutlined,
  CustomerServiceOutlined,
  EyeOutlined,
  PauseOutlined,
  PlayCircleOutlined,
  CaretRightOutlined,
} from '@ant-design/icons';
import { isGenericMediaName } from '@/components/MediaPreview/utils';
import type { MediaAsset } from './types';
import { kindLabel } from './utils';
import styles from './MediaUpload.module.less';

interface MediaPickerTileProps {
  item: MediaAsset;
  selected: boolean;
  disabled: boolean;
  playing?: boolean;
  onToggle: () => void;
  onPreview: () => void;
  onPlayToggle?: () => void;
}

function displayName(item: MediaAsset): string {
  const raw = item.name?.trim();
  if (raw && !isGenericMediaName(raw)) return raw;
  const fromUrl = item.url.split('/').pop()?.split('?')[0]?.split('#')[0]?.trim();
  if (fromUrl && !isGenericMediaName(fromUrl)) return fromUrl;
  return kindLabel(item.mediaType);
}

function hasDisplayTitle(item: MediaAsset): boolean {
  const raw = item.name?.trim();
  if (raw && !isGenericMediaName(raw)) return true;
  const fromUrl = item.url.split('/').pop()?.split('?')[0]?.split('#')[0]?.trim();
  return Boolean(fromUrl && !isGenericMediaName(fromUrl));
}

export function MediaPickerTile({
  item,
  selected,
  disabled,
  playing = false,
  onToggle,
  onPreview,
  onPlayToggle,
}: MediaPickerTileProps) {
  const label = displayName(item);
  const isAudio = item.mediaType === 'audio';
  const showTitle = hasDisplayTitle(item);

  const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      if (!disabled || selected) onToggle();
    }
  };

  return (
    <div className={styles.tileItem}>
      <div className={styles.tileShell}>
        <button
          type="button"
          className={`${styles.tile} ${selected ? styles.tileSelected : ''} ${
            disabled && !selected ? styles.tileDisabled : ''
          } ${isAudio ? styles.tileAudioItem : ''} ${playing ? styles.tileAudioPlaying : ''}`}
          onClick={() => {
            if (!disabled || selected) onToggle();
          }}
          onKeyDown={handleKeyDown}
          aria-pressed={selected}
          aria-label={`${selected ? '取消选择' : '选择'}${label}`}
          disabled={disabled && !selected}
        >
          <span className={styles.tileCover}>
            {item.mediaType === 'video' ? (
              <video className={styles.tileMedia} src={item.url} muted playsInline preload="metadata" />
            ) : null}
            {item.mediaType === 'image' ? (
              <img className={styles.tileMedia} src={item.thumbUrl || item.url} alt="" loading="lazy" />
            ) : null}
            {isAudio ? (
              <span className={styles.tileAudio}>
                <CustomerServiceOutlined className={styles.tileAudioIcon} aria-hidden />
              </span>
            ) : null}
            {item.mediaType === 'video' ? (
              <span className={styles.tileBadge} aria-hidden>
                <PlayCircleOutlined />
              </span>
            ) : null}
          </span>
          {selected ? (
            <span className={styles.tileCheck} aria-hidden>
              <CheckOutlined />
            </span>
          ) : null}
        </button>
        <button
          type="button"
          className={styles.tilePreview}
          aria-label={`预览${label}`}
          onClick={(event) => {
            event.stopPropagation();
            onPreview();
          }}
        >
          <EyeOutlined />
          预览
        </button>
        {isAudio ? (
          <button
            type="button"
            className={styles.tileAudioPlay}
            aria-label={playing ? `暂停 ${label}` : `播放 ${label}`}
            onClick={(event) => {
              event.stopPropagation();
              onPlayToggle?.();
            }}
          >
            {playing ? <PauseOutlined /> : <CaretRightOutlined />}
          </button>
        ) : null}
      </div>
      {showTitle ? (
        <span className={styles.tileCaption} title={label}>
          {label}
        </span>
      ) : null}
    </div>
  );
}
