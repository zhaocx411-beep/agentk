import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { routes, type MetaRouteObject } from '@/router';

/** 应用标题前缀 */
const APP_TITLE = import.meta.env.VITE_APP_TITLE || '可灵Agent中后台';

/**
 * 根据路径从路由配置中查找 meta.title
 */
function findRouteTitle(pathname: string, routeList: MetaRouteObject[]): string | undefined {
  for (const route of routeList) {
    // 匹配当前路由
    if (route.path === pathname || (route.index && pathname === '/')) {
      return route.meta?.title;
    }
    // 递归查找子路由
    if (route.children) {
      const title = findRouteTitle(pathname, route.children);
      if (title) return title;
    }
  }
  return undefined;
}

/**
 * useDocumentTitle - 根据当前路由自动更新浏览器标题
 *
 * 标题格式：页面标题 - 应用名称
 *
 * @example
 * // 在 App.tsx 或布局组件中调用
 * useDocumentTitle();
 */
export function useDocumentTitle() {
  const { pathname } = useLocation();

  useEffect(() => {
    const pageTitle = findRouteTitle(pathname, routes);
    document.title = pageTitle ? `${pageTitle} - ${APP_TITLE}` : APP_TITLE;
  }, [pathname]);
}
