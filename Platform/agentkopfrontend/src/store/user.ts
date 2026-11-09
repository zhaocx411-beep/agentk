import { create } from 'zustand';
import { get } from '@/utils/request';
import {
  canAccessRoute,
  collectAllowedMenuUrls,
  type BackendMenuUrl,
} from '@/router/routeRegistry';
import { LOCAL_ROUTE_MENU_ENABLED } from '@/config/localFeatures';

/** 用户信息类型（对应 /agent/operation/backend/user/info 接口返回） */
export interface UserInfo {
  username: string;
  name: string;
  role?: string;
  admin?: boolean;
  superAdmin?: boolean;
  /** @deprecated 兼容旧字段 */
  isAdmin?: boolean;
  roles?: string[];
  modules?: string[];
  menu?: string;
  /** 后端菜单权限树 */
  url?: BackendMenuUrl[];
}

/** 是否 SUPER_ADMIN（模版上传等仅超管可用） */
export function isSuperAdminUser(userInfo: UserInfo | null | undefined): boolean {
  if (!userInfo) return false;
  if (userInfo.superAdmin) return true;
  if (userInfo.role === 'SUPER_ADMIN') return true;
  return userInfo.roles?.includes('SUPER_ADMIN') ?? false;
}

function buildAllowedMenuUrls(userInfo: UserInfo | null): Set<string> {
  return collectAllowedMenuUrls(userInfo?.url);
}

/** 用户状态 Store */
interface UserState {
  userInfo: UserInfo | null;
  allowedMenuUrls: Set<string>;
  loaded: boolean;
  loading: boolean;
  ensureUserInfoLoaded: () => Promise<void>;
  /** 路由是否可访问（对齐后端 url.menuUrl） */
  hasRoutePermission: (path: string) => boolean;
  /** @deprecated 使用 hasRoutePermission */
  hasMenuPermission: (path: string) => boolean;
}

const USER_INFO_API = '/agent/operation/backend/user/info';

let loadingPromise: Promise<void> | null = null;

export const useUserStore = create<UserState>((set, getState) => ({
  userInfo: null,
  allowedMenuUrls: new Set<string>(),
  loaded: false,
  loading: false,

  ensureUserInfoLoaded: async () => {
    if (LOCAL_ROUTE_MENU_ENABLED) {
      set({ loaded: true, loading: false });
      return;
    }

    const state = getState();
    if (state.loaded) return;

    if (loadingPromise) return loadingPromise;
    loadingPromise = (async () => {
      set({ loading: true });
      try {
        const userInfo = await get<UserInfo>(USER_INFO_API);
        set({
          userInfo,
          allowedMenuUrls: buildAllowedMenuUrls(userInfo),
          loaded: true,
          loading: false,
        });
      } catch {
        set({ userInfo: null, allowedMenuUrls: new Set(), loaded: true, loading: false });
      } finally {
        loadingPromise = null;
      }
    })();
    return loadingPromise;
  },

  hasRoutePermission: (path: string) => {
    if (LOCAL_ROUTE_MENU_ENABLED) return true;
    const state = getState();
    return canAccessRoute(path, state.allowedMenuUrls, isSuperAdminUser(state.userInfo));
  },

  hasMenuPermission: (path: string) => getState().hasRoutePermission(path),
}));
