import { useMemo, type ReactNode } from 'react';
import { Button, Typography } from 'antd';
import { CopyOutlined } from '@ant-design/icons';
import { MediaPreviewThumb } from '@/components/MediaPreview';
import { message } from '@/utils/feedback';
import {
  flattenResultRows,
  prettyJson,
  type FlattenResultOptions,
  type ResultDisplayRow,
} from './flatten';
import styles from './ResultDataBlock.module.less';

interface ResultDataBlockProps extends FlattenResultOptions {
  title: string;
  data: Record<string, unknown>;
  children?: ReactNode;
}

function formatBoolean(value: unknown): string {
  return value ? '开' : '关';
}

function MediaValue({ url, mediaType }: { url: string; mediaType?: ResultDisplayRow['mediaType'] }) {
  return (
    <div className={styles.media}>
      <MediaPreviewThumb url={url} mediaType={mediaType} className={styles.thumb} />
      <Typography.Text
        className={styles.text}
        copyable={{ tooltips: ['复制', '已复制'] }}
        ellipsis={{ tooltip: url }}
      >
        {url}
      </Typography.Text>
    </div>
  );
}

function ResultValue({ row }: { row: ResultDisplayRow }) {
  if (row.kind === 'boolean') {
    return <span>{formatBoolean(row.value)}</span>;
  }

  if (row.kind === 'media' && typeof row.value === 'string') {
    return <MediaValue url={row.value} mediaType={row.mediaType} />;
  }

  if (row.kind === 'mediaList' && Array.isArray(row.value)) {
    const urls = row.value.filter((item): item is string => typeof item === 'string');
    return (
      <div className={styles.mediaList}>
        {urls.map((url) => (
          <MediaValue key={url} url={url} mediaType={row.mediaType} />
        ))}
      </div>
    );
  }

  if (row.kind === 'json') {
    return <pre className={styles.json}>{prettyJson(row.value)}</pre>;
  }

  const text = String(row.value);
  if (row.copyable) {
    return (
      <Typography.Text
        className={styles.text}
        copyable={{ tooltips: ['复制', '已复制'] }}
        ellipsis={{ tooltip: text }}
      >
        {text}
      </Typography.Text>
    );
  }

  return <span className={styles.text}>{text}</span>;
}

function ResultDataBlock({ title, data, skipKeys, mediaKeys, labelOf, children }: ResultDataBlockProps) {
  const rows = useMemo(
    () => flattenResultRows(data, { skipKeys, mediaKeys, labelOf }),
    [data, skipKeys, mediaKeys, labelOf],
  );
  const fieldRows = rows.filter((row) => row.kind !== 'json');
  const jsonRows = rows.filter((row) => row.kind === 'json');
  if (rows.length === 0 && !children) return null;

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(prettyJson(data));
      message.success('已复制');
    } catch {
      message.error('复制失败，请重试');
    }
  };

  return (
    <section className={styles.block} aria-label={title}>
      <div className={styles.head}>
        <h3 className={styles.title}>{title}</h3>
        <Button type="text" size="small" icon={<CopyOutlined />} onClick={() => void handleCopy()}>
          复制
        </Button>
      </div>
      {children}
      {fieldRows.length > 0 ? (
        <dl className={styles.kv}>
          {fieldRows.map((row) => (
            <div
              key={row.key}
              className={row.kind === 'mediaList' ? `${styles.kvRow} ${styles.kvRowStack}` : styles.kvRow}
            >
              <dt>{row.label}</dt>
              <dd>
                <ResultValue row={row} />
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      {jsonRows.map((row) => (
        <div key={row.key} className={styles.jsonBlock}>
          <p className={styles.jsonLabel}>{row.label}</p>
          <ResultValue row={row} />
        </div>
      ))}
    </section>
  );
}

export { ResultDataBlock };
