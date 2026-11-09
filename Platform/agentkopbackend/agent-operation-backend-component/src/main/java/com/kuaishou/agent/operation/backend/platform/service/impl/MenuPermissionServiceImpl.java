package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.LambdaUpdateWrapper;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.MenuPermissionMapper;
import com.kuaishou.agent.operation.backend.platform.dto.MenuPermissionTreeNode;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.MenuPermission;
import com.kuaishou.agent.operation.backend.platform.service.MenuPermissionService;

/**
 * 菜单权限 Service 实现。
 */
@Service
public class MenuPermissionServiceImpl implements MenuPermissionService {

    private final MenuPermissionMapper menuPermissionMapper;

    public MenuPermissionServiceImpl(MenuPermissionMapper menuPermissionMapper) {
        this.menuPermissionMapper = menuPermissionMapper;
    }

    @Override
    public List<MenuPermission> listByMenuNames(List<String> menuNames) {
        if (menuNames == null || menuNames.isEmpty()) {
            return Collections.emptyList();
        }
        List<String> safeNames = new ArrayList<>();
        for (String menuName : menuNames) {
            if (StringUtils.hasText(menuName)) {
                safeNames.add(menuName.trim());
            }
        }
        if (safeNames.isEmpty()) {
            return Collections.emptyList();
        }
        LambdaQueryWrapper<MenuPermission> query = new LambdaQueryWrapper<>();
        query.in(MenuPermission::getMenuName, safeNames);
        List<MenuPermission> rows = menuPermissionMapper.selectList(query);
        return rows == null ? Collections.emptyList() : rows;
    }

    @Override
    public List<MenuPermission> listByIds(List<Integer> ids) {
        if (ids == null || ids.isEmpty()) {
            return Collections.emptyList();
        }
        List<Integer> safeIds = new ArrayList<>();
        for (Integer id : ids) {
            if (id != null) {
                safeIds.add(id);
            }
        }
        if (safeIds.isEmpty()) {
            return Collections.emptyList();
        }
        List<MenuPermission> rows = menuPermissionMapper.selectBatchIds(safeIds);
        return rows == null ? Collections.emptyList() : rows;
    }

    @Override
    public List<MenuPermission> listAllMenuPermissions(String menuName, String menuUrl) {
        LambdaQueryWrapper<MenuPermission> query = buildQuery(menuName, menuUrl);
        List<MenuPermission> rows = menuPermissionMapper.selectList(query);
        return rows == null ? Collections.emptyList() : rows;
    }

    @Override
    public MenuPermission addMenuPermission(MenuPermission request) {
        if (request == null) {
            throw new IllegalArgumentException("request 不能为空");
        }
        if (!StringUtils.hasText(request.getMenuName())) {
            throw new IllegalArgumentException("menuName 不能为空");
        }
        if (!StringUtils.hasText(request.getMenuUrl())) {
            throw new IllegalArgumentException("menuUrl 不能为空");
        }
        validateLevelAndParent(request.getLevel(), request.getParentId());

        String normalizedMenuName = request.getMenuName().trim();
        if (existsMenuName(normalizedMenuName, null)) {
            throw new IllegalArgumentException("menuName 已存在");
        }

        MenuPermission entity = new MenuPermission();
        entity.setId(null);
        entity.setParentId(normalizeParentId(request.getParentId()));
        entity.setLevel(request.getLevel());
        entity.setMenuName(normalizedMenuName);
        entity.setMenuUrl(request.getMenuUrl().trim());
        menuPermissionMapper.insert(entity);
        return entity;
    }

