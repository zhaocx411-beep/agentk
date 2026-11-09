package com.kuaishou.agent.operation.backend.platform.service;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

import org.springframework.stereotype.Service;

import com.kuaishou.agent.operation.backend.model.dto.UserInfoResponse;
import com.kuaishou.agent.operation.backend.platform.dto.MenuPermissionTreeNode;
import com.kuaishou.agent.operation.backend.platform.entity.MenuPermission;

@Service
public class UserInfoService {

    private static final String ROLE_ADMIN = "ADMIN";
    private static final String ROLE_SUPER_ADMIN = "SUPER_ADMIN";
    private static final String ROLE_CHAT_REVIEWER = "chat_reviewer";
    private static final String ROLE_MENU_PREFIX = "menu:";
    private static final String SESSION_REVIEW_URL = "/session-review";
    private static final String SESSION_REVIEW_NAME = "数据回看";
    private static final String PERMISSION_CONTROL_URL = "/permission-control";
    private static final String PERMISSION_CONTROL_NAME = "权限控制";

    private final PlatformAdminService platformAdminService;
    private final WhiteListUserService whiteListUserService;
    private final MenuPermissionService menuPermissionService;

    public UserInfoService(PlatformAdminService platformAdminService,
            WhiteListUserService whiteListUserService,
            MenuPermissionService menuPermissionService) {
        this.platformAdminService = platformAdminService;
        this.whiteListUserService = whiteListUserService;
        this.menuPermissionService = menuPermissionService;
    }

    public UserInfoResponse build(String username, String name) {
        List<String> roles = platformAdminService.listActiveRoles(username);
        if (roles == null) {
            roles = Collections.emptyList();
        }
        String role = roles.isEmpty() ? "" : roles.get(0);
        UserInfoResponse response = new UserInfoResponse(username, name, role, roles);
        fillMenuPermissionInfo(response, username, roles);
        return response;
    }

    private void fillMenuPermissionInfo(UserInfoResponse response, String username, List<String> roles) {
        boolean superAdmin = containsRole(roles, ROLE_SUPER_ADMIN);
        boolean admin = superAdmin || containsRole(roles, ROLE_ADMIN);
        boolean sessionReviewer = admin || containsRole(roles, ROLE_CHAT_REVIEWER);
        List<MenuPermission> allMenus = menuPermissionService.listAllMenuPermissions(null, null);
        Set<Integer> permissionControlIds = collectMenuTreeIds(allMenus, PERMISSION_CONTROL_URL, PERMISSION_CONTROL_NAME);
        Set<Integer> sessionReviewIds = collectMenuTreeIds(allMenus, SESSION_REVIEW_URL, SESSION_REVIEW_NAME);

        LinkedHashSet<Integer> allowedIds = new LinkedHashSet<>();
        if (admin) {
            addAllMenuIds(allowedIds, allMenus);
        } else {
            addAllMenuIds(allowedIds, allMenus);
            addMenuIds(allowedIds, whiteListUserService.getUserMenu(username));
            addMenuIdsFromRoles(allowedIds, roles);
            allowedIds.removeAll(permissionControlIds);
            if (sessionReviewer) {
                allowedIds.addAll(sessionReviewIds);
            } else {
                allowedIds.removeAll(sessionReviewIds);
            }
        }

        List<MenuPermissionTreeNode> urlTree = buildUrlTree(allMenus, allowedIds);
        List<String> modules = extractMenuNames(allMenus, allowedIds);
        response.setAdmin(admin);
        response.setSuperAdmin(superAdmin);
        response.setModules(modules);
        response.setMenu(String.join(",", modules));
        response.setUrl(urlTree);
    }

    private void addAllMenuIds(LinkedHashSet<Integer> allowedIds, List<MenuPermission> allMenus) {
        for (MenuPermission menu : allMenus) {
            if (menu != null && menu.getId() != null) {
                allowedIds.add(menu.getId());
            }
        }
    }

