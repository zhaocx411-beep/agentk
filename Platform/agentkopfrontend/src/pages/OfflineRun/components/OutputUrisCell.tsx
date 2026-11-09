/**
 * 离线跑数用例产出：按 outputUris 直接渲染图片 / 视频缩略图
 */
import { Popover } from 'antd';
import { MediaPreviewThumb } from '@/components/MediaPreview';
import styles from './OutputUrisCell.module.less';

interface OutputUrisCellProps {
  uris?: string[];
}

const MAX_INLINE_ITEMS = 4;

export function OutputUrisCell({ uris }: OutputUrisCellProps) {
  const list = [...new Set((uris ?? []).map((uri) => uri.trim()).filter(Boolean))];
  if (list.length === 0) return <span>--</span>;

  const inlineCount = list.length > MAX_INLINE_ITEMS ? MAX_INLINE_ITEMS - 1 : list.length;
  const inlineItems = list.slice(0, inlineCount);
  const remaining = list.length - inlineItems.length;

  return (
    <div className={styles.list}>
      {inlineItems.map((uri) => (
        <MediaPreviewThumb key={uri} url={uri} className={styles.thumb} alt="离线任务产物" />
      ))}
      {remaining > 0 ? (
        <Popover
          trigger="click"
          placement="bottomLeft"
          title={`全部产出（${list.length}）`}
          content={
            <div className={styles.gallery}>
              {list.map((uri) => (
                <MediaPreviewThumb
                  key={uri}
                  url={uri}
                  className={styles.galleryThumb}
                  alt="离线任务产物"
                />
              ))}
            </div>
          }
        >
          <button type="button" className={styles.moreButton} aria-label={`查看其余 ${remaining} 个产物`}>
            +{remaining}
          </button>
        </Popover>
      ) : null}
    </div>
  );
}