    @Override
    public MenuPermission updateMenuPermission(MenuPermission request) {
        if (request == null) {
            throw new IllegalArgumentException("request 不能为空");
        }
        if (request.getId() == null) {
            throw new IllegalArgumentException("id 不能为空");
        }

        boolean hasMenuName = request.getMenuName() != null;
        boolean hasMenuUrl = request.getMenuUrl() != null;
        boolean hasParentId = request.getParentId() != null;
        boolean hasLevel = request.getLevel() != null;
        if (!hasMenuName && !hasMenuUrl && !hasParentId && !hasLevel) {
            throw new IllegalArgumentException("menuName/menuUrl/parentId/level 至少需要提供一个用于更新");
        }

        LambdaUpdateWrapper<MenuPermission> update = new LambdaUpdateWrapper<>();
        update.eq(MenuPermission::getId, request.getId());

        if (hasMenuName) {
            if (!StringUtils.hasText(request.getMenuName())) {
                throw new IllegalArgumentException("menuName 不能为空");
            }
            String normalizedMenuName = request.getMenuName().trim();
            if (existsMenuName(normalizedMenuName, request.getId())) {
                throw new IllegalArgumentException("menuName 已存在");
            }
            update.set(MenuPermission::getMenuName, normalizedMenuName);
        }
        if (hasMenuUrl) {
            if (!StringUtils.hasText(request.getMenuUrl())) {
                throw new IllegalArgumentException("menuUrl 不能为空");
            }
            update.set(MenuPermission::getMenuUrl, request.getMenuUrl().trim());
        }
        if (hasLevel || hasParentId) {
            MenuPermission existing = getMenuPermissionById(request.getId());
            if (existing == null) {
                return null;
            }
            Integer finalLevel = hasLevel ? request.getLevel() : existing.getLevel();
            Integer finalParentId = hasParentId ? request.getParentId() : existing.getParentId();
            validateLevelAndParent(finalLevel, finalParentId);
            if (hasLevel) {
                update.set(MenuPermission::getLevel, finalLevel);
            }
            if (hasParentId) {
                update.set(MenuPermission::getParentId, normalizeParentId(finalParentId));
            }
        }
        menuPermissionMapper.update(null, update);
        return getMenuPermissionById(request.getId());
    }

    @Override
    public int deleteMenuPermissions(List<Integer> ids) {
        if (ids == null || ids.isEmpty()) {
            return 0;
        }
        List<Integer> safeIds = new ArrayList<>();
        for (Integer id : ids) {
            if (id != null) {
                safeIds.add(id);
            }
        }
        if (safeIds.isEmpty()) {
            return 0;
        }
        return menuPermissionMapper.deleteBatchIds(safeIds);
    }

    @Override
    public MenuPermission getMenuPermissionById(Integer id) {
        if (id == null) {
            return null;
        }
        return menuPermissionMapper.selectById(id);
    }

    @Override
    public Page<MenuPermission> listMenuPermissions(String menuName, String menuUrl, Integer pageNum, Integer pageSize) {
        int actualPageNum = pageNum == null || pageNum <= 0 ? 1 : pageNum;
        int actualPageSize = pageSize == null || pageSize <= 0 ? 10 : pageSize;
        Page<MenuPermission> page = new Page<>(actualPageNum, actualPageSize);
        return menuPermissionMapper.selectPage(page, buildQuery(menuName, menuUrl));
    }

    @Override
    public PageResult<MenuPermission> listMenuPermissions(String menuName, String menuUrl, Integer parentId,
            Integer level, Integer pageNum, Integer pageSize) {
        Page<MenuPermission> page = listMenuPermissions(menuName, menuUrl, pageNum, pageSize);
        List<MenuPermission> records = page.getRecords() == null ? Collections.emptyList() : page.getRecords();
        if (parentId != null || level != null) {
            records = filterByTreeFields(records, parentId, level);
        }
        return PageResult.of(page.getCurrent(), page.getSize(), page.getTotal(), records);
    }

    @Override
    public List<MenuPermissionTreeNode> tree(String menuName, String menuUrl) {
        return buildTree(listAllMenuPermissions(menuName, menuUrl));
    }

    @Override
    public List<Integer> parseIds(String ids) {
        if (ids == null || ids.trim().isEmpty()) {
            throw new IllegalArgumentException("ids 不能为空");
        }
        List<Integer> result = new ArrayList<>();
        for (String part : ids.split(",")) {
            String trimmed = part == null ? null : part.trim();
            if (trimmed == null || trimmed.isEmpty()) {
                continue;
            }
            try {
                result.add(Integer.parseInt(trimmed));
            } catch (NumberFormatException e) {
                throw new IllegalArgumentException("ids 格式错误: " + trimmed);
            }
        }
        if (result.isEmpty()) {
            throw new IllegalArgumentException("ids 不能为空");
        }
        return result;
    }

