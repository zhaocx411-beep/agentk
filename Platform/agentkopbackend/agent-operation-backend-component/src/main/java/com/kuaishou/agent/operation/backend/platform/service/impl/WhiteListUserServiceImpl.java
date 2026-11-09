package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.time.LocalDateTime;
import java.time.ZoneId;
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
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.PlatformAdminMapper;
import com.kuaishou.agent.operation.backend.platform.entity.MenuPermission;
import com.kuaishou.agent.operation.backend.platform.entity.PlatformAdmin;
import com.kuaishou.agent.operation.backend.platform.entity.WhiteListUser;
import com.kuaishou.agent.operation.backend.platform.service.MenuPermissionService;
import com.kuaishou.agent.operation.backend.platform.service.WhiteListUserService;

/**
 * 菜单白名单用户 Service 实现。
 * <p>
 * 不单独建白名单表，复用 aop_platform_admin：
 * role=ADMIN 表示全量菜单，role=SUPER_ADMIN 表示超级管理员（全量菜单 + 个人历史看他人 + 管理回看），
 * role=chat_reviewer 保留已有数据回看角色，
 * role=menu:{menuId} 表示某个菜单权限。
 */
@Service
public class WhiteListUserServiceImpl implements WhiteListUserService {
    public static final String ROLE_ADMIN = "ADMIN";
    public static final String ROLE_SUPER_ADMIN = "SUPER_ADMIN";
    public static final String ROLE_CHAT_REVIEWER = "chat_reviewer";
    private static final String ROLE_MENU_PREFIX = "menu:";
    private static final String STATUS_ACTIVE = "active";
    private static final String STATUS_DISABLED = "disabled";

    private final PlatformAdminMapper platformAdminMapper;
    private final MenuPermissionService menuPermissionService;

    public WhiteListUserServiceImpl(PlatformAdminMapper platformAdminMapper,
            MenuPermissionService menuPermissionService) {
        this.platformAdminMapper = platformAdminMapper;
        this.menuPermissionService = menuPermissionService;
    }

    @Override
    public boolean isUserInWhiteList(String username) {
        if (!StringUtils.hasText(username)) {
            return false;
        }
        return !listActiveRows(username).isEmpty();
    }

    @Override
    public boolean isUserHasRole(String username, String requiredRole) {
        if (!StringUtils.hasText(username) || !StringUtils.hasText(requiredRole)) {
            return false;
        }
        Set<String> roleSet = activeRoleSet(username);
        String required = normalizeRole(requiredRole);
        if (ROLE_SUPER_ADMIN.equals(required)) {
            return roleSet.contains(ROLE_SUPER_ADMIN);
        }
        return roleSet.contains(ROLE_ADMIN)
                || roleSet.contains(ROLE_SUPER_ADMIN)
                || roleSet.contains(required);
    }

    @Override
    public List<String> getUserRoles(String username) {
        if (!StringUtils.hasText(username)) {
            return Collections.emptyList();
        }
        return new ArrayList<>(activeRoleSet(username));
    }

    @Override
    public String getUserMenu(String username) {
        if (!StringUtils.hasText(username)) {
            return "";
        }
        return String.join(",", extractMenuIds(listActiveRows(username)));
    }

    @Override
    public WhiteListUser addWhiteListUser(WhiteListUser request) {
        return saveUserGrants(request, null);
    }

    @Override
    public WhiteListUser updateWhiteListUser(WhiteListUser request) {
        if (request == null) {
            throw new IllegalArgumentException("request 不能为空");
        }
        if (request.getId() == null) {
            throw new IllegalArgumentException("id 不能为空");
        }
        PlatformAdmin anchor = platformAdminMapper.selectById(request.getId());
        if (anchor == null) {
            return null;
        }
        String username = StringUtils.hasText(request.getUsername()) ? request.getUsername() : anchor.getUserId();
        return saveUserGrants(request, username);
    }

    @Override
    public int deleteWhiteListUsers(List<Long> ids) {
        if (ids == null || ids.isEmpty()) {
            return 0;
        }
        int updated = 0;
        for (Long id : ids) {
            if (id == null) {
                continue;
            }
            PlatformAdmin anchor = platformAdminMapper.selectById(id);
            if (anchor == null || !StringUtils.hasText(anchor.getUserId())) {
                continue;
            }
            for (PlatformAdmin row : listRows(anchor.getUserId(), null)) {
                if (isManagedRole(row.getRole()) && !STATUS_DISABLED.equals(row.getStatus())) {
                    row.setStatus(STATUS_DISABLED);
                    row.setUpdatedAt(LocalDateTime.now());
                    platformAdminMapper.updateById(row);
                    updated++;
                }
            }
        }
        return updated;
    }

