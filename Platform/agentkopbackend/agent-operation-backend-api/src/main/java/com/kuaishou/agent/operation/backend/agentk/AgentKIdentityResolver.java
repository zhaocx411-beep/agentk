package com.kuaishou.agent.operation.backend.agentk;

import org.springframework.stereotype.Component;

import com.kuaishou.agent.operation.backend.common.interceptor.LoginInterceptor;
import com.kuaishou.security.accessproxy.v1.model.ApPrincipal;

/**
 * 在同步阶段解析 BFF 操作者身份（日志 userId / 无 API Key 时的 SSO 头），
 * 避免 SSE 异步写流时再读 ThreadLocal。
 * <p>
 * userId 与 {@code UserInfoController} 的 username 一致：取自 {@code ApPrincipal#getUsername()}。
 */
@Component
public class AgentKIdentityResolver {

    public AgentKOutboundIdentity resolve() {
        ApPrincipal principal = LoginInterceptor.getThreadLocal().get();
        if (principal == null) {
            return fallbackIdentity();
        }
        // 与 UserInfoController.toUserInfoResponse 对齐：username 优先，缺省回退 admin
        String username = principal.getUsername() != null ? principal.getUsername() : "admin";
        String name = principal.getName() != null ? principal.getName() : username;
        return new AgentKOutboundIdentity(username, username, name);
    }

    private static AgentKOutboundIdentity fallbackIdentity() {
        // 与 UserInfoController 无登录态一致
        return new AgentKOutboundIdentity("admin", "admin", "admin");
    }
}
