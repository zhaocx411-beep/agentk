import { useState, useEffect, useRef, useCallback } from 'react';

interface UseTableScrollYResult<T extends HTMLElement> {
  /** 容器 ref */
  ref: React.RefObject<T>;
  /** 计算出的表格滚动高度 */
  scrollY: number | undefined;
  /** 手动触发重新计算 */
  recalculate: () => void;
}

/**
 * 自动计算表格纵向滚动区域高度
 * 监听容器尺寸变化，动态计算 scroll.y 值，避免分页器被遮挡
 */
export function useTableScrollY<
  T extends HTMLElement = HTMLDivElement,
>(): UseTableScrollYResult<T> {
  const ref = useRef<T>(null);
  const [scrollY, setScrollY] = useState<number | undefined>(undefined);

  const recalculate = useCallback(() => {
    const el = ref.current;
    if (!el) return;

    /** 获取元素包含 margin 的外层总高度 */
    const getOuterHeight = (element: Element | null) => {
      if (!element) return 0;
      const style = window.getComputedStyle(element);
      const height = element.getBoundingClientRect().height;
      const marginTop = parseFloat(style.marginTop) || 0;
      const marginBottom = parseFloat(style.marginBottom) || 0;
      return height + marginTop + marginBottom;
    };

    // antd 启用固定表头后生成 .ant-table-header
    const headerEl = el.querySelector('.ant-table-header') || el.querySelector('.ant-table-thead');
    // 分页器在容器内部（DataTable 将分页器放在表格外部）
    const paginationEl = el.querySelector('.ant-pagination');
    if (!headerEl) return;

    const containerHeight = el.clientHeight;
    const headerHeight = getOuterHeight(headerEl);
    const paginationHeight = getOuterHeight(paginationEl);
    // 8px 安全余量应对 border 等额外高度，最小保证 100px
    setScrollY(Math.max(containerHeight - headerHeight - paginationHeight - 8, 100));
  }, []);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    // 首次测量 + 延迟二次测量（应对 antd 内部渲染延迟）
    const rafId = requestAnimationFrame(recalculate);
    const timeoutId = setTimeout(recalculate, 100);

    // ResizeObserver 监听容器变化
    const ro = new ResizeObserver(() => {
      recalculate();
      setTimeout(recalculate, 50);
    });
    ro.observe(el);

    return () => {
      cancelAnimationFrame(rafId);
      clearTimeout(timeoutId);
      ro.disconnect();
    };
  }, [recalculate]);

  return { ref, scrollY, recalculate };
}
