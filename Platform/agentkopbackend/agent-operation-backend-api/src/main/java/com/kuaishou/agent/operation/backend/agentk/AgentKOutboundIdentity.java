package com.kuaishou.agent.operation.backend.agentk;

/**
 * BFF 侧操作者身份：访问日志的 userId。
 * {@code userId} 与 {@link com.kuaishou.agent.operation.backend.controller.UserInfoController}
 * 的 username 同源（登录态 {@code ApPrincipal#getUsername()}）。
 */
public class AgentKOutboundIdentity {

    private final String userId;
    private final String username;
    private final String displayName;

    public AgentKOutboundIdentity(String userId, String username, String displayName) {
        this.userId = userId;
        this.username = username;
        this.displayName = displayName;
    }

    public String getUserId() {
        return userId;
    }

    public String getUsername() {
        return username;
    }

    public String getDisplayName() {
        return displayName;
    }
}
