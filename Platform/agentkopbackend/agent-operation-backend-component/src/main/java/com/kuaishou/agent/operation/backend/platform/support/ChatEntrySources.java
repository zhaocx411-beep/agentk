package com.kuaishou.agent.operation.backend.platform.support;

import org.springframework.util.StringUtils;

import com.kuaishou.agent.operation.backend.platform.agent100.Agent100Constants;

/**
 * {@code entry_source}：区分在线对话、离线跑数，以及少量平台专用入口标签。
 * <p>
 * 权威值在 {@code aop_chat_session.entry_source}；{@code aop_session_event.entry_source} 是它的冗余副本，
 * 目的是让「只查离线事件」「按入口分批删事件」不必回表 join 会话。
 * <p>
 * 已知专用入口（落库时保留，不折叠为 {@link #ONLINE}）：
 * <ul>
 *   <li>{@link OfficialDemoAssets#ENTRY_SOURCE_CAPABILITY_LAB}</li>
 *   <li>{@link Agent100Constants#ENTRY_SOURCE}</li>
 * </ul>
 * 其余未知标签（如历史 {@code official_quick}）一律归一为 {@link #ONLINE}。
 */
public final class ChatEntrySources {

    public static final String ONLINE = "online";

    /** 离线批量跑数登记的会话，见 {@code OfflineArtifactRecorder}。 */
    public static final String OFFLINE_BATCH = "offline_batch";

    private ChatEntrySources() {
    }

    /**
     * 事件 / 会话的 {@code entry_source}：显式入参优先，其次跟随会话登记的入口。
     * 离线与已知专用入口原样保留；其余（空、乱值）一律 {@link #ONLINE}。
     */
    public static String resolve(String explicit, String sessionEntrySource) {
        if (isOfflineBatch(explicit)) {
            return OFFLINE_BATCH;
        }
        if (isSpecialty(explicit)) {
            return explicit.trim();
        }
        if (!StringUtils.hasText(explicit) && isOfflineBatch(sessionEntrySource)) {
            return OFFLINE_BATCH;
        }
        if (!StringUtils.hasText(explicit) && isSpecialty(sessionEntrySource)) {
            return sessionEntrySource.trim();
        }
        return ONLINE;
    }

    public static boolean isOfflineBatch(String value) {
        return StringUtils.hasText(value) && OFFLINE_BATCH.equals(value.trim());
    }

    /** Capability Lab / Agent100 等平台专用入口，需与普通 online 会话区分。 */
    public static boolean isSpecialty(String value) {
        if (!StringUtils.hasText(value)) {
            return false;
        }
        String trimmed = value.trim();
        return OfficialDemoAssets.ENTRY_SOURCE_CAPABILITY_LAB.equals(trimmed)
                || Agent100Constants.ENTRY_SOURCE.equals(trimmed);
    }
}
