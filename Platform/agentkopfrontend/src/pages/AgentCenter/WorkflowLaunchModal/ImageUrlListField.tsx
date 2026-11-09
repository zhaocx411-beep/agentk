/**
 * 弹窗内图片列表：本地 / 历史创作 / 历史上传 / 素材库，也可粘贴 URL
 */
import { Input } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useState } from 'react';
import { MediaUpload, type MediaAsset } from '@/components';
import { message } from '@/utils/feedback';
import styles from './WorkflowLaunchModal.module.less';

const MAX_IMAGES = 8;

interface ImageUrlListFieldProps {
  label: string;
  fieldKey: string;
  value: string[];
  onChange: (urls: string[]) => void;
  required?: boolean;
}

function toAssets(urls: string[]): MediaAsset[] {
  return urls.map((url) => ({
    id: url,
    url,
    name: url,
    mediaType: 'image',
    source: 'local',
  }));
}

function ImageUrlListField({ label, fieldKey, value, onChange, required }: ImageUrlListFieldProps) {
  const [draft, setDraft] = useState('');
  const list = Array.isArray(value) ? value : [];

  const addUrl = (raw: string) => {
    const url = raw.trim();
    if (!url) return;
    if (list.includes(url)) return;
    if (list.length >= MAX_IMAGES) {
      message.warning(`最多添加 ${MAX_IMAGES} 张图片`);
      return;
    }
    onChange([...list, url]);
    setDraft('');
  };

  return (
    <div className={styles.field}>
      <div className={styles.fieldHead}>
        <span className={styles.fieldLabel}>
          {label}
          {required ? <span className={styles.requiredMark}>*</span> : null}
        </span>
        <span className={styles.argKey}>{fieldKey}</span>
      </div>

      <MediaUpload
        value={toAssets(list)}
        onChange={(items) => onChange(items.map((item) => item.url))}
        accept="image"
        maxCount={MAX_IMAGES}
        title={`添加${label}`}
        variant="compact"
      />

      <Input.Search
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onSearch={addUrl}
        enterButton={
          <span>
            <PlusOutlined /> 添加
          </span>
        }
        placeholder="粘贴图片 URL 后回车添加"
        disabled={list.length >= MAX_IMAGES}
      />
    </div>
  );
}

export default ImageUrlListField;
