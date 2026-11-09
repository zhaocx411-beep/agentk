package com.kuaishou.agent.operation.backend.platform.support;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.LocalDateTime;

import org.junit.jupiter.api.Test;

class PlatformDateTimesTest {

    @Test
    void parseConvertsUtcInstantToShanghaiWallClock() {
        assertThat(PlatformDateTimes.parse("2026-09-08T06:13:24.058Z"))
                .isEqualTo(LocalDateTime.of(2026, 9, 8, 14, 13, 24, 58_000_000));
    }

    @Test
    void parseKeepsShanghaiOffsetAsLocalWallClock() {
        assertThat(PlatformDateTimes.parse("2026-09-08T14:13:24.058+08:00"))
                .isEqualTo(LocalDateTime.of(2026, 9, 8, 14, 13, 24, 58_000_000));
    }

    @Test
    void parseTreatsNaiveIsoAsAlreadyLocal() {
        assertThat(PlatformDateTimes.parse("2026-09-08T14:13:24.058"))
                .isEqualTo(LocalDateTime.of(2026, 9, 8, 14, 13, 24, 58_000_000));
    }

    @Test
    void assumeUtcWallIfBeforeConvertsLegacyUtcColumns() {
        LocalDateTime created = LocalDateTime.of(2026, 9, 8, 14, 13, 24);
        LocalDateTime finishedUtcWall = LocalDateTime.of(2026, 9, 8, 6, 13, 26);
        assertThat(PlatformDateTimes.assumeUtcWallIfBefore(finishedUtcWall, created))
                .isEqualTo(LocalDateTime.of(2026, 9, 8, 14, 13, 26));
    }

    @Test
    void assumeUtcWallIfBeforeKeepsAlreadyLocalTimes() {
        LocalDateTime created = LocalDateTime.of(2026, 9, 8, 14, 13, 24);
        LocalDateTime finished = LocalDateTime.of(2026, 9, 8, 14, 13, 26);
        assertThat(PlatformDateTimes.assumeUtcWallIfBefore(finished, created)).isEqualTo(finished);
    }

    @Test
    void formatMatchesJacksonDateFormat() {
        assertThat(PlatformDateTimes.format(LocalDateTime.of(2026, 9, 8, 14, 13, 24)))
                .isEqualTo("2026-09-08 14:13:24");
    }
}
