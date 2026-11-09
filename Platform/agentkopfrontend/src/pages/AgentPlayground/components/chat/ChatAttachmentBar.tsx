/**
 * 对话附件条：选文件 → 上传 → 预览 chips（仅 agent-loop 使用）
 * 发送成功由 ChatArea 调用 clear；失败保留
 */
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react';
import {
  CloseOutlined,
  CloudOutlined,
  FileOutlined,
  FolderOpenOutlined,
  HistoryOutlined,
  PictureOutlined,
  UploadOutlined,
} from '@ant-design/icons';
import { Dropdown, type MenuProps } from 'antd';
import {
  MediaPickerModal,
  MediaPreviewThumb,
  resolvePreviewKind,
  resolveMediaFileLabel,
  usesCompactFileStack,
  type MediaAsset,
  type MediaKind,
  type MediaPickerTab,
} from '@/components';
import { message } from '@/utils/feedback';
import {
  inferMediaType,
  messageAttachmentFromUrl,
  PLATFORM_MEDIA_UPLOAD_ACCEPT,
  SEND_MESSAGE_MAX_ATTACHMENTS,
  SEND_MESSAGE_MAX_VIDEOS,
  toMessageAttachment,
  uploadMedia,
  validateMediaUploadFile,
} from '@/services/media';
import { rememberMediaUrlSize } from '@/services/mediaSizeGuard';
import type { PlatformMediaType } from '@/services/mediaTypes';
import type { MessageAttachment } from '../../types';
import styles from '../../AgentPlayground.module.less';

const PICKER_KINDS: MediaKind[] = ['image', 'video', 'audio'];

function toPickerKind(mediaType: string): MediaKind | undefined {
  if (mediaType === 'image' || mediaType === 'video' || mediaType === 'audio') return mediaType;
  return undefined;
}

function attachmentPreviewUrl(item: PendingAttachment): string | undefined {
  if (item.previewUrl) return item.previewUrl;
  if (item.mediaType === 'image' && item.attachment?.url) return item.attachment.url;
  return undefined;
}
interface PendingAttachment {
  id: string;
  name: string;
  mediaType: string;
  previewUrl?: string;
  status: 'uploading' | 'done' | 'error';
  attachment?: MessageAttachment;
  error?: string;
}

function limitAttachmentCandidates<T extends { mediaType: string }>(
  existing: PendingAttachment[],
  candidates: T[],
): { accepted: T[]; countLimited: boolean; videoLimited: boolean } {
  const accepted: T[] = [];
  let videoCount = existing.filter((item) => item.mediaType === 'video').length;
  let countLimited = false;
  let videoLimited = false;
  for (const candidate of candidates) {
    if (existing.length + accepted.length >= SEND_MESSAGE_MAX_ATTACHMENTS) {
      countLimited = true;
      continue;
    }
    if (candidate.mediaType === 'video' && videoCount >= SEND_MESSAGE_MAX_VIDEOS) {
      videoLimited = true;
      continue;
    }
    accepted.push(candidate);
    if (candidate.mediaType === 'video') videoCount += 1;
  }
  return { accepted, countLimited, videoLimited };
}

export interface ChatAttachmentBarHandle {
  open: () => void;
  openPicker: (tab: MediaPickerTab) => void;
  clear: () => void;
  /** 用远程 URL 预填素材（如 TEMPLATE referImages） */
  setFromUrls: (urls: string[], options?: { replace?: boolean }) => void;
  /** 当前可发送的附件（上传完成） */
  getReady: () => MessageAttachment[];
  isUploading: () => boolean;
}

interface ChatAttachmentBarProps {
  disabled?: boolean;
  /** inline：横条预览；tile：嵌入输入框左侧的上传块 */
  layout?: 'inline' | 'tile';
  onReadyChange?: (attachments: MessageAttachment[], uploading: boolean) => void;
  uploadAccept?: string;
  uploadAllowedTypes?: readonly PlatformMediaType[];
  /** tile 模式「添加素材」按钮文案 */
  uploadAddLabel?: string;
}

