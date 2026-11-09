/**
 * 全局通用类型定义
 */

/** 通用 Record 类型 */
export type AnyObject = Record<string, unknown>;

/** 通用组件 Props 基础接口 */
export interface BaseComponentProps {
  className?: string;
  style?: React.CSSProperties;
  children?: React.ReactNode;
}

/** 路由元数据 */
export interface RouteMeta {
  /** 页面标题 */
  title: string;
  /** 菜单图标 */
  icon?: string;
  /** 是否需要鉴权 */
  requireAuth?: boolean;
  /** 所需权限 */
  permissions?: string[];
  /** 是否在菜单中隐藏 */
  hidden?: boolean;
}
