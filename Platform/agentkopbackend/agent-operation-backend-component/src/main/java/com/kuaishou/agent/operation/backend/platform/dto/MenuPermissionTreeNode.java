package com.kuaishou.agent.operation.backend.platform.dto;

import java.util.ArrayList;
import java.util.List;

import lombok.Data;

/**
 * 菜单权限树节点。
 */
@Data
public class MenuPermissionTreeNode {
    private Integer id;
    private Integer parentId;
    private Integer level;
    private String menuName;
    private String menuUrl;
    private List<MenuPermissionTreeNode> children = new ArrayList<>();
}
