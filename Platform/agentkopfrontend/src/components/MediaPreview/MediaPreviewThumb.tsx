import { useState, type ReactNode } from 'react';
import { Image } from 'antd';
import {
  FileExcelOutlined,
  FileOutlined,
  FilePdfOutlined,
  FilePptOutlined,
  FileTextOutlined,
  FileWordOutlined,
  PlayCircleOutlined,
  SoundOutlined,
} from '@ant-design/icons';
import { MediaPreviewModal } from './MediaPreviewModal';
import {
  fileDisplayName,
  fileExtensionLabel,
  isDocumentPreviewKind,
  resolvePreviewKind,
  shouldShowCompactCaption,
  usesCompactFileStack,
  type PreviewKind,
} from './utils';
import styles from './MediaPreview.module.less';

interface MediaPreviewThumbProps {
  url: string;
  name?: string;
  mediaType?: string;
  className?: string;
  alt?: string;
  /** 上传区等小尺寸：音频 / 文档标题移到方块外展示 */
  compact?: boolean;
  /** 上传中等场景禁用点击预览 */
  disabled?: boolean;
}

function DocThumbIcon({ kind, name }: { kind: PreviewKind; name: string }) {
  const lower = name.toLowerCase();
  if (kind === 'pdf' || lower.endsWith('.pdf')) return <FilePdfOutlined />;
  if (/\.xlsx?$/.test(lower)) return <FileExcelOutlined />;
  if (/\.pptx?$/.test(lower)) return <FilePptOutlined />;
  if (/\.docx?$/.test(lower)) return <FileWordOutlined />;
  if (kind === 'text') return <FileTextOutlined />;
  return <FileOutlined />;
}

function shouldShowTitle(kind: PreviewKind, displayName: string): boolean {
  if (!usesCompactFileStack(kind)) return true;
  return shouldShowCompactCaption(displayName);
}

function AudioCompactSquare({ className }: { className?: string }) {
  const rootClass = [styles.thumbRoot, styles.thumbRootCompact, className].filter(Boolean).join(' ');
  return (
    <div className={rootClass}>
      <div className={styles.fileThumbCompactAudio}>
        <span className={styles.fileThumbIcon} aria-hidden>
          <SoundOutlined />
        </span>
      </div>
    </div>
  );
}

function DocCompactSquare({
  kind,
  url,
  name,
  className,
}: {
  kind: PreviewKind;
  url: string;
  name: string;
  className?: string;
}) {
  const extLabel = fileExtensionLabel(url, name, kind);
  const rootClass = [styles.thumbRoot, styles.thumbRootCompact, className].filter(Boolean).join(' ');

  return (
    <div className={rootClass}>
      <div className={styles.fileThumbCompactDoc}>
        <span className={styles.fileThumbIcon} aria-hidden>
          <DocThumbIcon kind={kind} name={name} />
        </span>
        {extLabel ? <span className={styles.fileThumbExt}>{extLabel}</span> : null}
      </div>
    </div>
  );
}

function FileTypeThumb({ kind, url, name }: { kind: PreviewKind; url: string; name: string }) {
  const extLabel = fileExtensionLabel(url, name, kind);
  const isAudio = kind === 'audio';
  const isDoc = isDocumentPreviewKind(kind);
  const showName = shouldShowTitle(kind, name);
  const showExt = Boolean(extLabel) && (!showName || isDoc);

  return (
    <div className={`${styles.fileThumb} ${isAudio ? styles.fileThumbAudio : styles.fileThumbDoc}`}>
      <span className={styles.fileThumbIcon} aria-hidden>
        {isAudio ? <SoundOutlined /> : <DocThumbIcon kind={kind} name={name} />}
      </span>
      {showExt && extLabel ? <span className={styles.fileThumbExt}>{extLabel}</span> : null}
      {showName && !isDoc ? (
        <span className={styles.fileThumbName} title={name}>
          {name}
        </span>
      ) : null}
    </div>
  );
}