    private List<MenuPermission> filterByTreeFields(List<MenuPermission> records, Integer parentId, Integer level) {
        List<MenuPermission> filtered = new ArrayList<>();
        for (MenuPermission record : records) {
            if (record == null) {
                continue;
            }
            if (parentId != null && (record.getParentId() == null || !parentId.equals(record.getParentId()))) {
                continue;
            }
            if (level != null && (record.getLevel() == null || !level.equals(record.getLevel()))) {
                continue;
            }
            filtered.add(record);
        }
        return filtered;
    }

    private List<MenuPermissionTreeNode> buildTree(List<MenuPermission> rows) {
        if (rows == null || rows.isEmpty()) {
            return Collections.emptyList();
        }
        LinkedHashMap<Integer, MenuPermissionTreeNode> nodeMap = new LinkedHashMap<>();
        for (MenuPermission row : rows) {
            if (row == null || row.getId() == null) {
                continue;
            }
            MenuPermissionTreeNode node = new MenuPermissionTreeNode();
            node.setId(row.getId());
            node.setParentId(row.getParentId() == null ? 0 : row.getParentId());
            node.setLevel(row.getLevel());
            node.setMenuName(row.getMenuName());
            node.setMenuUrl(row.getMenuUrl());
            nodeMap.put(node.getId(), node);
        }

        List<MenuPermissionTreeNode> roots = new ArrayList<>();
        for (MenuPermissionTreeNode node : nodeMap.values()) {
            Integer parentId = node.getParentId() == null ? 0 : node.getParentId();
            if (parentId == 0) {
                roots.add(node);
                continue;
            }
            MenuPermissionTreeNode parent = nodeMap.get(parentId);
            if (parent == null) {
                roots.add(node);
                continue;
            }
            parent.getChildren().add(node);
        }

        for (MenuPermissionTreeNode root : roots) {
            clearAfterThirdLevel(root, 1);
        }
        return roots;
    }

    private void clearAfterThirdLevel(MenuPermissionTreeNode node, int depth) {
        if (node == null || node.getChildren() == null) {
            return;
        }
        if (depth >= 3) {
            node.getChildren().clear();
            return;
        }
        for (MenuPermissionTreeNode child : node.getChildren()) {
            clearAfterThirdLevel(child, depth + 1);
        }
    }

    private LambdaQueryWrapper<MenuPermission> buildQuery(String menuName, String menuUrl) {
        LambdaQueryWrapper<MenuPermission> query = new LambdaQueryWrapper<>();
        if (StringUtils.hasText(menuName)) {
            query.like(MenuPermission::getMenuName, menuName.trim());
        }
        if (StringUtils.hasText(menuUrl)) {
            query.like(MenuPermission::getMenuUrl, menuUrl.trim());
        }
        query.orderByAsc(MenuPermission::getLevel)
                .orderByAsc(MenuPermission::getParentId)
                .orderByAsc(MenuPermission::getId);
        return query;
    }

    private boolean existsMenuName(String menuName, Integer excludeId) {
        if (!StringUtils.hasText(menuName)) {
            return false;
        }
        LambdaQueryWrapper<MenuPermission> query = new LambdaQueryWrapper<>();
        query.eq(MenuPermission::getMenuName, menuName.trim());
        if (excludeId != null) {
            query.ne(MenuPermission::getId, excludeId);
        }
        Integer count = menuPermissionMapper.selectCount(query);
        return count != null && count > 0;
    }

    private Integer normalizeParentId(Integer parentId) {
        return parentId == null ? 0 : parentId;
    }

    private void validateLevelAndParent(Integer level, Integer parentId) {
        if (level == null) {
            throw new IllegalArgumentException("level 不能为空（1/2/3）");
        }
        if (level < 1 || level > 3) {
            throw new IllegalArgumentException("level 仅支持 1/2/3");
        }
        Integer actualParentId = parentId == null ? 0 : parentId;
        if (level == 1 && actualParentId != 0) {
            throw new IllegalArgumentException("level=1 时 parentId 必须为 0");
        }
        if (level > 1 && actualParentId <= 0) {
            throw new IllegalArgumentException("level=" + level + " 时 parentId 必须为正整数");
        }
    }
}