const ChatAttachmentBar = forwardRef<ChatAttachmentBarHandle, ChatAttachmentBarProps>(
  function ChatAttachmentBar(
    {
      disabled,
      layout = 'inline',
      onReadyChange,
      uploadAccept = PLATFORM_MEDIA_UPLOAD_ACCEPT,
      uploadAllowedTypes = ['image', 'video', 'audio', 'doc'],
      uploadAddLabel = '添加素材',
    },
    ref,
  ) {
    const maxAttachments = SEND_MESSAGE_MAX_ATTACHMENTS;
    const inputRef = useRef<HTMLInputElement>(null);
    const [items, setItems] = useState<PendingAttachment[]>([]);
    const [pickerOpen, setPickerOpen] = useState(false);
    const [pickerInitialTab, setPickerInitialTab] = useState<MediaPickerTab>('portfolio');
    const previewUrlsRef = useRef<string[]>([]);
    const itemsRef = useRef(items);
    itemsRef.current = items;

    const revokePreviews = useCallback((urls: string[]) => {
      for (const url of urls) {
        if (url.startsWith('blob:')) URL.revokeObjectURL(url);
      }
    }, []);

    const clearAll = useCallback(() => {
      revokePreviews(previewUrlsRef.current);
      previewUrlsRef.current = [];
      setItems([]);
    }, [revokePreviews]);

    useEffect(() => {
      return () => {
        revokePreviews(previewUrlsRef.current);
        previewUrlsRef.current = [];
      };
    }, [revokePreviews]);

    useEffect(() => {
      const ready = items
        .filter((item) => item.status === 'done' && item.attachment)
        .map((item) => item.attachment!);
      const uploading = items.some((item) => item.status === 'uploading');
      onReadyChange?.(ready, uploading);
    }, [items, onReadyChange]);

    const addFromUrls = useCallback(
      (urls: string[], options?: { replace?: boolean }) => {
        const normalized = urls.map((url) => url.trim()).filter(Boolean);
        if (normalized.length === 0) {
          if (options?.replace) clearAll();
          return;
        }

        const batchId = Date.now();
        const pendingItems = normalized.map((url, index) => {
          const attachment = messageAttachmentFromUrl(url);
          return {
            id: `template-url-${batchId}-${index}`,
            sourceUrl: url,
            name: attachment.name || '素材',
            mediaType: attachment.mediaType || 'file',
            previewUrl: attachment.mediaType === 'image' ? attachment.url : undefined,
            status: 'done' as const,
            attachment,
          };
        });

        const prev = itemsRef.current;
        const base = options?.replace ? [] : prev;
        if (options?.replace) {
          for (const item of prev) {
            if (item.previewUrl?.startsWith('blob:')) {
              URL.revokeObjectURL(item.previewUrl);
              previewUrlsRef.current = previewUrlsRef.current.filter((u) => u !== item.previewUrl);
            }
          }
        }
        const existingUrls = new Set(
          base.map((item) => item.attachment?.url).filter((url): url is string => Boolean(url)),
        );
        const candidates = pendingItems.filter((item) => !existingUrls.has(item.sourceUrl));
        const limited = limitAttachmentCandidates(base, candidates);
        if (limited.countLimited) {
          message.warning(`最多上传 ${maxAttachments} 个附件`);
        } else if (limited.videoLimited) {
          message.warning(`每轮最多上传 ${SEND_MESSAGE_MAX_VIDEOS} 个视频`);
        }
        const additions = limited.accepted.map(({ sourceUrl: _sourceUrl, ...item }) => item);
        setItems([...base, ...additions]);
      },
      [clearAll, maxAttachments],
    );

    useImperativeHandle(
      ref,
      () => ({
        open: () => {
          if (!disabled) inputRef.current?.click();
        },
        openPicker: (tab) => {
          if (disabled) return;
          setPickerInitialTab(tab);
          setPickerOpen(true);
        },
        clear: clearAll,
        setFromUrls: addFromUrls,
        getReady: () =>
          itemsRef.current
            .filter((item) => item.status === 'done' && item.attachment)
            .map((item) => item.attachment!),
        isUploading: () => itemsRef.current.some((item) => item.status === 'uploading'),
      }),
      [addFromUrls, clearAll, disabled],
    );

    const uploadOne = useCallback(async (file: File) => {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      const mediaType = inferMediaType(file);
      const previewUrl = mediaType === 'image' ? URL.createObjectURL(file) : undefined;
      if (previewUrl) previewUrlsRef.current.push(previewUrl);

      setItems((prev) => [
        ...prev,
        { id, name: file.name, mediaType, previewUrl, status: 'uploading' },
      ]);

      try {
        const result = await uploadMedia(file);
        const attachment = toMessageAttachment(result, file);
        rememberMediaUrlSize(attachment.url, attachment.size);
        setItems((prev) =>
          prev.map((item) =>
            item.id === id
              ? {
                  ...item,
                  name: resolveMediaFileLabel(attachment.url, file.name, item.mediaType),
                  status: 'done',
                  attachment,
                }
              : item,
          ),
        );
      } catch (err: unknown) {
        const error = err instanceof Error ? err.message : '上传失败';
        setItems((prev) =>
          prev.map((item) => (item.id === id ? { ...item, status: 'error', error } : item)),
        );
        message.error(`${file.name}：${error}`);
      }
    }, []);

    const importRemoteAsset = useCallback(
      (asset: MediaAsset) => {
        const id = `asset-${asset.source}-${asset.id}`;
        const url = asset.url.trim();
        if (!url) {
          message.error(`${asset.name || '素材'}：文件地址无效`);
          return;
        }
        if (itemsRef.current.some((item) => item.id === id)) return;
        if (itemsRef.current.length >= maxAttachments) {
          message.warning(`最多上传 ${maxAttachments} 个附件`);
          return;
        }
        if (
          asset.mediaType === 'video' &&
          itemsRef.current.some((item) => item.mediaType === 'video')
        ) {
          message.warning(`每轮最多上传 ${SEND_MESSAGE_MAX_VIDEOS} 个视频`);
          return;
        }

        const attachment = messageAttachmentFromUrl(url, asset.name, asset.mediaType);
        rememberMediaUrlSize(url, asset.size);
        const resolvedName = resolveMediaFileLabel(
          url,
          attachment.name || asset.name,
          attachment.mediaType,
        );
        setItems((prev) => [
          ...prev,
          {
            id,
            name: resolvedName,
            mediaType: asset.mediaType,
            previewUrl: asset.mediaType === 'image' ? url : undefined,
            status: 'done' as const,
            attachment: { ...attachment, name: resolvedName, size: asset.size },
          },
        ]);
      },
      [maxAttachments],
    );

    const handlePick = useCallback(
      (fileList: FileList | null) => {
        if (!fileList || fileList.length === 0) return;
        const candidates: Array<{ file: File; mediaType: string }> = [];
        for (const file of Array.from(fileList)) {
          const validationError = validateMediaUploadFile(file, uploadAllowedTypes);
          if (validationError) {
            message.error(validationError);
            continue;
          }
          candidates.push({ file, mediaType: inferMediaType(file) });
        }
        const limited = limitAttachmentCandidates(items, candidates);
        if (limited.countLimited) {
          message.warning(`最多上传 ${maxAttachments} 个附件`);
        }
        if (limited.videoLimited) {
          message.warning(`每轮最多上传 ${SEND_MESSAGE_MAX_VIDEOS} 个视频`);
        }
        for (const { file } of limited.accepted) {
          void uploadOne(file);
        }
        if (inputRef.current) inputRef.current.value = '';
      },
      [items, maxAttachments, uploadAllowedTypes, uploadOne],
    );

    const removeItem = useCallback((id: string) => {
      setItems((prev) => {
        const target = prev.find((item) => item.id === id);
        if (target?.previewUrl?.startsWith('blob:')) {
          URL.revokeObjectURL(target.previewUrl);
          previewUrlsRef.current = previewUrlsRef.current.filter((u) => u !== target.previewUrl);
        }
        return prev.filter((item) => item.id !== id);
      });
    }, []);

    const pickerExisting: MediaAsset[] = items.flatMap((item) => {
      const mediaType = toPickerKind(item.mediaType);
      const url = item.attachment?.url;
      if (!mediaType || !url) return [];
      return [
        {
          id: item.id,
          url,
          name: item.name,
          mediaType,
          source: 'history',
        },
      ];
    });
    const pickerMaxCount = maxAttachments - items.length + pickerExisting.length;

    const handlePickerConfirm = useCallback(
      (picked: MediaAsset[]) => {
        setPickerOpen(false);
        const prev = itemsRef.current;
        const existingIds = new Set(prev.map((item) => item.id));
        const existingUrls = new Set(
          prev.map((item) => item.attachment?.url).filter((url): url is string => Boolean(url)),
        );
        const candidates = picked.filter(
          (item) =>
            !existingUrls.has(item.url) && !existingIds.has(`asset-${item.source}-${item.id}`),
        );
        const limited = limitAttachmentCandidates(prev, candidates);
        if (limited.countLimited) {
          message.warning(`最多上传 ${maxAttachments} 个附件`);
        }
        if (limited.videoLimited) {
          message.warning(`每轮最多上传 ${SEND_MESSAGE_MAX_VIDEOS} 个视频`);
        }
        for (const asset of limited.accepted) {
          void importRemoteAsset(asset);
        }
      },
      [importRemoteAsset, maxAttachments],
    );

    const showAddTile = items.length < maxAttachments;

    const openPicker = useCallback(
      (tab: MediaPickerTab) => {
        if (disabled) return;
        setPickerInitialTab(tab);
        setPickerOpen(true);
      },
      [disabled],
    );

    const sourceMenuItems: MenuProps['items'] = [
      { key: 'local', icon: <UploadOutlined />, label: '本地上传' },
      { key: 'portfolio', icon: <HistoryOutlined />, label: '历史创作', disabled },
      { key: 'uploads', icon: <CloudOutlined />, label: '历史上传', disabled },
      { key: 'material', icon: <FolderOpenOutlined />, label: '素材库', disabled },
    ];

    const handleSourceMenuClick: MenuProps['onClick'] = ({ key }) => {
      if (key === 'local') {
        inputRef.current?.click();
        return;
      }
      if (key === 'portfolio' || key === 'uploads' || key === 'material') {
        openPicker(key);
      }
    };

    const renderAttachment = (item: PendingAttachment) => {
      const previewSrc = attachmentPreviewUrl(item);
      const readyUrl = item.status === 'done' ? item.attachment?.url : undefined;
      const previewKind = resolvePreviewKind(
        readyUrl || previewSrc || '',
        item.mediaType,
        item.name,
      );
      const attachmentName = item.attachment?.name || item.name;
      const stackedTile = layout === 'tile' && usesCompactFileStack(previewKind);
      const isVisual =
        Boolean(previewSrc || readyUrl) && item.mediaType !== 'doc' && item.mediaType !== 'file';
      const isDoc = item.mediaType === 'doc' || item.mediaType === 'file';
      return (
        <li
          key={item.id}
          className={`${layout === 'tile' ? styles.attachTileItem : styles.attachChip} ${
            stackedTile ? styles.attachTileItemStacked : ''
          } ${
            isVisual || (readyUrl && isDoc && !stackedTile) ? styles.attachChipImage : ''
          } ${item.status === 'uploading' ? styles.attachChipUploading : ''} ${
            item.status === 'error' ? styles.attachChipFailed : ''
          }`}
        >
          {readyUrl ? (
            <MediaPreviewThumb
              url={readyUrl}
              name={attachmentName}
              mediaType={item.mediaType}
              className={styles.attachThumb}
              compact={stackedTile}
            />
          ) : previewSrc ? (
            <MediaPreviewThumb
              url={previewSrc}
              name={attachmentName}
              mediaType={item.mediaType}
              className={styles.attachThumb}
              compact={stackedTile}
              disabled
            />
          ) : (
            <span className={styles.attachFileIcon}>
              <FileOutlined />
            </span>
          )}
          {layout === 'inline' &&
          !(
            (readyUrl || previewSrc) &&
            (item.mediaType === 'image' || item.mediaType === 'video')
          ) ? (
            <span className={styles.attachName} title={item.name}>
              {item.name}
            </span>
          ) : null}
          {item.status === 'uploading' ? (
            <span className={styles.attachUploadMask} aria-label="上传中">
              <span className={styles.attachUploadSpinner} />
            </span>
          ) : null}
          {item.status === 'error' ? (
            <span className={styles.attachUploadMask} title={item.error || '上传失败'}>
              <span className={styles.attachErrorBadge}>失败</span>
            </span>
          ) : null}
          <button
            type="button"
            className={styles.attachRemove}
            aria-label={`移除 ${item.name}`}
            onClick={() => removeItem(item.id)}
            disabled={disabled}
          >
            <CloseOutlined />
          </button>
        </li>
      );
    };

    return (
      <>
        <input
          ref={inputRef}
          type="file"
          accept={uploadAccept}
          multiple
          className={styles.attachInput}
          disabled={disabled}
          onChange={(e) => handlePick(e.target.files)}
        />
        {layout === 'tile' ? (
          <div className={styles.attachTileColumn} aria-label="素材上传">
            {items.length > 0 ? (
              <ul className={styles.attachTileGrid}>{items.map(renderAttachment)}</ul>
            ) : null}
            {showAddTile ? (
              <div className={styles.attachAddTile}>
                <Dropdown
                  menu={{ items: sourceMenuItems, onClick: handleSourceMenuClick }}
                  trigger={['click']}
                  placement="topLeft"
                  disabled={disabled}
                >
                  <button
                    type="button"
                    className={styles.attachAddTileTrigger}
                    disabled={disabled}
                    aria-label="添加素材"
                    title="添加素材"
                  >
                    <span className={styles.attachAddIconWrap} aria-hidden>
                      <PictureOutlined className={styles.attachAddIcon} />
                      <span className={styles.attachAddPlus}>+</span>
                    </span>
                    <span className={styles.attachAddLabel}>{uploadAddLabel}</span>
                  </button>
                </Dropdown>
                <button
                  type="button"
                  className={styles.attachHistoryBtn}
                  disabled={disabled}
                  onClick={(event) => {
                    event.stopPropagation();
                    openPicker('portfolio');
                  }}
                >
                  历史创作
                </button>
              </div>
            ) : null}
          </div>
        ) : null}
        {layout === 'inline' && items.length > 0 ? (
          <ul className={styles.attachList} aria-label="待发送附件">
            {items.map(renderAttachment)}
          </ul>
        ) : null}
        <MediaPickerModal
          open={pickerOpen}
          initialTab={pickerInitialTab}
          kinds={PICKER_KINDS}
          maxCount={pickerMaxCount}
          existing={pickerExisting}
          onCancel={() => setPickerOpen(false)}
          onConfirm={handlePickerConfirm}
        />
      </>
    );
  },
);

export default ChatAttachmentBar;
