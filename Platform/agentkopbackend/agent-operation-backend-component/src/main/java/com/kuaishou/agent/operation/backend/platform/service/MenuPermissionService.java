package com.kuaishou.agent.operation.backend.platform.service;

import java.util.List;

import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.platform.dto.MenuPermissionTreeNode;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.MenuPermission;

/**
 * 菜单权限 Service。
 */
public interface MenuPermissionService {
    List<MenuPermission> listByMenuNames(List<String> menuNames);

    List<MenuPermission> listByIds(List<Integer> ids);

    List<MenuPermission> listAllMenuPermissions(String menuName, String menuUrl);

    MenuPermission addMenuPermission(MenuPermission request);

    MenuPermission updateMenuPermission(MenuPermission request);

    int deleteMenuPermissions(List<Integer> ids);

    MenuPermission getMenuPermissionById(Integer id);

    Page<MenuPermission> listMenuPermissions(String menuName, String menuUrl, Integer pageNum, Integer pageSize);

    PageResult<MenuPermission> listMenuPermissions(String menuName, String menuUrl, Integer parentId,
            Integer level, Integer pageNum, Integer pageSize);

    List<MenuPermissionTreeNode> tree(String menuName, String menuUrl);

    List<Integer> parseIds(String ids);
}