    @Override
    public WhiteListUser getWhiteListUserById(Long id) {
        if (id == null) {
            return null;
        }
        PlatformAdmin anchor = platformAdminMapper.selectById(id);
        if (anchor == null || !StringUtils.hasText(anchor.getUserId())) {
            return null;
        }
        return buildUser(anchor.getUserId(), listRows(anchor.getUserId(), null));
    }

    @Override
    public Page<WhiteListUser> listWhiteListUsers(String username, Integer status, Integer pageNum, Integer pageSize) {
        int actualPageNum = pageNum == null || pageNum <= 0 ? 1 : pageNum;
        int actualPageSize = pageSize == null || pageSize <= 0 ? 10 : pageSize;
        LambdaQueryWrapper<PlatformAdmin> query = new LambdaQueryWrapper<>();
        if (StringUtils.hasText(username)) {
            query.like(PlatformAdmin::getUserId, username.trim());
        }
        if (status != null) {
            query.eq(PlatformAdmin::getStatus, status == 1 ? STATUS_ACTIVE : STATUS_DISABLED);
        }
        query.orderByDesc(PlatformAdmin::getUpdatedAt).orderByDesc(PlatformAdmin::getId);
        Map<String, List<PlatformAdmin>> grouped = groupByUser(platformAdminMapper.selectList(query));
        List<WhiteListUser> all = new ArrayList<>();
        for (Map.Entry<String, List<PlatformAdmin>> entry : grouped.entrySet()) {
            WhiteListUser user = buildUser(entry.getKey(), entry.getValue());
            if (user != null && hasManagedGrant(user)) {
                all.add(user);
            }
        }

        int from = Math.min((actualPageNum - 1) * actualPageSize, all.size());
        int to = Math.min(from + actualPageSize, all.size());
        Page<WhiteListUser> page = new Page<>(actualPageNum, actualPageSize);
        page.setTotal(all.size());
        page.setRecords(new ArrayList<>(all.subList(from, to)));
        return page;
    }

    private WhiteListUser saveUserGrants(WhiteListUser request, String fallbackUsername) {
        if (request == null) {
            throw new IllegalArgumentException("request 不能为空");
        }
        String username = StringUtils.hasText(request.getUsername()) ? request.getUsername().trim() : fallbackUsername;
        if (!StringUtils.hasText(username)) {
            throw new IllegalArgumentException("username 不能为空");
        }

        String status = request.getStatus() == null || request.getStatus() == 1 ? STATUS_ACTIVE : STATUS_DISABLED;
        LinkedHashSet<String> requestedRoles = collectRequestedRoles(request);
        if (requestedRoles.isEmpty()) {
            throw new IllegalArgumentException("role/menu 至少需要配置一个");
        }

        List<PlatformAdmin> existingRows = listRows(username, null);
        for (PlatformAdmin row : existingRows) {
            if (!isManagedRole(row.getRole())) {
                continue;
            }
            if (!requestedRoles.contains(row.getRole()) && !STATUS_DISABLED.equals(row.getStatus())) {
                row.setStatus(STATUS_DISABLED);
                row.setUpdatedAt(LocalDateTime.now());
                platformAdminMapper.updateById(row);
            }
        }

        for (String role : requestedRoles) {
            upsertGrant(username, role, status);
        }
        return buildUser(username, listRows(username, null));
    }

    private void upsertGrant(String username, String role, String status) {
        PlatformAdmin existing = platformAdminMapper.selectOne(new LambdaQueryWrapper<PlatformAdmin>()
                .eq(PlatformAdmin::getUserId, username)
                .eq(PlatformAdmin::getRole, role)
                .last("LIMIT 1"));
        LocalDateTime now = LocalDateTime.now();
        if (existing != null) {
            existing.setStatus(status);
            existing.setUpdatedAt(now);
            platformAdminMapper.updateById(existing);
            return;
        }

        PlatformAdmin created = new PlatformAdmin();
        created.setUserId(username);
        created.setRole(role);
        created.setStatus(status);
        created.setCreatedBy("");
        created.setCreatedAt(now);
        created.setUpdatedAt(now);
        platformAdminMapper.insert(created);
    }

