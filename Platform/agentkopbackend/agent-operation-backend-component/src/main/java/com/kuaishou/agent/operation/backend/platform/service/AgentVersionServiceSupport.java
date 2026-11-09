package com.kuaishou.agent.operation.backend.platform.service;

import java.util.regex.Matcher;
import java.util.regex.Pattern;

import org.springframework.util.StringUtils;

/**
 * AgentVersionService 静态工具（保持接口可被 Controller 直接调用）。
 */
final class AgentVersionServiceSupport {

    /** 历史 YAML version: 行；新 JSON profile 通常无此字段，回落 contentHash。 */
    private static final Pattern VERSION_LINE = Pattern.compile("(?m)^version:\\s*[\"']?([^\\s\"'#]+)");

    private AgentVersionServiceSupport() {
    }

    /**
     * 提取版本标签：优先历史 YAML {@code version:} 行，否则 contentHash 前 12 位。
     */
    static String extractProfileVersionLabel(String profileJson, String contentHash) {
        if (StringUtils.hasText(profileJson)) {
            Matcher m = VERSION_LINE.matcher(profileJson);
            if (m.find()) {
                return m.group(1).trim();
            }
        }
        String hash = contentHash == null ? "" : contentHash;
        return hash.length() >= 12 ? hash.substring(0, 12) : hash;
    }
}
