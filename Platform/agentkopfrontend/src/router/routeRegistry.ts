/**
 * 前端已开放路由 ↔ 后端 user/info.url.menuUrl 权限映射
 */

/** 与后端 user/info.url[] 单项结构对齐 */
export interface BackendMenuUrl {
  id: number;
  parentId?: number;
  level?: number;
  menuName: string;
  menuUrl: string;
  children?: BackendMenuUrl[];
}

/** 前端路由节点（children 内 path 为实际 pathname 模式） */
export interface FrontendRouteNode {
  menuName: string;
  /** 路由 path 模式，如 /agents/:agentId */
  path: string;
  /** 是否在顶栏/抽屉菜单直接展示 */
  inNav?: boolean;
  /** 页面 title（router meta） */
  title?: string;
}

/** 带 children 的完整路由树（仅已开放入口） */
export interface FrontendRouteMenuTree {
  menuName: string;
  menuUrl: string;
  children: FrontendRouteNode[];
}

/**
 * 当前前端已开放路由树（不含菜单隐藏 / router 注释下线的页面）
 * 权限：优先匹配 children.path 对应的后端 menuUrl；模块内存在子级授权时为细粒度模式
 */
export const OPEN_FRONTEND_ROUTE_TREE: FrontendRouteMenuTree[] = [
  {
    menuName: '首页',
    menuUrl: '/',
    children: [{ menuName: '平台首页', path: '/', inNav: true, title: '首页' }],
  },
  {
    menuName: 'Agent管理平台',
    menuUrl: '/agents',
    children: [
      { menuName: '我的 Agent', path: '/agents/mine', inNav: true, title: '我的 Agent' },
      { menuName: '自定义Agent', path: '/agents/new', inNav: true, title: '自定义Agent' },
      { menuName: '官方 Agent 目录', path: '/agents/official', title: '可灵Agent中后台' },
      { menuName: '旧地址转发', path: '/agents', title: '转发' },
      { menuName: '自建 Agent 配置', path: '/agents/:agentId', title: 'Agent 配置' },
      { menuName: '工作流 Agent 配置', path: '/agents/wf/:workflowId', title: '工作流 Agent 配置' },
      {
        menuName: '官方 Agent 自由对话',
        path: '/agents/official/chat/:sessionId',
        title: '官方 Agent 体验',
      },
      {
        menuName: '工作流生成控制台',
        path: '/agents/official/console/:sessionId',
        title: '工作流生成控制台',
      },
      {
        menuName: '官方 Agent 市场详情',
        path: '/agents/official/app/:cardKey',
        title: 'Agent 详情',
      },
      { menuName: 'Agent100', path: '/agent100', inNav: true, title: 'Agent100' },
      { menuName: 'Agent100 工作台', path: '/agent100/:agentId', title: 'Agent100' },
      { menuName: '工具广场', path: '/tools', inNav: true, title: '工具广场' },
      { menuName: '工具详情', path: '/tools/:toolName', title: '工具详情' },
      { menuName: '开源能力灵感库', path: '/oss-tools', inNav: true, title: '开源能力灵感库' },
      { menuName: '素材库', path: '/material-library', inNav: true, title: '素材库' },
    ],
  },
  {
    menuName: '作品展示',
    menuUrl: '/works-gallery',
    children: [
      { menuName: '团队作品展示', path: '/works-gallery', inNav: true, title: '团队作品展示' },
      { menuName: '我的作品集', path: '/my-works', inNav: true, title: '我的作品集' },
    ],
  },
  {
    menuName: '离线跑数',
    menuUrl: '/offline-run',
    children: [
      { menuName: '离线跑数入口', path: '/offline-run', title: '重定向至数据集管理' },
      { menuName: '数据集管理', path: '/offline-run/datasets', inNav: true, title: '数据集管理' },
      { menuName: '离线跑数任务', path: '/offline-run/jobs', inNav: true, title: '离线跑数' },
    ],
  },
  {
    menuName: '数据回看',
    menuUrl: '/session-review',
    children: [
      { menuName: '数据回看', path: '/session-review', inNav: true, title: '数据回看' },
      {
        menuName: '对话回看',
        path: '/session-review/:agentkSessionId',
        title: '对话回看',
      },
    ],
  },
  {
    menuName: '多模型对战',
    menuUrl: '/model-arena',
    children: [
      { menuName: '多模型对战', path: '/model-arena', title: '多模型对战' },
      { menuName: '对战管理', path: '/model-arena/manage', title: '模型对战管理' },
    ],
  },
  {
    menuName: '权限控制',
    menuUrl: '/permission-control',
    children: [
      { menuName: '权限控制', path: '/permission-control', inNav: true, title: '权限控制' },
    ],
  },
];

/** @deprecated 扁平列表，请用 OPEN_FRONTEND_ROUTE_TREE */
export const OPEN_ROUTE_MENUS: BackendMenuUrl[] = OPEN_FRONTEND_ROUTE_TREE.flatMap((group, index) => [
  {
    id: index,
    menuName: group.menuName,
    menuUrl: group.menuUrl,
    children: group.children.map((child, childIndex) => ({
      id: index * 100 + childIndex,
      menuName: child.menuName,
      menuUrl: group.menuUrl,
    })),
  },
]);

