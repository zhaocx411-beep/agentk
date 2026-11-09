/**
 * 权限控制 - API 接口
 */
import { get, post, del } from '@/utils/request';
import type { AdminUsersResponse, AddAdminParams, RemoveAdminParams } from './types';

/** 获取权限名单 */
export function getAdminUsers() {
  return get<AdminUsersResponse>('/agent/operation/backend/platform/admin/users');
}

/** 增加权限 */
export function addAdmin(data: AddAdminParams) {
  return post<unknown>('/agent/operation/backend/platform/admin/users', data);
}

/** 删除权限 */
export function removeAdmin(params: RemoveAdminParams) {
  return del<unknown>('/agent/operation/backend/platform/admin/users', params);
}
