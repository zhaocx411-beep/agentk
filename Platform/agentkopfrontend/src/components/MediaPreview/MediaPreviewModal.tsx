import { useEffect, useState } from 'react';
import { Button, Modal, Spin } from 'antd';
import {
  DownloadOutlined,
  ExportOutlined,
  FileExcelOutlined,
  FileOutlined,
  FilePdfOutlined,
  FilePptOutlined,
  FileTextOutlined,
  FileWordOutlined,
} from '@ant-design/icons';
import { downloadMediaByUrl } from '@/services/media';
import { message } from '@/utils/feedback';
import type { PreviewKind } from './utils';
import { fileDisplayName, previewKindLabel } from './utils';
import styles from './MediaPreview.module.less';

interface MediaPreviewModalProps {
  open: boolean;
  url: string;
  name?: string;
  kind: PreviewKind;
  onClose: () => void;
}

function DocIcon({ kind, name }: { kind: PreviewKind; name?: string }) {
  const label = (name || '').toLowerCase();
  if (kind === 'pdf' || label.endsWith('.pdf')) return <FilePdfOutlined />;
  if (/\.xlsx?$/.test(label)) return <FileExcelOutlined />;
  if (/\.pptx?$/.test(label)) return <FilePptOutlined />;
  if (/\.docx?$/.test(label)) return <FileWordOutlined />;
  if (kind === 'text') return <FileTextOutlined />;
  return <FileOutlined />;
}

function TextFilePreview({ url }: { url: string }) {
  const [text, setText] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setFailed(false);
    setText(null);
    void (async () => {
      try {
        const res = await fetch(url, { credentials: 'omit', mode: 'cors' });
        if (!res.ok) throw new Error('fetch failed');
        const body = await res.text();
        if (!cancelled) setText(body);
      } catch {
        if (!cancelled) setFailed(true);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [url]);

  if (loading) {
    return (
      <div className={styles.loadingWrap}>
        <Spin />
      </div>
    );
  }
  if (failed || text === null) {
    return <DocFallbackPanel url={url} kind="text" name="文本文件" />;
  }
  return <pre className={styles.textPreview}>{text}</pre>;
}

function DocFallbackPanel({
  url,
  kind,
  name,
}: {
  url: string;
  kind: PreviewKind;
  name?: string;
}) {
  const displayName = fileDisplayName(url, name, kind);
  const label = previewKindLabel(kind);

  const openTab = () => {
    window.open(url, '_blank', 'noopener,noreferrer');
  };

  const handleDownload = async () => {
    try {
      await downloadMediaByUrl(url, displayName);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '下载失败');
    }
  };

  return (
    <div className={styles.docFallback}>
      <span className={styles.docFallbackIcon} aria-hidden>
        <DocIcon kind={kind} name={displayName} />
      </span>
      <p className={styles.docFallbackTitle}>{displayName}</p>
      <p className={styles.docFallbackHint}>
        {kind === 'office'
          ? 'Word / Excel / PPT 无法在浏览器内直接预览，请在新标签页打开或使用本地 Office 查看。'
          : kind === 'text'
            ? '无法在线读取该文本文件，请在新标签页打开或下载后查看。'
            : `${label}文件暂不支持内嵌预览，请在新标签页打开或下载。`}
      </p>
      <div className={styles.docFallbackActions}>
        <Button type="primary" icon={<ExportOutlined />} onClick={openTab}>
          新标签页打开
        </Button>
        <Button icon={<DownloadOutlined />} onClick={() => void handleDownload()}>
          下载
        </Button>
      </div>
    </div>
  );
}

export function MediaPreviewModal({ open, url, name, kind, onClose }: MediaPreviewModalProps) {
  const title = fileDisplayName(url, name, kind);
  const width =
    kind === 'audio' ? 480 : kind === 'text' || kind === 'office' || kind === 'file' ? 560 : 720;

  const renderBody = () => {
    if (kind === 'image') {
      return <img className={styles.imagePreview} src={url} alt={title} />;
    }
    if (kind === 'video') {
      return <video className={styles.player} src={url} controls autoPlay playsInline />;
    }
    if (kind === 'audio') {
      return <audio className={styles.audioPlayer} src={url} controls autoPlay />;
    }
    if (kind === 'pdf') {
      return <iframe className={styles.docFrame} src={url} title={title} />;
    }
    if (kind === 'text') {
      return <TextFilePreview url={url} />;
    }
    if (kind === 'office' || kind === 'file') {
      return <DocFallbackPanel url={url} kind={kind} name={name} />;
    }
    return null;
  };

  return (
    <Modal
      open={open}
      title={title}
      onCancel={onClose}
      footer={null}
      width={width}
      destroyOnHidden
      className={styles.modal}
    >
      <div className={styles.modalBody}>{renderBody()}</div>
    </Modal>
  );
}
