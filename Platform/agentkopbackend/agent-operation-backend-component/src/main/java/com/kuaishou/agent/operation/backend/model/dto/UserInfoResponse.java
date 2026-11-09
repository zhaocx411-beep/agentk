package com.kuaishou.agent.operation.backend.model.dto;

import java.util.Collections;
import java.util.List;

import com.kuaishou.agent.operation.backend.platform.dto.MenuPermissionTreeNode;

import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * 当前登录用户信息。
 */
@Data
@NoArgsConstructor
public class UserInfoResponse {

    private String username;
    private String name;
    /**
     * 主角色：取自 {@code aop_platform_admin} 中 status=active 的第一条；无授权则为空串。
     * 常见值：{@code chat_reviewer}、{@code ADMIN}、{@code SUPER_ADMIN}。
     */
    private String role;
    /** 全部 active 角色（一人可多角色）。 */
    private List<String> roles;
    /** 前端可访问模块名称列表。 */
    private List<String> modules = Collections.emptyList();
    /** 可访问菜单名称，逗号分隔，兼容参考项目字段。 */
    private String menu = "";
    /** 是否拥有全量菜单权限。 */
    private boolean admin;
    /** 是否超级管理员：个人历史可查看他人 session。 */
    private boolean superAdmin;
    /** 可访问菜单路由树。 */
    private List<MenuPermissionTreeNode> url = Collections.emptyList();

    public UserInfoResponse(String username, String name) {
        this(username, name, "", Collections.emptyList());
    }

    public UserInfoResponse(String username, String name, String role, List<String> roles) {
        this.username = username;
        this.name = name;
        this.role = role == null ? "" : role;
        this.roles = roles == null ? Collections.emptyList() : roles;
    }
}