    private LinkedHashSet<String> collectRequestedRoles(WhiteListUser request) {
        LinkedHashSet<String> roles = new LinkedHashSet<>();
        for (String role : splitTokens(request.getRole())) {
            roles.add(normalizeRole(role));
        }
        for (String menuId : splitTokens(request.getMenu())) {
            roles.add(ROLE_MENU_PREFIX + menuId);
        }
        return roles;
    }

    private List<PlatformAdmin> listActiveRows(String username) {
        return listRows(username, STATUS_ACTIVE);
    }

    private List<PlatformAdmin> listRows(String username, String status) {
        if (!StringUtils.hasText(username)) {
            return Collections.emptyList();
        }
        LambdaQueryWrapper<PlatformAdmin> query = new LambdaQueryWrapper<>();
        query.eq(PlatformAdmin::getUserId, username.trim());
        if (StringUtils.hasText(status)) {
            query.eq(PlatformAdmin::getStatus, status);
        }
        query.orderByAsc(PlatformAdmin::getId);
        List<PlatformAdmin> rows = platformAdminMapper.selectList(query);
        return rows == null ? Collections.emptyList() : rows;
    }

    private Map<String, List<PlatformAdmin>> groupByUser(List<PlatformAdmin> rows) {
        Map<String, List<PlatformAdmin>> grouped = new LinkedHashMap<>();
        if (rows == null) {
            return grouped;
        }
        for (PlatformAdmin row : rows) {
            if (row == null || !StringUtils.hasText(row.getUserId())) {
                continue;
            }
            grouped.computeIfAbsent(row.getUserId(), key -> new ArrayList<>()).add(row);
        }
        return grouped;
    }

    private WhiteListUser buildUser(String username, List<PlatformAdmin> rows) {
        if (!StringUtils.hasText(username) || rows == null || rows.isEmpty()) {
            return null;
        }
        WhiteListUser user = new WhiteListUser();
        user.setUsername(username);
        user.setStatus(hasActiveRow(rows) ? 1 : 0);
        user.setId(firstId(rows));
        user.setCreateTime(toEpochMillis(firstCreatedAt(rows)));
        user.setUpdateTime(toEpochMillis(lastUpdatedAt(rows)));
        user.setRole(String.join(",", extractPlainRoles(rows)));
        user.setMenu(String.join(",", extractMenuIds(rows)));
        user.setRemark("复用 aop_platform_admin");
        return user;
    }

    private boolean hasManagedGrant(WhiteListUser user) {
        return user != null && (StringUtils.hasText(user.getRole()) || StringUtils.hasText(user.getMenu()));
    }

    private boolean hasActiveRow(List<PlatformAdmin> rows) {
        for (PlatformAdmin row : rows) {
            if (row != null && STATUS_ACTIVE.equals(row.getStatus()) && isManagedRole(row.getRole())) {
                return true;
            }
        }
        return false;
    }

    private Long firstId(List<PlatformAdmin> rows) {
        Long id = null;
        for (PlatformAdmin row : rows) {
            if (row == null || row.getId() == null || !isManagedRole(row.getRole())) {
                continue;
            }
            if (id == null || row.getId() < id) {
                id = row.getId();
            }
        }
        return id;
    }

    private LocalDateTime firstCreatedAt(List<PlatformAdmin> rows) {
        LocalDateTime first = null;
        for (PlatformAdmin row : rows) {
            if (row == null || row.getCreatedAt() == null || !isManagedRole(row.getRole())) {
                continue;
            }
            if (first == null || row.getCreatedAt().isBefore(first)) {
                first = row.getCreatedAt();
            }
        }
        return first;
    }

    private LocalDateTime lastUpdatedAt(List<PlatformAdmin> rows) {
        LocalDateTime last = null;
        for (PlatformAdmin row : rows) {
            if (row == null || row.getUpdatedAt() == null || !isManagedRole(row.getRole())) {
                continue;
            }
            if (last == null || row.getUpdatedAt().isAfter(last)) {
                last = row.getUpdatedAt();
            }
        }
        return last;
    }

    private long toEpochMillis(LocalDateTime time) {
        if (time == null) {
            return 0L;
        }
        return time.atZone(ZoneId.systemDefault()).toInstant().toEpochMilli();
    }

