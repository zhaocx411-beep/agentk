/**
 * 监听哨兵是否滚出滚动容器，用于「默认展开、滚出后吸顶」
 */
import { useEffect, useRef, useState } from 'react';

export function useStickySentinel() {
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [stuck, setStuck] = useState(false);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel) return;
    const root = sentinel.closest('[data-catalog-scroll]');
    const observer = new IntersectionObserver(([entry]) => setStuck(!entry.isIntersecting), {
      root: root instanceof Element ? root : null,
      threshold: 0,
    });
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, []);

  return { sentinelRef, stuck };
}