/** 从 user/info 提取允许访问的后端 menuUrl 集合 */
export function collectAllowedMenuUrls(urls: BackendMenuUrl[] | undefined): Set<string> {
  const allowed = new Set<string>();
  if (!urls?.length) return allowed;
  const walk = (items: BackendMenuUrl[]) => {
    for (const item of items) {
      const key = item.menuUrl?.trim();
      if (key) allowed.add(key);
      if (item.children?.length) walk(item.children);
    }
  };
  walk(urls);
  return allowed;
}

function normalizePathname(pathname: string): string {
  const raw = pathname.split('?')[0]?.trim() || '/';
  if (raw === '/') return '/';
  return raw.replace(/\/+$/, '') || '/';
}

/** pathname 是否匹配后端 menuUrl（支持 :param 段） */
export function matchesMenuUrlPattern(pathname: string, menuUrl: string): boolean {
  const path = normalizePathname(pathname);
  const pattern = normalizePathname(menuUrl);
  if (path === pattern) return true;

  const pathSegs = path.split('/').filter(Boolean);
  const patternSegs = pattern.split('/').filter(Boolean);
  if (pathSegs.length !== patternSegs.length) return false;

  return patternSegs.every((seg, index) => seg.startsWith(':') || seg === pathSegs[index]);
}

/** 静态段越多、路径越长，匹配优先级越高（避免 /agents/official 误匹配 /agents/:agentId） */
function patternSpecificity(menuUrl: string): number {
  const segs = normalizePathname(menuUrl).split('/').filter(Boolean);
  const staticWeight = segs.reduce(
    (sum, seg) => sum + (seg.startsWith(':') ? 1 : 100),
    0,
  );
  return staticWeight * 10 + segs.length;
}

/** 在前端路由树中为 pathname 解析最具体的 menuUrl 模式 */
export function resolveCanonicalMenuUrl(pathname: string): string | null {
  const path = normalizePathname(pathname);
  let best: string | null = null;
  let bestScore = -1;

  for (const group of OPEN_FRONTEND_ROUTE_TREE) {
    for (const child of group.children) {
      if (!matchesMenuUrlPattern(path, child.path)) continue;
      const score = patternSpecificity(child.path);
      if (score > bestScore) {
        bestScore = score;
        best = child.path;
      }
    }
  }

  return best;
}

/** 模块内是否已下发子页面级权限（存在比父级更具体的 menuUrl） */
function isModuleGranular(allowedMenuUrls: Set<string>, moduleKey: string): boolean {
  for (const menuUrl of allowedMenuUrls) {
    if (menuUrl === moduleKey) continue;
    if (resolveRoutePermissionKey(menuUrl) === moduleKey) return true;
  }
  return false;
}

/** 前端 pathname → 所需后端 menuUrl 权限；null 表示登录即可（首页） */
export function resolveRoutePermissionKey(pathname: string): string | null {
  const path = pathname.split('?')[0]?.replace(/\/+$/, '') || '/';
  if (path === '/') return null;

  if (path.startsWith('/agents')) return '/agents';
  if (path.startsWith('/tools') || path.startsWith('/oss-tools') || path.startsWith('/agent100')) {
    return '/agents';
  }
  if (path.startsWith('/material-library')) return '/agents';
  if (path.startsWith('/works-gallery') || path.startsWith('/my-works')) return '/works-gallery';
  if (path.startsWith('/offline-run')) return '/offline-run';
  if (path.startsWith('/session-review')) return '/session-review';
  if (path.startsWith('/permission-control')) return '/permission-control';
  if (path.startsWith('/agent-playground')) return '/agent-playground';
  if (path.startsWith('/workflow-runner')) return '/workflow-runner';
  if (path.startsWith('/model-arena')) return '/model-arena';
  if (path.startsWith('/workflow')) return '/workflow';

  return path;
}

/** 是否可访问该路由（SUPER_ADMIN 放行全部） */
export function canAccessRoute(
  pathname: string,
  allowedMenuUrls: Set<string>,
  isSuperAdmin: boolean,
): boolean {
  if (isSuperAdmin) return true;

  const path = normalizePathname(pathname);
  if (path === '/') return true;
  // 能进「我的 Agent」即可创建自定义 Agent，避免细粒度权限漏掉新建入口
  if (path === '/agents/new') {
    return canAccessRoute('/agents/mine', allowedMenuUrls, isSuperAdmin);
  }
  if (allowedMenuUrls.size === 0) return false;

  const canonical = resolveCanonicalMenuUrl(path);
  if (canonical && allowedMenuUrls.has(canonical)) return true;
  if (allowedMenuUrls.has(path)) return true;

  const moduleKey = resolveRoutePermissionKey(path);
  if (moduleKey === null) return true;
  if (isModuleGranular(allowedMenuUrls, moduleKey)) return false;
  return allowedMenuUrls.has(moduleKey);
}
