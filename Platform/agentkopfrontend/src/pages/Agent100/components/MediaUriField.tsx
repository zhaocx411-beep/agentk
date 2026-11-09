import { useMemo, useState } from 'react';
import { Input } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { MediaUpload, type MediaAsset } from '@/components';
import { message } from '@/utils/feedback';
import styles from '../Agent100.module.less';

interface MediaUriFieldProps {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  invalid?: boolean;
  title?: string;
}

function toAssets(uri: string): MediaAsset[] {
  if (!uri.trim()) return [];
  return [
    {
      id: uri,
      url: uri,
      name: uri.split('/').pop() || uri,
      mediaType: 'video',
      source: 'local',
    },
  ];
}

function MediaUriField({
  value,
  onChange,
  disabled = false,
  invalid = false,
  title = '添加视频',
}: MediaUriFieldProps) {
  const [draft, setDraft] = useState('');
  const [uploading, setUploading] = useState(false);
  const assets = useMemo(() => toAssets(value), [value]);
  const hasValue = Boolean(value.trim());
  const cannotAdd = disabled || uploading || hasValue;

  const addUrl = (raw: string) => {
    const url = raw.trim();
    if (!url) return;
    if (hasValue) {
      message.warning('最多添加 1 个素材');
      return;
    }
    onChange(url);
    setDraft('');
  };

  return (
    <div className={`${styles.mediaField} ${invalid ? styles.mediaFieldInvalid : ''}`}>
      <MediaUpload
        accept="video"
        maxCount={1}
        title={title}
        variant="compact"
        value={assets}
        disabled={disabled || uploading}
        onChange={(items) => onChange(items[0]?.url ?? '')}
        onUploadingChange={setUploading}
      />
      <Input.Search
        value={draft}
        enterButton={
          <span>
            <PlusOutlined /> 添加
          </span>
        }
        placeholder="粘贴素材 URL 后回车添加"
        disabled={cannotAdd}
        status={invalid ? 'error' : undefined}
        className={styles.mediaFieldUrlInput}
        onChange={(event) => setDraft(event.target.value)}
        onSearch={addUrl}
      />
      <p className={styles.mediaFieldHint}>支持本地或历史视频素材</p>
    </div>
  );
}

export { MediaUriField };