function StaticThumb({
  kind,
  url,
  name,
  className,
  alt,
}: {
  kind: PreviewKind;
  url: string;
  name: string;
  className?: string;
  alt?: string;
}) {
  const rootClass = [styles.thumbRoot, className].filter(Boolean).join(' ');

  if (kind === 'image') {
    return (
      <div className={rootClass}>
        <img src={url} alt={alt || name} className={styles.mediaCover} />
      </div>
    );
  }
  if (kind === 'video') {
    return (
      <div className={rootClass}>
        <span className={styles.videoThumbWrap}>
          <video src={url} className={styles.mediaCover} muted preload="metadata" playsInline />
          <PlayCircleOutlined className={styles.playBadge} aria-hidden />
        </span>
      </div>
    );
  }
  return (
    <div className={rootClass}>
      <FileTypeThumb kind={kind} url={url} name={name} />
    </div>
  );
}

function CompactStackSquare({
  kind,
  url,
  name,
  className,
}: {
  kind: PreviewKind;
  url: string;
  name: string;
  className?: string;
}) {
  if (kind === 'audio') return <AudioCompactSquare className={className} />;
  return <DocCompactSquare kind={kind} url={url} name={name} className={className} />;
}

export function MediaPreviewThumb({
  url,
  name,
  mediaType,
  className,
  alt,
  compact = false,
  disabled = false,
}: MediaPreviewThumbProps) {
  const kind = resolvePreviewKind(url, mediaType, name);
  const displayName = fileDisplayName(url, name, kind);
  const [open, setOpen] = useState(false);
  const rootClass = [styles.thumbRoot, className].filter(Boolean).join(' ');
  const useCompactStack = compact && usesCompactFileStack(kind);
  const showCompactCaption = useCompactStack && shouldShowTitle(kind, displayName);

  if (disabled) {
    if (useCompactStack) {
      return (
        <div className={styles.thumbStack}>
          <CompactStackSquare kind={kind} url={url} name={displayName} className={className} />
          {showCompactCaption ? (
            <span className={styles.thumbStackCaption} title={displayName}>
              {displayName}
            </span>
          ) : null}
        </div>
      );
    }
    return <StaticThumb kind={kind} url={url} name={displayName} className={className} alt={alt} />;
  }

  if (kind === 'image') {
    return (
      <Image
        src={url}
        alt={alt || displayName}
        className={styles.mediaCover}
        rootClassName={rootClass}
        preview={{ mask: '预览' }}
      />
    );
  }

  const openPreview = () => setOpen(true);

  let trigger: ReactNode;
  if (useCompactStack) {
    trigger = (
      <div className={styles.thumbStack}>
        <button
          type="button"
          className={styles.previewTriggerStack}
          onClick={openPreview}
          aria-label={`预览 ${displayName}`}
        >
          <CompactStackSquare kind={kind} url={url} name={displayName} className={className} />
        </button>
        {showCompactCaption ? (
          <span className={styles.thumbStackCaption} title={displayName}>
            {displayName}
          </span>
        ) : null}
      </div>
    );
  } else if (kind === 'video') {
    trigger = (
      <button
        type="button"
        className={styles.previewTrigger}
        onClick={openPreview}
        aria-label={`预览视频 ${displayName}`}
      >
        <StaticThumb kind={kind} url={url} name={displayName} className={className} alt={alt} />
      </button>
    );
  } else {
    trigger = (
      <button
        type="button"
        className={styles.previewTrigger}
        onClick={openPreview}
        aria-label={`预览 ${displayName}`}
      >
        <StaticThumb kind={kind} url={url} name={displayName} className={className} alt={alt} />
      </button>
    );
  }

  return (
    <>
      {trigger}
      <MediaPreviewModal open={open} url={url} name={name} kind={kind} onClose={() => setOpen(false)} />
    </>
  );
}
