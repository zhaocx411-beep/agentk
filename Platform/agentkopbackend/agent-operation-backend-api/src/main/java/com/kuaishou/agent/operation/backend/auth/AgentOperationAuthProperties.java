package com.kuaishou.agent.operation.backend.auth;

import java.util.ArrayList;
import java.util.List;

import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * 后台 API 鉴权配置。
 */
@ConfigurationProperties(prefix = "agent.operation.backend.auth")
public class AgentOperationAuthProperties {

    private boolean enabled = false;

    /**
     * LoginInterceptor 匹配全局前缀后需放行的路径（Ant 风格）。
     */
    private List<String> interceptorExcludePathPatterns = new ArrayList<>();

    /**
     * true 时跳过业务权限判定，仅建议本地/压测使用。
     * 生产管理员请写入表 {@code aop_platform_admin}。
     */
    private boolean permissionStub = false;

    public boolean isEnabled() {
        return enabled;
    }

    public void setEnabled(boolean enabled) {
        this.enabled = enabled;
    }

    public List<String> getInterceptorExcludePathPatterns() {
        return interceptorExcludePathPatterns;
    }

    public void setInterceptorExcludePathPatterns(List<String> interceptorExcludePathPatterns) {
        this.interceptorExcludePathPatterns = interceptorExcludePathPatterns;
    }

    public boolean isPermissionStub() {
        return permissionStub;
    }

    public void setPermissionStub(boolean permissionStub) {
        this.permissionStub = permissionStub;
    }
}