    private Set<String> activeRoleSet(String username) {
        LinkedHashSet<String> roles = new LinkedHashSet<>();
        for (PlatformAdmin row : listActiveRows(username)) {
            if (row != null && StringUtils.hasText(row.getRole())) {
                roles.add(normalizeRole(row.getRole()));
            }
        }
        return roles;
    }

    private List<String> extractPlainRoles(List<PlatformAdmin> rows) {
        LinkedHashSet<String> roles = new LinkedHashSet<>();
        for (PlatformAdmin row : rows) {
            if (row == null || !StringUtils.hasText(row.getRole()) || !isManagedRole(row.getRole())) {
                continue;
            }
            String role = row.getRole().trim();
            if (!role.startsWith(ROLE_MENU_PREFIX)) {
                roles.add(role);
            }
        }
        return new ArrayList<>(roles);
    }

    private List<String> extractMenuIds(List<PlatformAdmin> rows) {
        LinkedHashSet<String> menuIds = new LinkedHashSet<>();
        for (PlatformAdmin row : rows) {
            if (row == null || !StringUtils.hasText(row.getRole())) {
                continue;
            }
            String role = row.getRole().trim();
            if (role.startsWith(ROLE_MENU_PREFIX) && role.length() > ROLE_MENU_PREFIX.length()) {
                menuIds.add(role.substring(ROLE_MENU_PREFIX.length()));
            }
        }
        return new ArrayList<>(menuIds);
    }

    private List<String> splitTokens(String raw) {
        if (!StringUtils.hasText(raw)) {
            return Collections.emptyList();
        }
        List<String> tokens = new ArrayList<>();
        for (String part : raw.split(",")) {
            if (StringUtils.hasText(part)) {
                tokens.add(part.trim());
            }
        }
        return tokens;
    }

    private String normalizeRole(String role) {
        String normalized = role == null ? "" : role.trim();
        if (normalized.startsWith(ROLE_MENU_PREFIX)) {
            return normalized;
        }
        if (ROLE_ADMIN.equalsIgnoreCase(normalized)) {
            return ROLE_ADMIN;
        }
        if (ROLE_SUPER_ADMIN.equalsIgnoreCase(normalized)) {
            return ROLE_SUPER_ADMIN;
        }
        if (ROLE_CHAT_REVIEWER.equalsIgnoreCase(normalized)) {
            return ROLE_CHAT_REVIEWER;
        }
        return normalized;
    }

    private boolean isManagedRole(String role) {
        if (!StringUtils.hasText(role)) {
            return false;
        }
        String normalized = normalizeRole(role);
        return ROLE_ADMIN.equals(normalized)
                || ROLE_SUPER_ADMIN.equals(normalized)
                || ROLE_CHAT_REVIEWER.equals(normalized)
                || normalized.startsWith(ROLE_MENU_PREFIX);
    }

    @Override
    public WhiteListUser addValidated(WhiteListUser request) {
        if (request != null) {
            request.setId(null);
            if (request.getMenu() != null) {
                request.setMenu(normalizeAndValidateMenu(request.getMenu()));
            }
        }
        return addWhiteListUser(request);
    }

    @Override
    public WhiteListUser updateValidated(WhiteListUser request) {
        if (request != null && request.getMenu() != null) {
            request.setMenu(normalizeAndValidateMenu(request.getMenu()));
        }
        return updateWhiteListUser(request);
    }

    @Override
    public WhiteListUser getByIdWithMenuNames(Long id) {
        WhiteListUser user = getWhiteListUserById(id);
        if (user == null) {
            return null;
        }
        fillMenuNames(Collections.singletonList(user));
        return user;
    }

    @Override
    public List<Long> parseIds(String ids) {
        if (ids == null || ids.trim().isEmpty()) {
            throw new IllegalArgumentException("ids 不能为空");
        }
        List<Long> result = new ArrayList<>();
        for (String part : ids.split(",")) {
            String trimmed = part == null ? null : part.trim();
            if (trimmed == null || trimmed.isEmpty()) {
                continue;
            }
            try {
                result.add(Long.parseLong(trimmed));
            } catch (NumberFormatException e) {
                throw new IllegalArgumentException("ids 格式错误: " + trimmed);
            }
        }
        if (result.isEmpty()) {
            throw new IllegalArgumentException("ids 不能为空");
        }
        return result;
    }

