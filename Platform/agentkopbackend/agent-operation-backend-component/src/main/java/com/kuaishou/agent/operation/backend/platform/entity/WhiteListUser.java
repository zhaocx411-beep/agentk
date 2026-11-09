package com.kuaishou.agent.operation.backend.platform.entity;

import lombok.Data;

/**
 * 菜单白名单用户接口模型。
 * <p>
 * 实际授权数据复用 aop_platform_admin，不单独创建白名单表。
 */
@Data
public class WhiteListUser {
    /** 对应 aop_platform_admin 中该用户第一条受管授权记录 id，便于兼容旧接口按 id 操作。 */
    private Long id;
    private String username;
    /** 状态：0=禁用，1=启用。 */
    private Integer status;
    private Long createTime;
    private Long updateTime;
    private String remark;
    /** 多个角色逗号分隔，例如 SUPER_ADMIN,chat_reviewer。ADMIN 全量菜单；SUPER_ADMIN 含全量菜单、个人历史看他人、管理回看。 */
    private String role;
    /** 可访问菜单：推荐存 aop_menu_permission.id，逗号分隔。 */
    private String menu;
}
