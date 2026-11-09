import { useCallback, useRef, useState, type DragEvent, type KeyboardEvent } from 'react';
import { Button } from 'antd';
import {
  CloseOutlined,
  CustomerServiceOutlined,
  PictureOutlined,
  PlusOutlined,
  VideoCameraOutlined,
} from '@ant-design/icons';
import { message } from '@/utils/feedback';
import { MediaPreviewThumb } from '@/components/MediaPreview';
import { resolvePreviewKind, usesCompactFileStack } from '@/components/MediaPreview';
import { uploadMedia, validateMediaUploadFile } from '@/services/media';
import { rememberMediaUrlSize } from '@/services/mediaSizeGuard';
import { MediaPickerModal } from './MediaPickerModal';
import type { MediaAsset, MediaKind, MediaUploadProps } from './types';
import {
  acceptAttr,
  fileMatchesKinds,
  kindLabel,
  normalizeKinds,
  parseMediaKind,
  pickerTitle,
} from './utils';
import styles from './MediaUpload.module.less';

function DropzoneIcon({ kinds }: { kinds: MediaKind[] }) {
  if (kinds.includes('video') && kinds.length > 1) return <VideoCameraOutlined className={styles.icon} />;
  if (kinds.length === 1) {
    if (kinds[0] === 'video') return <VideoCameraOutlined className={styles.icon} />;
    if (kinds[0] === 'audio') return <CustomerServiceOutlined className={styles.icon} />;
    return <PictureOutlined className={styles.icon} />;
  }
  return <PictureOutlined className={styles.icon} />;
}