    /**
     * 匹配指定路由/名称的节点及其子孙（父级与子级同名同路由时也能整棵摘掉或加回）。
     */
    private Set<Integer> collectMenuTreeIds(List<MenuPermission> allMenus, String menuUrl, String menuName) {
        Set<Integer> roots = new HashSet<>();
        if (allMenus == null) {
            return roots;
        }
        for (MenuPermission menu : allMenus) {
            if (menu == null || menu.getId() == null) {
                continue;
            }
            boolean urlMatched = menuUrl != null && menuUrl.equals(menu.getMenuUrl());
            boolean nameMatched = menuName != null && menuName.equals(menu.getMenuName());
            if (urlMatched || nameMatched) {
                roots.add(menu.getId());
            }
        }
        if (roots.isEmpty()) {
            return roots;
        }
        Set<Integer> ids = new LinkedHashSet<>(roots);
        boolean grew = true;
        while (grew) {
            grew = false;
            for (MenuPermission menu : allMenus) {
                if (menu == null || menu.getId() == null || ids.contains(menu.getId())) {
                    continue;
                }
                Integer parentId = menu.getParentId() == null ? 0 : menu.getParentId();
                if (parentId != 0 && ids.contains(parentId)) {
                    ids.add(menu.getId());
                    grew = true;
                }
            }
        }
        return ids;
    }

    private boolean containsRole(List<String> roles, String expectedRole) {
        if (roles == null || expectedRole == null) {
            return false;
        }
        for (String role : roles) {
            if (expectedRole.equalsIgnoreCase(role == null ? "" : role.trim())) {
                return true;
            }
        }
        return false;
    }

    private void addMenuIds(LinkedHashSet<Integer> allowedIds, String menu) {
        if (menu == null || menu.trim().isEmpty()) {
            return;
        }
        for (String part : menu.split(",")) {
            String token = part == null ? null : part.trim();
            if (token == null || token.isEmpty()) {
                continue;
            }
            try {
                allowedIds.add(Integer.parseInt(token));
            } catch (NumberFormatException ignore) {
                // menu 字段按约定存 ID；异常 token 忽略，避免影响 user/info 返回。
            }
        }
    }

    private void addMenuIdsFromRoles(LinkedHashSet<Integer> allowedIds, List<String> roles) {
        if (roles == null) {
            return;
        }
        for (String role : roles) {
            String token = role == null ? null : role.trim();
            if (token == null || !token.startsWith(ROLE_MENU_PREFIX)) {
                continue;
            }
            try {
                allowedIds.add(Integer.parseInt(token.substring(ROLE_MENU_PREFIX.length())));
            } catch (NumberFormatException ignore) {
                // 非法 menu role 忽略。
            }
        }
    }

    private List<String> extractMenuNames(List<MenuPermission> allMenus, Set<Integer> allowedIds) {
        if (allowedIds == null || allowedIds.isEmpty()) {
            return Collections.emptyList();
        }
        Map<Integer, MenuPermission> menuMap = toMenuMap(allMenus);
        List<String> names = new ArrayList<>();
        for (Integer id : allowedIds) {
            MenuPermission menu = menuMap.get(id);
            if (menu != null && menu.getMenuName() != null) {
                names.add(menu.getMenuName());
            }
        }
        return names;
    }

    private List<MenuPermissionTreeNode> buildUrlTree(List<MenuPermission> allMenus, Set<Integer> allowedIds) {
        if (allowedIds == null || allowedIds.isEmpty()) {
            return Collections.emptyList();
        }
        Map<Integer, MenuPermission> allMap = toMenuMap(allMenus);
        LinkedHashSet<Integer> includedIds = new LinkedHashSet<>(allowedIds);
        for (Integer id : allowedIds) {
            Integer current = id;
            int guard = 0;
            while (current != null && guard++ < 10) {
                MenuPermission node = allMap.get(current);
                if (node == null) {
                    break;
                }
                Integer parentId = node.getParentId() == null ? 0 : node.getParentId();
                if (parentId == 0) {
                    break;
                }
                includedIds.add(parentId);
                current = parentId;
            }
        }

        LinkedHashMap<Integer, MenuPermissionTreeNode> nodeMap = new LinkedHashMap<>();
        for (Integer id : includedIds) {
            MenuPermission menu = allMap.get(id);
            if (menu == null || menu.getId() == null) {
                continue;
            }
            MenuPermissionTreeNode node = new MenuPermissionTreeNode();
            node.setId(menu.getId());
            node.setParentId(menu.getParentId() == null ? 0 : menu.getParentId());
            node.setLevel(menu.getLevel());
            node.setMenuName(menu.getMenuName());
            node.setMenuUrl(menu.getMenuUrl());
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

    private Map<Integer, MenuPermission> toMenuMap(List<MenuPermission> menus) {
        Map<Integer, MenuPermission> menuMap = new HashMap<>();
        if (menus == null) {
            return menuMap;
        }
        for (MenuPermission menu : menus) {
            if (menu != null && menu.getId() != null) {
                menuMap.put(menu.getId(), menu);
            }
        }
        return menuMap;
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
}
