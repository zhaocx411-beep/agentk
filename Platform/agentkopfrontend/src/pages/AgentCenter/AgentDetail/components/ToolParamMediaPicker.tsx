import { useMemo, useRef, useState } from 'react';
import { Input } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { MediaUpload, type MediaAsset } from '@/components';
import { uploadMedia, validateMediaUploadFile } from '@/services/media';
import { message } from '@/utils/feedback';
import type { OfficialToolParameter } from '@/types/officialTools';
import {
  acceptForMediaParam,
  extractMediaUrls,
  mediaKindsForParam,
  mediaKindsHint,
  supportsLocalMediaUpload,
  urlsToParamFormValue,
} from '../lib/toolDefaultsUtils';
import styles from '../AgentDetail.module.less';

const MAX_TOOL_MEDIA_ITEMS = 20;

interface ToolParamMediaPickerProps {
  param: OfficialToolParameter;
  value: unknown;
  onChange: (value: unknown) => void;
  invalid?: boolean;
}

function urlsToMediaAssets(urls: readonly string[], kind: MediaAsset['mediaType']): MediaAsset[] {
  return urls.map((url, index) => ({
    id: `tool-${index}-${url}`,
    url,
    name: url.split('/').pop() || url,
    mediaType: kind,
    source: 'local',
  }));
}

async function uploadFiles(files: File[]): Promise<string[]> {
  const urls: string[] = [];
  for (const file of files) {
    const result = await uploadMedia(file);
    const url = result.url;
    if (url) urls.push(url);
  }
  return urls;
}

function ToolParamMediaPicker({ param, value, onChange, invalid = false }: ToolParamMediaPickerProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [draft, setDraft] = useState('');
  const pickerKinds = mediaKindsForParam(param);
  const currentUrls = extractMediaUrls(param, value);
  const mediaKind = pickerKinds[0] ?? 'image';
  const kindsHint = mediaKindsHint(pickerKinds);
  const maxCount =
    param.type === 'string' ? 1 : MAX_TOOL_MEDIA_ITEMS;

  const assets = useMemo(
    () => urlsToMediaAssets(currentUrls, mediaKind),
    [currentUrls, mediaKind],
  );

  if (!supportsLocalMediaUpload(param)) return null;

  const syncUrls = (urls: readonly string[]) => {
    onChange(urlsToParamFormValue(param, urls));
  };

  const handleAssetsChange = (items: MediaAsset[]) => {
    syncUrls(items.map((item) => item.url));
  };

  const addUrl = (raw: string) => {
    const url = raw.trim();
    if (!url) return;
    if (currentUrls.includes(url)) return;
    if (currentUrls.length >= maxCount) {
      message.warning(`最多添加 ${maxCount} 个素材`);
      return;
    }
    syncUrls([...currentUrls, url]);
    setDraft('');
  };

  const handleLocalUpload = async (files: File[]) => {
    const validFiles = files.filter((file) => {
      const validationError = validateMediaUploadFile(file);
      if (validationError) message.error(validationError);
      return !validationError;
    });
    if (validFiles.length === 0) return;
    setUploading(true);
    try {
      const urls = await uploadFiles(validFiles);
      if (urls.length === 0) {
        message.error('上传成功但未拿到文件地址');
        return;
      }
      const merged = [...currentUrls];
      for (const url of urls) {
        if (url && !merged.includes(url)) merged.push(url);
      }
      syncUrls(merged.slice(0, maxCount));
      message.success(`已上传 ${urls.length} 个文件`);
    } catch (error: unknown) {
      message.error(error instanceof Error ? error.message : '上传失败');
    } finally {
      setUploading(false);
    }
  };

  const wrapClass = `${styles.toolParamMediaWrap}${invalid ? ` ${styles.toolParamMediaWrapInvalid}` : ''}`;

  if (pickerKinds.length === 0) {
    return (
      <div className={wrapClass}>
        <div className={styles.toolParamUploadRow}>
          <input
            ref={inputRef}
            type="file"
            accept={acceptForMediaParam(param)}
            multiple
            className={styles.toolParamFileInput}
            disabled={uploading}
            onChange={(event) => {
              const files = Array.from(event.target.files ?? []);
              event.target.value = '';
              void handleLocalUpload(files);
            }}
          />
          <button
            type="button"
            className={styles.toolParamLocalUpload}
            disabled={uploading || currentUrls.length >= maxCount}
            onClick={() => inputRef.current?.click()}
          >
            {uploading ? '上传中…' : '本地上传'}
          </button>
          <span className={styles.toolParamUploadHint}>当前参数仅支持本地文件</span>
        </div>
        {currentUrls.length > 0 ? (
          <ul className={styles.toolParamUrlList}>
            {currentUrls.map((url) => (
              <li key={url}>{url}</li>
            ))}
          </ul>
        ) : null}
      </div>
    );
  }

  return (
    <div className={wrapClass}>
      <MediaUpload
        value={assets}
        onChange={handleAssetsChange}
        accept={pickerKinds.length === 1 ? pickerKinds[0] : pickerKinds}
        maxCount={maxCount}
        title={`添加${kindsHint}`}
        variant="compact"
        disabled={uploading}
        onUploadingChange={setUploading}
      />
      <Input.Search
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onSearch={addUrl}
        enterButton={
          <span>
            <PlusOutlined /> 添加
          </span>
        }
        placeholder="粘贴素材 URL 后回车添加"
        disabled={currentUrls.length >= maxCount || uploading}
        className={styles.toolParamUrlInput}
      />
      <p className={styles.toolParamUploadHint}>支持本地或历史{kindsHint}素材</p>
    </div>
  );
}

export { ToolParamMediaPicker };