function MediaUpload({
  value,
  onChange,
  accept,
  maxCount = 8,
  title = '添加素材',
  variant = 'default',
  materialCategoryDefault,
  disabled = false,
  onUploadingChange,
}: MediaUploadProps) {
  const kinds = normalizeKinds(accept);
  const inputRef = useRef<HTMLInputElement>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const remaining = Math.max(maxCount - value.length, 0);
  const canAdd = remaining > 0 && !disabled && !uploading;
  const dropTitle = title || pickerTitle(kinds);

  const setBusy = useCallback(
    (next: boolean) => {
      setUploading(next);
      onUploadingChange?.(next);
    },
    [onUploadingChange],
  );

  const addFiles = useCallback(
    async (files: File[]) => {
      if (!canAdd || files.length === 0) return;
      const room = remaining;
      const picked = files.slice(0, room);
      setBusy(true);
      try {
        const next = [...value];
        for (const file of picked) {
          if (!fileMatchesKinds(file, kinds)) {
            message.warning(`${file.name} 不是${kinds.map(kindLabel).join('/')}文件`);
            continue;
          }
          const validationError = validateMediaUploadFile(file);
          if (validationError) {
            message.error(validationError);
            continue;
          }
          const result = await uploadMedia(file);
          const url = result.url;
          if (!url) {
            message.error(`${file.name} 上传成功但未拿到文件地址`);
            continue;
          }
          if (next.some((item) => item.url === url)) continue;
          const size = typeof result.size === 'number' ? result.size : file.size;
          rememberMediaUrlSize(url, size);
          next.push({
            id: `local-${Date.now()}-${file.name}`,
            url,
            name: result.name ?? file.name,
            mediaType: parseMediaKind(result.mediaType) ?? kinds[0],
            source: 'local',
            size,
          });
        }
        onChange(next.slice(0, maxCount));
      } catch (error) {
        message.error(error instanceof Error ? error.message : '上传失败');
      } finally {
        setBusy(false);
        if (inputRef.current) inputRef.current.value = '';
      }
    },
    [canAdd, kinds, maxCount, onChange, remaining, setBusy, value],
  );

  const openFile = () => {
    if (!canAdd) return;
    inputRef.current?.click();
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    void addFiles(Array.from(event.dataTransfer.files));
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      openFile();
    }
  };

  const removeItem = (id: string) => {
    onChange(value.filter((item) => item.id !== id));
  };

  const importPickedAssets = useCallback(
    (picked: MediaAsset[]) => {
      if (picked.length === 0) return;
      const urls = new Set(value.map((item) => item.url));
      const toImport = picked.filter((item) => !urls.has(item.url)).slice(0, remaining);
      if (toImport.length === 0) return;

      const next = [...value];
      for (const asset of toImport) {
        const url = asset.url.trim();
        if (!url || next.some((item) => item.url === url)) continue;
        rememberMediaUrlSize(url, asset.size);
        next.push({
          id: `asset-${asset.source}-${asset.id}`,
          url,
          name: asset.name,
          mediaType: asset.mediaType,
          source: asset.source,
          size: asset.size,
        });
      }
      onChange(next.slice(0, maxCount));
    },
    [maxCount, onChange, remaining, value],
  );

  return (
    <div className={styles.root}>
      <input
        ref={inputRef}
        type="file"
        accept={acceptAttr(kinds)}
        multiple={maxCount > 1}
        className={styles.fileInput}
        disabled={!canAdd}
        onChange={(event) => void addFiles(Array.from(event.target.files ?? []))}
      />

      {value.length > 0 ? (
        <ul className={styles.thumbList}>
          {value.map((item, index) => {
            const kind = resolvePreviewKind(item.url, item.mediaType, item.name);
            const stacked = usesCompactFileStack(kind);
            return (
            <li
              key={item.id}
              className={`${styles.thumbItem} ${stacked ? styles.thumbItemStacked : ''}`}
            >
              <MediaPreviewThumb
                url={item.url}
                name={item.name}
                mediaType={item.mediaType}
                alt={item.name || `已选 ${index + 1}`}
                className={styles.thumb}
                compact={stacked}
              />
              <button
                type="button"
                className={styles.remove}
                aria-label={`移除第 ${index + 1} 项`}
                onClick={(event) => {
                  event.stopPropagation();
                  removeItem(item.id);
                }}
                disabled={disabled}
              >
                <CloseOutlined />
              </button>
            </li>
            );
          })}
        </ul>
      ) : null}

      {remaining > 0 ? (
        <div
          className={`${styles.dropzone} ${
            variant === 'compact' ? styles.dropzoneInline : ''
          } ${value.length > 0 && variant === 'default' ? styles.dropzoneCompact : ''} ${
            dragging ? styles.dropzoneActive : ''
          } ${!canAdd ? styles.dropzoneDisabled : ''}`}
          role="button"
          tabIndex={canAdd ? 0 : -1}
          onClick={openFile}
          onKeyDown={handleKeyDown}
          onDragOver={(event) => {
            event.preventDefault();
            if (canAdd) setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          aria-label={dropTitle}
        >
          <span className={styles.iconWrap} aria-hidden>
            <DropzoneIcon kinds={kinds} />
            <span className={styles.plusBadge}>
              <PlusOutlined />
            </span>
          </span>
          <p className={styles.dropTitle}>{value.length > 0 ? '继续添加' : dropTitle}</p>
          <Button
            htmlType="button"
            disabled={disabled || uploading}
            onClick={(event) => {
              event.stopPropagation();
              setPickerOpen(true);
            }}
          >
            历史创作
          </Button>
        </div>
      ) : null}

      <MediaPickerModal
        open={pickerOpen}
        kinds={kinds}
        maxCount={maxCount}
        existing={value}
        materialCategoryDefault={materialCategoryDefault}
        onCancel={() => setPickerOpen(false)}
        onConfirm={(picked) => {
          setPickerOpen(false);
          importPickedAssets(picked);
        }}
      />
    </div>
  );
}

export { MediaUpload };
export { MediaPickerModal };
export { MediaSourceMenu } from './MediaSourceMenu';
export type {
  MediaAsset,
  MediaKind,
  MediaPickerTab,
  MediaUploadProps,
  MediaUploadVariant,
} from './types';
