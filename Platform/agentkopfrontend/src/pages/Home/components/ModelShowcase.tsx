/**
 * 首页视频 / 图像模型能力展示：列表 + 大预览交替布局
 * 当前展示数据使用静图预览；有视频资源时仍支持播放。
 */
import { useEffect, useRef, useState } from 'react';
import { HOME_MODEL_SHOWCASE, type ModelShowcaseBlock } from '../modelShowcase';
import styles from './ModelShowcase.module.less';

function usePrefersReducedMotion() {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    const sync = () => setReduced(mq.matches);
    sync();
    mq.addEventListener('change', sync);
    return () => mq.removeEventListener('change', sync);
  }, []);
  return reduced;
}

function ModelShowcaseBlockView({ block }: { block: ModelShowcaseBlock }) {
  const [activeId, setActiveId] = useState(block.items[0]?.id ?? '');
  const hoverTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const videoRefs = useRef<Map<string, HTMLVideoElement>>(new Map());
  const reducedMotion = usePrefersReducedMotion();
  const active = block.items.find((item) => item.id === activeId) ?? block.items[0];
  const mediaLeft = block.mediaSide === 'left';

  useEffect(
    () => () => {
      if (hoverTimerRef.current) clearTimeout(hoverTimerRef.current);
    },
    [],
  );

  // 仅播放当前选中项（默认静音）；其余暂停复位，避免多路同时解码
  useEffect(() => {
    videoRefs.current.forEach((el, id) => {
      if (id === active?.id && !reducedMotion && active.video) {
        el.muted = true;
        try {
          el.currentTime = 0;
        } catch {
          // ignore seek before metadata
        }
        void el.play().catch(() => undefined);
      } else {
        el.pause();
        try {
          el.currentTime = 0;
        } catch {
          // ignore seek before metadata
        }
      }
    });
  }, [active?.id, active?.video, reducedMotion]);

  /** 短防抖：快速划过时避免每行都触发布局/切图 */
  const selectItem = (id: string, immediate = false) => {
    if (hoverTimerRef.current) clearTimeout(hoverTimerRef.current);
    if (immediate) {
      setActiveId(id);
      return;
    }
    hoverTimerRef.current = setTimeout(() => setActiveId(id), 60);
  };

  const list = (
    <ul className={styles.list} role="listbox" aria-label={`${block.title}能力列表`}>
      {block.items.map((item) => {
        const selected = item.id === active?.id;
        return (
          <li key={item.id} role="option" aria-selected={selected}>
            <button
              type="button"
              className={`${styles.item} ${selected ? styles.itemActive : ''}`}
              onMouseEnter={() => selectItem(item.id)}
              onFocus={() => selectItem(item.id, true)}
              onClick={() => selectItem(item.id, true)}
            >
              <span className={styles.itemTitle}>{item.title}</span>
              <div
                className={`${styles.itemDescWrap} ${selected ? styles.itemDescWrapOpen : ''}`}
              >
                <div className={styles.itemDescInner}>
                  <p className={styles.itemDesc}>{item.description}</p>
                </div>
              </div>
            </button>
          </li>
        );
      })}
    </ul>
  );

  const media = (
    <div className={styles.media}>
      {block.items.map((item) => {
        const selected = item.id === active?.id;
        const showVideo = Boolean(item.video) && !reducedMotion;
        if (showVideo && item.video) {
          return (
            <video
              key={item.id}
              ref={(el) => {
                if (el) videoRefs.current.set(item.id, el);
                else videoRefs.current.delete(item.id);
              }}
              className={`${styles.preview} ${selected ? styles.previewActive : ''}`}
              src={item.video}
              poster={item.preview}
              controls
              controlsList="nodownload"
              muted
              autoPlay={selected}
              loop
              playsInline
              preload={selected ? 'auto' : 'metadata'}
              aria-hidden={!selected}
            />
          );
        }
        return (
          <img
            key={item.id}
            className={`${styles.preview} ${selected ? styles.previewActive : ''}`}
            src={item.preview}
            alt=""
            loading="lazy"
            decoding="async"
            width={640}
            height={426}
            aria-hidden={!selected}
          />
        );
      })}
    </div>
  );

  return (
    <section className={styles.block} aria-labelledby={`home-model-${block.id}`}>
      <h2 id={`home-model-${block.id}`} className={styles.blockTitle}>
        {block.title}
      </h2>
      <div className={`${styles.row} ${mediaLeft ? styles.rowMediaLeft : styles.rowMediaRight}`}>
        {mediaLeft ? (
          <>
            {media}
            {list}
          </>
        ) : (
          <>
            {list}
            {media}
          </>
        )}
      </div>
    </section>
  );
}

export function ModelShowcase() {
  return (
    <div className={styles.wrap}>
      {HOME_MODEL_SHOWCASE.map((block) => (
        <ModelShowcaseBlockView key={block.id} block={block} />
      ))}
    </div>
  );
}
