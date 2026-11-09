package com.kuaishou.agent.operation.backend.platform.support;

import org.springframework.util.StringUtils;

/**
 * {@code aop_chat_asset.source}：产物业务来源（与 {@code source_kind} 正交）。
 * <ul>
 *   <li>{@code source_kind}：技术产出路径 agent_loop / pipeline / capability / api</li>
 *   <li>{@code source}：在线问答 vs 离线批量跑数（离线以 {@code batcheval_offline_job_cell} 为准）</li>
 * </ul>
 */
public final class ChatAssetSources {

    public static final String ONLINE = "online";
    public static final String OFFLINE = "offline";

    private ChatAssetSources() {
    }

    public static String normalize(String source) {
        if (!StringUtils.hasText(source)) {
            return ONLINE;
        }
        String normalized = source.trim().toLowerCase();
        return OFFLINE.equals(normalized) ? OFFLINE : ONLINE;
    }
}
