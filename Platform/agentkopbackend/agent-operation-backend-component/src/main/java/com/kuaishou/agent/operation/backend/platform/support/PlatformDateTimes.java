package com.kuaishou.agent.operation.backend.platform.support;

import java.time.Instant;
import java.time.LocalDateTime;
import java.time.OffsetDateTime;
import java.time.ZoneId;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;

/**
 * 平台时间：落库/展示一律按东八区墙钟，与 Jackson {@code time-zone: GMT+8}、
 * JDBC {@code serverTimezone=Asia/Shanghai} 对齐。
 */
public final class PlatformDateTimes {

    public static final ZoneId ZONE = ZoneId.of("Asia/Shanghai");

    private static final DateTimeFormatter DISPLAY = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss");

    private PlatformDateTimes() {
    }

    public static String nowOffsetIso() {
        return OffsetDateTime.now(ZONE).toString();
    }

    /**
     * 解析上游 ISO 时间。带偏移/Z 的按同一瞬间转到东八区；无时区则视为已经是东八区墙钟。
     */
    public static LocalDateTime parse(String raw) {
        if (raw == null || raw.trim().isEmpty()) {
            return null;
        }
        String value = raw.trim();
        try {
            return OffsetDateTime.parse(value).atZoneSameInstant(ZONE).toLocalDateTime();
        } catch (Exception ignored) {
            // fall through
        }
        try {
            return Instant.parse(value).atZone(ZONE).toLocalDateTime();
        } catch (Exception ignored) {
            // fall through
        }
        try {
            return LocalDateTime.parse(value);
        } catch (Exception ignored) {
            return null;
        }
    }

    public static String format(LocalDateTime time) {
        return time == null ? null : DISPLAY.format(time);
    }

    /**
     * 旧数据把 UTC Instant 的墙钟直接当成 {@link LocalDateTime} 写入，相对东八区锚点会早约 8 小时。
     * 读接口按同一瞬间补回上海时间；新写入已经是本地墙钟则原样返回。
     */
    public static LocalDateTime assumeUtcWallIfBefore(LocalDateTime stored, LocalDateTime localAnchor) {
        if (stored == null) {
            return null;
        }
        if (localAnchor != null && stored.isBefore(localAnchor.minusMinutes(1))) {
            return stored.atOffset(ZoneOffset.UTC).atZoneSameInstant(ZONE).toLocalDateTime();
        }
        return stored;
    }
}
