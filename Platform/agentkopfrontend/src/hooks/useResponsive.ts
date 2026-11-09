import { useState, useEffect } from 'react';

const MOBILE_BREAKPOINT = 768;

/**
 * 响应式断点 hook
 * 根据视口宽度判断当前是否为移动端
 */
export function useResponsive() {
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < MOBILE_BREAKPOINT);

  useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`);

    const handleChange = (e: MediaQueryListEvent) => {
      setIsMobile(e.matches);
    };

    // 初始同步（SSR hydration 场景）
    setIsMobile(mql.matches);

    mql.addEventListener('change', handleChange);
    return () => mql.removeEventListener('change', handleChange);
  }, []);

  return { isMobile };
}
