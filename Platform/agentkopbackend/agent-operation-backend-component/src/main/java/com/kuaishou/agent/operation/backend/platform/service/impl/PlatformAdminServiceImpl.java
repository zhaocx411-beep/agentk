package com.kuaishou.agent.operation.backend.platform.service.impl;

import java.time.LocalDateTime;
import java.util.Collections;
import java.util.List;
import java.util.stream.Collectors;

import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.PlatformAdminMapper;
import com.kuaishou.agent.operation.backend.platform.entity.PlatformAdmin;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.agent.operation.backend.platform.support.PlatformAccessException;

/**
 * 管理员授权以 DB 为准（KConf 目录暂不可用时的方案）。
 */
@Service
public class PlatformAdminServiceImpl implements PlatformAdminService {

    public static final String ROLE_CHAT_REVIEWER = "chat_reviewer";
    public static final String ROLE_ADMIN = "ADMIN";
    public static final String ROLE_SUPER_ADMIN = "SUPER_ADMIN";

    private final PlatformAdminMapper platformAdminMapper;

    public PlatformAdminServiceImpl(PlatformAdminMapper platformAdminMapper) {
        this.platformAdminMapper = platformAdminMapper;
    }

    @Override
    public boolean isGlobalAdmin(String userId) {
        if (!StringUtils.hasText(userId)) {
            return false;
        }
        Integer count = platformAdminMapper.selectCount(new LambdaQueryWrapper<PlatformAdmin>()
                .eq(PlatformAdmin::getUserId, userId)
                .eq(PlatformAdmin::getStatus, "active")
                .in(PlatformAdmin::getRole, ROLE_CHAT_REVIEWER, ROLE_SUPER_ADMIN));
        return count != null && count > 0;
    }

    @Override
    public boolean isPlatformAdmin(String userId) {
        if (!StringUtils.hasText(userId)) {
            return false;
        }
        Integer count = platformAdminMapper.selectCount(new LambdaQueryWrapper<PlatformAdmin>()
                .eq(PlatformAdmin::getUserId, userId)
                .eq(PlatformAdmin::getStatus, "active")
                .in(PlatformAdmin::getRole, ROLE_ADMIN, ROLE_SUPER_ADMIN));
        return count != null && count > 0;
    }

    @Override
    public boolean isSuperAdmin(String userId) {
        if (!StringUtils.hasText(userId)) {
            return false;
        }
        Integer count = platformAdminMapper.selectCount(new LambdaQueryWrapper<PlatformAdmin>()
                .eq(PlatformAdmin::getUserId, userId)
                .eq(PlatformAdmin::getStatus, "active")
                .eq(PlatformAdmin::getRole, ROLE_SUPER_ADMIN));
        return count != null && count > 0;
    }

    @Override
    public long countActiveAdmins() {
        Integer count = platformAdminMapper.selectCount(new LambdaQueryWrapper<PlatformAdmin>()
                .ne(PlatformAdmin::getStatus, "disabled"));
        return count == null ? 0L : count.longValue();
    }

    @Override
    public List<PlatformAdmin> listDbAdmins() {
        return platformAdminMapper.selectList(new LambdaQueryWrapper<PlatformAdmin>()
                .ne(PlatformAdmin::getStatus, "disabled")
                .orderByDesc(PlatformAdmin::getId));
    }

    @Override
    public List<String> listActiveRoles(String userId) {
        if (!StringUtils.hasText(userId)) {
            return Collections.emptyList();
        }
        return platformAdminMapper.selectList(new LambdaQueryWrapper<PlatformAdmin>()
                        .eq(PlatformAdmin::getUserId, userId.trim())
                        .eq(PlatformAdmin::getStatus, "active")
                        .orderByAsc(PlatformAdmin::getId))
                .stream()
                .map(PlatformAdmin::getRole)
                .filter(StringUtils::hasText)
                .distinct()
                .collect(Collectors.toList());
    }

    @Override
    public PlatformAdmin grant(String userId, String role, String operatorUserId) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("userId required");
        }
        String r = StringUtils.hasText(role) ? role : ROLE_CHAT_REVIEWER;
        PlatformAdmin existing = platformAdminMapper.selectOne(new LambdaQueryWrapper<PlatformAdmin>()
                .eq(PlatformAdmin::getUserId, userId.trim())
                .eq(PlatformAdmin::getRole, r)
                .last("LIMIT 1"));
        LocalDateTime now = LocalDateTime.now();
        if (existing != null) {
            existing.setStatus("active");
            existing.setUpdatedAt(now);
            platformAdminMapper.updateById(existing);
            return existing;
        }
        PlatformAdmin admin = new PlatformAdmin();
        admin.setUserId(userId.trim());
        admin.setRole(r);
        admin.setStatus("active");
        admin.setCreatedBy(operatorUserId == null ? "" : operatorUserId);
        admin.setCreatedAt(now);
        admin.setUpdatedAt(now);
        platformAdminMapper.insert(admin);
        return admin;
    }

    @Override
    public void revoke(String userId, String role, String operatorUserId) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("userId required");
        }
        String r = StringUtils.hasText(role) ? role : ROLE_CHAT_REVIEWER;
        PlatformAdmin existing = platformAdminMapper.selectOne(new LambdaQueryWrapper<PlatformAdmin>()
                .eq(PlatformAdmin::getUserId, userId.trim())
                .eq(PlatformAdmin::getRole, r)
                .last("LIMIT 1"));
        if (existing == null) {
            return;
        }
        existing.setStatus("disabled");
        existing.setUpdatedAt(LocalDateTime.now());
        platformAdminMapper.updateById(existing);
    }

    @Override
    public void assertAdminOrBootstrap(String userId, boolean permissionStub) {
        if (permissionStub) {
            return;
        }
        if (isGlobalAdmin(userId)) {
            return;
        }
        if (countActiveAdmins() == 0) {
            return;
        }
        throw new PlatformAccessException(
                "global admin required; ask an existing admin or insert into aop_platform_admin");
    }

    @Override
    public void assertGlobalAdmin(String userId, boolean permissionStub) {
        if (permissionStub) {
            return;
        }
        if (!isGlobalAdmin(userId)) {
            throw new PlatformAccessException("global admin required");
        }
    }

    @Override
    public void assertPlatformAdmin(String userId, boolean permissionStub) {
        if (permissionStub) {
            return;
        }
        if (!isPlatformAdmin(userId)) {
            throw new PlatformAccessException("platform admin required");
        }
    }
}
