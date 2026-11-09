/**
 * 输入框上方横滚精选：hover 显示箭头，横滚不带动页面纵向滚动
 */
import { useCallback, useEffect, useRef, useState, type PointerEvent } from 'react';
import { LeftOutlined, ReloadOutlined, RightOutlined } from '@ant-design/icons';
import { Skeleton } from 'antd';
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import styles from './OfficialLoopPromptStrip.module.less';

interface ScrollEdges {
  left: boolean;
  right: boolean;
}

interface OfficialLoopPromptStripProps {
  items: ChatSuggestionItem[];
  loading?: boolean;
  refreshing?: boolean;
  skeletonCount?: number;
  onPick: (item: ChatSuggestionItem) => void;
  onRefresh?: () => void;
}

const SCROLL_STEP_PX = 240;
const SCROLL_EDGE_EPSILON = 4;

function OfficialLoopPromptStrip({
  items,
  loading = false,
  refreshing = false,
  skeletonCount = 6,
  onPick,
  onRefresh,
}: OfficialLoopPromptStripProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState<ScrollEdges>({ left: false, right: false });

  const syncEdges = useCallback(() => {
    const node = scrollRef.current;
    if (!node) return;
    const maxScroll = node.scrollWidth - node.clientWidth;
    if (maxScroll <= SCROLL_EDGE_EPSILON) {
      setEdges({ left: false, right: false });
      return;
    }
    setEdges({
      left: node.scrollLeft > SCROLL_EDGE_EPSILON,
      right: node.scrollLeft < maxScroll - SCROLL_EDGE_EPSILON,
    });
  }, []);

  useEffect(() => {
    syncEdges();
    const node = scrollRef.current;
    if (!node) return;
    const observer = new ResizeObserver(syncEdges);
    observer.observe(node);
    return () => observer.disconnect();
  }, [items, loading, syncEdges]);

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) return;
    const onWheel = (event: WheelEvent) => {
      if (node.scrollWidth <= node.clientWidth + SCROLL_EDGE_EPSILON) return;
      const horizontal = Math.abs(event.deltaX) > Math.abs(event.deltaY);
      if (horizontal || event.deltaY !== 0) {
        event.preventDefault();
        event.stopPropagation();
        node.scrollLeft += horizontal ? event.deltaX : event.deltaY;
      }
    };
    node.addEventListener('wheel', onWheel, { passive: false });
    return () => node.removeEventListener('wheel', onWheel);
  }, [items, loading]);

  const scrollBy = (delta: number) => {
    scrollRef.current?.scrollBy({ left: delta, behavior: 'smooth' });
  };

  const handleArrowPointerDown = (event: PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
  };

  if (!loading && items.length === 0) return null;

  return (
    <div className={styles.shell}>
      <div className={styles.row}>
        <div className={styles.track}>
          <div
            ref={scrollRef}
            className={styles.strip}
            aria-label="快捷灵感"
            onScroll={syncEdges}
          >
            {loading
              ? Array.from({ length: skeletonCount }, (_, index) => (
                  <Skeleton.Button key={`strip-skeleton-${index}`} active className={styles.pillSkeleton} />
                ))
              : items.map((item, index) => (
                  <button
                    key={`${item.label}-${index}`}
                    type="button"
                    className={styles.pill}
                    title={item.prompt}
                    onClick={() => onPick(item)}
                  >
                    {item.label}
                  </button>
                ))}
          </div>
          <span
            className={`${styles.fade} ${styles.fadeLeft} ${edges.left ? styles.fadeOn : ''}`}
            aria-hidden
          />
          <span
            className={`${styles.fade} ${styles.fadeRight} ${edges.right ? styles.fadeOn : ''}`}
            aria-hidden
          />
          {edges.left ? (
            <button
              type="button"
              className={`${styles.arrowOverlay} ${styles.arrowLeft} ${styles.arrowReady}`}
              aria-label="向左滚动"
              onPointerDown={handleArrowPointerDown}
              onClick={() => scrollBy(-SCROLL_STEP_PX)}
            >
              <LeftOutlined aria-hidden />
            </button>
          ) : null}
          {edges.right ? (
            <button
              type="button"
              className={`${styles.arrowOverlay} ${styles.arrowRight} ${styles.arrowReady}`}
              aria-label="向右滚动"
              onPointerDown={handleArrowPointerDown}
              onClick={() => scrollBy(SCROLL_STEP_PX)}
            >
              <RightOutlined aria-hidden />
            </button>
          ) : null}
        </div>
        {onRefresh ? (
          <button
            type="button"
            className={styles.refresh}
            disabled={loading || refreshing}
            aria-label="换一批灵感"
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.stopPropagation();
              onRefresh();
            }}
          >
            <ReloadOutlined spin={refreshing} aria-hidden />
            <span>换一换</span>
          </button>
        ) : null}
      </div>
    </div>
  );
}

export default OfficialLoopPromptStrip;
