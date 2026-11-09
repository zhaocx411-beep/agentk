/**
 * 权限控制 - 类型定义
 */

/** 管理员记录（对应 aop_platform_admin 表） */
export interface AdminUser {
  /** 主键 ID */
  id: number;
  /** 用户 ID（邮箱前缀） */
  userId: string;
  /** 角色（如 chat_reviewer） */
  role: string;
  /** 状态（active / inactive） */
  status: string;
  /** 创建人 */
  createdBy: string;
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/** 获取权限名单响应 */
export interface AdminUsersResponse {
  /** 活跃管理员数量 */
  activeCount: number;
  /** 管理员列表 */
  dbAdmins: AdminUser[];
  /** 数据来源 */
  source: string;
  /** 表名 */
  table: string;
}

/** 增加权限请求参数 */
export interface AddAdminParams {
  /** 用户 ID（邮箱前缀） */
  userId: string;
  /** 角色 */
  role: string;
}

/** 删除权限请求参数 */
export interface RemoveAdminParams {
  /** 用户 ID（邮箱前缀） */
  userId: string;
  /** 角色（可选） */
  role?: string;
}
