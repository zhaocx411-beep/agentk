package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;

import com.kuaishou.agent.operation.backend.platform.entity.PlatformAdmin;

/**
 * 平台管理员判定与 DB 授权管理。
 */
public interface PlatformAdminService {

    /** 是否全局管理员（chat_reviewer 或 SUPER_ADMIN，可进管理回看）。 */
    boolean isGlobalAdmin(String userId);

    /** 是否平台管理员（ADMIN 或 SUPER_ADMIN，与 user/info 的 admin 标志一致）。 */
    boolean isPlatformAdmin(String userId);

    /** 是否超级管理员（可在个人历史中查看他人 session）。 */
    boolean isSuperAdmin(String userId);

    /** 当前未禁用的授权数量（用于空表引导首个管理员）。 */
    long countActiveAdmins();

    /** 返回表中未禁用的全部授权（不按角色过滤）。 */
    List<PlatformAdmin> listDbAdmins();

    /** 当前用户在表中 status=active 的全部角色（按 id 升序）。 */
    List<String> listActiveRoles(String userId);

    PlatformAdmin grant(String userId, String role, String operatorUserId);

    void revoke(String userId, String role, String operatorUserId);

    /** permissionStub 为 true 时跳过判定。 */
    void assertAdminOrBootstrap(String userId, boolean permissionStub);

    void assertGlobalAdmin(String userId, boolean permissionStub);

    /** permissionStub 为 true 时跳过判定。平台管理员 = ADMIN / SUPER_ADMIN。 */
    void assertPlatformAdmin(String userId, boolean permissionStub);
}
