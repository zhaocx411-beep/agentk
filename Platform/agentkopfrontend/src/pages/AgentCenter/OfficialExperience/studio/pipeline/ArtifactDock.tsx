/**
 * 底部成品区：主预览 + 侧栏成片缩略
 */
import { forwardRef, useEffect, useMemo, useState } from 'react';
import { Empty, Image, Spin } from 'antd';
import type { ArtifactItem } from '../lib/types';
import styles from '../CreationStudio.module.less';

interface PreviewItem {
  key: string;
  kind: 'video' | 'image' | 'audio';
  url: string;
  from: string;
}

interface ArtifactDockProps {
  artifacts: ArtifactItem[];
  isBusy?: boolean;
}

const ArtifactDock = forwardRef<HTMLDivElement, ArtifactDockProps>(function ArtifactDock(
  { artifacts, isBusy = false },
  ref,
) {
  const videos = useMemo(() => artifacts.filter((item) => item.kind === 'video'), [artifacts]);
  const images = useMemo(() => artifacts.filter((item) => item.kind === 'image'), [artifacts]);

  const previews = useMemo<PreviewItem[]>(
    () =>
      [...videos, ...images].map((item) => ({
        key: `artifact:${item.url}`,
        kind: item.kind,
        url: item.url,
        from: item.from,
      })),
    [videos, images],
  );

  const [activeKey, setActiveKey] = useState<string | null>(null);

  useEffect(() => {
    if (previews.length === 0) {
      setActiveKey(null);
      return;
    }
    if (!activeKey || !previews.some((item) => item.key === activeKey)) {
      setActiveKey(previews[0].key);
    }
  }, [previews, activeKey]);

  const active = previews.find((item) => item.key === activeKey) ?? previews[0] ?? null;
  const hasContent = previews.length > 0;

  return (
    <div ref={ref} className={styles.dock} id="creation-artifact-dock">
      {hasContent ? (
        <div className={styles.dockBar}>
          <span className={styles.dockTitle}>
            {videos.length} 段视频 · {images.length} 张图片
          </span>
          {active && <span className={styles.dockMeta}>来自 {active.from}</span>}
        </div>
      ) : null}

      {!hasContent ? (
        <div className={styles.dockEmptyWrap}>
          {isBusy ? (
            <div className={styles.dockGenerating}>
              <Spin />
              <p>正在生成，完成后会自动显示在这里</p>
            </div>
          ) : (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="生成完成后，视频与图片会显示在这里"
            />
          )}
        </div>
      ) : (
        <div className={styles.dockBody}>
          <div className={styles.dockStage}>
            {active?.kind === 'video' ? (
              <video
                key={active.key}
                className={styles.dockPlayer}
                controls
                preload="metadata"
                src={active.url}
              />
            ) : active?.kind === 'image' ? (
              <Image
                rootClassName={styles.dockPlayerRoot}
                className={styles.dockPlayer}
                src={active.url}
                alt=""
                preview={{ mask: false }}
              />
            ) : (
              <p className={styles.dockEmpty}>选择右侧缩略图预览</p>
            )}
          </div>

          <aside className={styles.dockRail}>
            <section className={styles.dockRailSection}>
              <h3 className={styles.compareLabel}>成片</h3>
              <ul className={styles.dockThumbList}>
                {previews.map((item) => {
                  const selected = item.key === active?.key;
                  return (
                    <li key={item.key}>
                      <button
                        type="button"
                        className={`${styles.dockThumb} ${selected ? styles.dockThumbActive : ''}`}
                        onClick={() => setActiveKey(item.key)}
                      >
                        {item.kind === 'video' ? (
                          <video
                            className={styles.dockThumbMedia}
                            muted
                            preload="metadata"
                            src={item.url}
                          />
                        ) : (
                          <img className={styles.dockThumbMedia} src={item.url} alt="" />
                        )}
                        <span className={styles.dockThumbBadge}>
                          {item.kind === 'video' ? '视频' : '图片'}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </section>
          </aside>
        </div>
      )}
    </div>
  );
});

export default ArtifactDock;
