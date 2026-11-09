import { create } from 'zustand';

/** 全局应用状态 */
interface AppState {
  /** 全局 Loading 状态 */
  loading: boolean;
  /** 侧边栏折叠 */
  collapsed: boolean;
  /** 设置 Loading */
  setLoading: (loading: boolean) => void;
  /** 切换侧边栏折叠 */
  toggleCollapsed: () => void;
}

/**
 * 全局应用状态管理
 * - 全局 loading 控制
 * - 侧边栏折叠状态
 */
export const useAppStore = create<AppState>((set) => ({
  loading: false,
  collapsed: false,

  setLoading: (loading: boolean) => set({ loading }),

  toggleCollapsed: () => set((state) => ({ collapsed: !state.collapsed })),
}));
