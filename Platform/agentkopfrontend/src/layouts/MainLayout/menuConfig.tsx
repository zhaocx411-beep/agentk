/**
 * 主导航菜单配置（顶栏 / 移动端抽屉共用）
 * 菜单项 key 对齐后端 user/info.url[].menuUrl，可见性由 canAccessRoute 判定
 */
import type { MenuProps } from 'antd';
import { canAccessRoute } from '@/router/routeRegistry';

/** 一级可点路由项（如首页） */
export interface MenuLeaf {
  type: 'leaf';
  key: string;
  label: string;
}

/** 一级分组：仅作归类，展开后点二级路由 */
export interface MenuGroup {
  type: 'group';
  key: string;
  label: string;
  children: Array<{
    key: string;
    label: string;
  }>;
}

export type MenuEntry = MenuLeaf | MenuGroup;

/**
 * 顶栏菜单（仅已开放入口）
 * Pipeline 编辑器、Agent 调试台、Pipeline 运行等已下线项不在此配置
 */
export const MENU_ENTRIES: MenuEntry[] = [
  {
    type: 'leaf',
    key: '/',
    label: '首页',
  },
  {
    type: 'group',
    key: 'agent-assets',
    label: 'Agent 资产',
    children: [
      { key: '/agents/mine', label: '我的 Agent' },
      { key: '/agent100', label: 'Agent100' },
      { key: '/tools', label: '工具广场' },
      { key: '/oss-tools', label: '开源能力灵感库' },
    ],
  },
  {
    type: 'leaf',
    key: '/agents/new',
    label: '自定义Agent',
  },
  {
    type: 'group',
    key: 'asset-library',
    label: '资产库',
    children: [
      { key: '/works-gallery', label: '团队作品展示' },
      { key: '/my-works', label: '我的作品集' },
      { key: '/material-library', label: '素材库' },
    ],
  },
  {
    type: 'group',
    key: 'evaluation',
    label: '评测与数据',
    children: [
      { key: '/offline-run/datasets', label: '数据集管理' },
      { key: '/offline-run/jobs', label: '离线跑数' },
      { key: '/session-review', label: '数据回看' },
    ],
  },
  {
    type: 'group',
    key: 'system',
    label: '系统设置',
    children: [{ key: '/permission-control', label: '权限控制' }],
  },
];

/** 按后端 url 权限过滤后的 Ant Design Menu items */
export function buildPermittedMenuItems(
  allowedMenuUrls: Set<string>,
  isSuperAdmin: boolean,
  showAllLocalRoutes = false,
): MenuProps['items'] {
  const items: MenuProps['items'] = [];
  const canShow = (path: string) =>
    showAllLocalRoutes || canAccessRoute(path, allowedMenuUrls, isSuperAdmin);

  for (const entry of MENU_ENTRIES) {
    if (entry.type === 'leaf') {
      if (!canShow(entry.key)) {
        continue;
      }
      items.push({ key: entry.key, label: entry.label });
      continue;
    }

    const children = entry.children
      .filter((child) => canShow(child.key))
      .map((child) => ({ key: child.key, label: child.label }));

    if (children.length === 0) continue;

    items.push({
      key: entry.key,
      label: entry.label,
      children,
    });
  }

  return items;
}

/** 路由 pathname 是否可展示在菜单（与 hasRoutePermission 一致，供外部校验） */
export function canShowMenuPath(
  path: string,
  allowedMenuUrls: Set<string>,
  isSuperAdmin: boolean,
): boolean {
  return canAccessRoute(path, allowedMenuUrls, isSuperAdmin);
}

/**
 * 把当前路径归到某个菜单项
 * 详情页没有独立菜单，归到其入口列表页保持高亮
 */
export function resolveSelectedKey(pathname: string): string {
  if (pathname === '/' || pathname === '') return '/';
  if (pathname === '/agents/new') return '/agents/new';
  if (pathname === '/agent100' || pathname.startsWith('/agent100/')) return '/agent100';
  if (pathname === '/agents/official' || pathname.startsWith('/agents/official/')) {
    return '/agents/official';
  }
  if (pathname.startsWith('/agents')) return '/agents/mine';
  if (pathname.startsWith('/offline-run/jobs')) return '/offline-run/jobs';
  if (pathname.startsWith('/offline-run')) return '/offline-run/datasets';
  if (pathname.startsWith('/session-review')) return '/session-review';
  if (pathname.startsWith('/model-arena/manage')) return '/model-arena/manage';
  if (pathname.startsWith('/model-arena')) return '/model-arena';
  return pathname;
}

/** 根据选中项反查所属一级分组（顶栏高亮分组用；首页等叶子项无分组） */
export function resolveGroupKey(selectedKey: string): string | undefined {
  for (const entry of MENU_ENTRIES) {
    if (entry.type !== 'group') continue;
    if (entry.children.some((child) => child.key === selectedKey)) return entry.key;
  }
  return undefined;
}
