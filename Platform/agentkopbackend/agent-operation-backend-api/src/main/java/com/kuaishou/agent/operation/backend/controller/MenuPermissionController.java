package com.kuaishou.agent.operation.backend.controller;

import java.util.List;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.MenuPermissionTreeNode;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.MenuPermission;
import com.kuaishou.agent.operation.backend.platform.service.MenuPermissionService;

/**
 * 菜单权限管理控制器（aop_menu_permission）。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/menuPermission")
public class MenuPermissionController {
    private static final Logger logger = LoggerFactory.getLogger(MenuPermissionController.class);

    private final MenuPermissionService menuPermissionService;

    public MenuPermissionController(MenuPermissionService menuPermissionService) {
        this.menuPermissionService = menuPermissionService;
    }

    @PostMapping("/add")
    public Message<MenuPermission> add(@RequestBody MenuPermission request) {
        try {
            if (request != null) {
                request.setId(null);
            }
            logger.info("新增菜单权限: menuName={}, menuUrl={}",
                    request == null ? null : request.getMenuName(),
                    request == null ? null : request.getMenuUrl());
            return Message.ok(menuPermissionService.addMenuPermission(request));
        } catch (IllegalArgumentException e) {
            logger.warn("新增菜单权限参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("新增菜单权限失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "新增失败：" + e.getMessage());
        }
    }

    @PostMapping("/update")
    public Message<MenuPermission> update(@RequestBody MenuPermission request) {
        try {
            logger.info("更新菜单权限: id={}", request == null ? null : request.getId());
            MenuPermission updated = menuPermissionService.updateMenuPermission(request);
            if (updated == null) {
                return Message.error(ResultCode.NOT_FOUND, "数据不存在");
            }
            return Message.ok(updated);
        } catch (IllegalArgumentException e) {
            logger.warn("更新菜单权限参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("更新菜单权限失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "更新失败：" + e.getMessage());
        }
    }

    @PostMapping("/delete")
    public Message<String> delete(@RequestParam String ids) {
        try {
            int deleted = menuPermissionService.deleteMenuPermissions(menuPermissionService.parseIds(ids));
            return Message.ok(String.format("成功删除 %d 条记录", deleted));
        } catch (IllegalArgumentException e) {
            logger.warn("删除菜单权限参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("删除菜单权限失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "删除失败：" + e.getMessage());
        }
    }

    @PostMapping("/get")
    public Message<MenuPermission> get(@RequestParam Integer id) {
        try {
            MenuPermission data = menuPermissionService.getMenuPermissionById(id);
            if (data == null) {
                return Message.error(ResultCode.NOT_FOUND, "数据不存在");
            }
            return Message.ok(data);
        } catch (IllegalArgumentException e) {
            logger.warn("查询菜单权限参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("查询菜单权限失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "查询失败：" + e.getMessage());
        }
    }

    @PostMapping("/list")
    public Message<PageResult<MenuPermission>> list(
            @RequestParam(required = false) String menuName,
            @RequestParam(required = false) String menuUrl,
            @RequestParam(required = false) Integer parentId,
            @RequestParam(required = false) Integer level,
            @RequestParam(defaultValue = "1") Integer pageNum,
            @RequestParam(defaultValue = "10") Integer pageSize) {
        try {
            return Message.ok(menuPermissionService.listMenuPermissions(
                    menuName, menuUrl, parentId, level, pageNum, pageSize));
        } catch (IllegalArgumentException e) {
            logger.warn("分页查询菜单权限参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("分页查询菜单权限失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "查询失败：" + e.getMessage());
        }
    }

    @PostMapping("/tree")
    public Message<List<MenuPermissionTreeNode>> tree(
            @RequestParam(required = false) String menuName,
            @RequestParam(required = false) String menuUrl) {
        try {
            return Message.ok(menuPermissionService.tree(menuName, menuUrl));
        } catch (IllegalArgumentException e) {
            logger.warn("树形查询菜单权限参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("树形查询菜单权限失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "查询失败：" + e.getMessage());
        }
    }
}
