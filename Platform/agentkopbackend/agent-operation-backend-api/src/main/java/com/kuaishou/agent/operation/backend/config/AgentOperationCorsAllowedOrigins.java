package com.kuaishou.agent.operation.backend.config;

import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * Agent 运维后台允许跨域的来源，必须与浏览器 Origin 完全一致。
 */
public final class AgentOperationCorsAllowedOrigins {

    public static final List<String> ALL =
            Collections.unmodifiableList(Arrays.asList(
                    "https://keling-operation-platform.corp.kuaishou.com",
                    "https://keling-operation-platform-test.staging.kuaishou.com",
                    "http://localhost:3000"));

    private AgentOperationCorsAllowedOrigins() {
    }

    public static boolean isAllowed(String rawOrigin) {
        if (rawOrigin == null) {
            return false;
        }
        String trimmed = rawOrigin.trim();
        return !trimmed.isEmpty() && ALL.contains(trimmed);
    }
}
