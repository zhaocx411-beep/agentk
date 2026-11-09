package com.kuaishou.agent.operation.backend.controller;

import java.util.HashMap;
import java.util.Map;

import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.kuaishou.agent.operation.backend.agentk.AgentKIdentityResolver;
import com.kuaishou.agent.operation.backend.auth.AgentOperationAuthProperties;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.platform.dto.GrantAdminRequest;
import com.kuaishou.agent.operation.backend.platform.entity.PlatformAdmin;
import com.kuaishou.agent.operation.backend.platform.service.PlatformAdminService;
import com.kuaishou.agent.operation.backend.platform.service.impl.PlatformAdminServiceImpl;

/**
 * 平台管理员 DB 授权（当前以 {@code aop_platform_admin} 为准）。
 * <p>
 * 表为空时，任意已登录用户可完成一次「首个管理员」引导授权；之后仅管理员可增删。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/admin/users")
public class AdminUserController {

    private final PlatformAdminService platformAdminService;
    private final AgentKIdentityResolver identityResolver;
    private final AgentOperationAuthProperties authProperties;

    public AdminUserController(PlatformAdminService platformAdminService,
            AgentKIdentityResolver identityResolver,
            AgentOperationAuthProperties authProperties) {
        this.platformAdminService = platformAdminService;
        this.identityResolver = identityResolver;
        this.authProperties = authProperties;
    }

    @GetMapping
    public Message<Map<String, Object>> listAdmins() {
        String userId = identityResolver.resolve().getUserId();
        platformAdminService.assertAdminOrBootstrap(userId, authProperties.isPermissionStub());
        Map<String, Object> data = new HashMap<>();
        data.put("source", "db");
        data.put("table", "aop_platform_admin");
        data.put("activeCount", platformAdminService.countActiveAdmins());
        data.put("dbAdmins", platformAdminService.listDbAdmins());
        return Message.ok(data);
    }

    /**
     * DB 授权。表为空时允许首个登录用户把自己或他人写成管理员（bootstrap）。
     */
    @PostMapping
    public Message<PlatformAdmin> grant(@RequestBody GrantAdminRequest request) {
        String operator = identityResolver.resolve().getUserId();
        platformAdminService.assertAdminOrBootstrap(operator, authProperties.isPermissionStub());
        PlatformAdmin admin = platformAdminService.grant(
                request == null ? null : request.getUserId(),
                request == null ? null : request.getRole(),
                operator);
        return Message.ok(admin);
    }

    @DeleteMapping
    public Message<Void> revoke(@RequestParam("userId") String userId,
            @RequestParam(value = "role", required = false) String role) {
        String operator = identityResolver.resolve().getUserId();
        platformAdminService.assertGlobalAdmin(operator, authProperties.isPermissionStub());
        platformAdminService.revoke(userId,
                role == null ? PlatformAdminServiceImpl.ROLE_CHAT_REVIEWER : role,
                operator);
        return Message.ok(null);
    }
}