    private void fillMenuNames(List<WhiteListUser> users) {
        if (users == null || users.isEmpty()) {
            return;
        }

        LinkedHashSet<Integer> allIds = new LinkedHashSet<>();
        for (WhiteListUser user : users) {
            if (user == null || user.getMenu() == null || user.getMenu().trim().isEmpty()) {
                continue;
            }
            for (String part : user.getMenu().split(",")) {
                String token = part == null ? null : part.trim();
                if (token != null && token.matches("^\\d+$")) {
                    try {
                        allIds.add(Integer.parseInt(token));
                    } catch (NumberFormatException ignore) {
                        // 超大数字按旧菜单名称兼容处理。
                    }
                }
            }
        }

        Map<Integer, String> idToName = new HashMap<>();
        if (!allIds.isEmpty()) {
            List<MenuPermission> rows = menuPermissionService.listByIds(new ArrayList<>(allIds));
            for (MenuPermission row : rows) {
                if (row != null && row.getId() != null && row.getMenuName() != null) {
                    idToName.put(row.getId(), row.getMenuName());
                }
            }
        }

        for (WhiteListUser user : users) {
            if (user == null || user.getMenu() == null || user.getMenu().trim().isEmpty()) {
                continue;
            }
            List<String> out = new ArrayList<>();
            for (String part : user.getMenu().split(",")) {
                String token = part == null ? null : part.trim();
                if (token == null || token.isEmpty()) {
                    continue;
                }
                if (token.matches("^\\d+$")) {
                    try {
                        Integer id = Integer.parseInt(token);
                        out.add(idToName.getOrDefault(id, token));
                    } catch (NumberFormatException e) {
                        out.add(token);
                    }
                } else {
                    out.add(token);
                }
            }
            user.setMenu(String.join(",", out));
        }
    }

    private String normalizeAndValidateMenu(String menu) {
        String raw = menu == null ? "" : menu.trim();
        if (raw.isEmpty()) {
            return "";
        }

        List<String> rawTokens = new ArrayList<>();
        List<Integer> idTokens = new ArrayList<>();
        List<String> nameTokens = new ArrayList<>();
        for (String part : raw.split(",")) {
            String token = part == null ? null : part.trim();
            if (token == null || token.isEmpty() || rawTokens.contains(token)) {
                continue;
            }
            rawTokens.add(token);
            if (token.matches("^\\d+$")) {
                try {
                    idTokens.add(Integer.parseInt(token));
                } catch (NumberFormatException e) {
                    nameTokens.add(token);
                }
            } else {
                nameTokens.add(token);
            }
        }

        validateMenuIds(idTokens);
        Map<String, Integer> nameToId = validateMenuNames(nameTokens);

        LinkedHashSet<String> normalizedTokens = new LinkedHashSet<>();
        for (String token : rawTokens) {
            if (token.matches("^\\d+$")) {
                normalizedTokens.add(token);
            } else {
                Integer id = nameToId.get(token);
                if (id != null) {
                    normalizedTokens.add(String.valueOf(id));
                }
            }
        }
        return String.join(",", normalizedTokens);
    }

    private void validateMenuIds(List<Integer> idTokens) {
        if (idTokens.isEmpty()) {
            return;
        }
        HashSet<Integer> exists = new HashSet<>();
        for (MenuPermission row : menuPermissionService.listByIds(idTokens)) {
            if (row != null && row.getId() != null) {
                exists.add(row.getId());
            }
        }
        for (Integer id : idTokens) {
            if (id != null && !exists.contains(id)) {
                throw new IllegalArgumentException("menu 中包含不存在的菜单权限ID: " + id);
            }
        }
    }

    private Map<String, Integer> validateMenuNames(List<String> nameTokens) {
        Map<String, Integer> nameToId = new HashMap<>();
        if (nameTokens.isEmpty()) {
            return nameToId;
        }
        HashSet<String> exists = new HashSet<>();
        for (MenuPermission row : menuPermissionService.listByMenuNames(nameTokens)) {
            if (row != null && row.getMenuName() != null) {
                exists.add(row.getMenuName());
                nameToId.put(row.getMenuName(), row.getId());
            }
        }
        for (String name : nameTokens) {
            if (name != null && !exists.contains(name)) {
                throw new IllegalArgumentException("menu 中包含不存在的菜单名称: " + name);
            }
        }
        return nameToId;
    }
}
