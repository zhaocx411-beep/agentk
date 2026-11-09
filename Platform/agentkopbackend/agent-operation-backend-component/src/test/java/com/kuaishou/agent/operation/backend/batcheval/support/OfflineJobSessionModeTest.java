package com.kuaishou.agent.operation.backend.batcheval.support;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class OfflineJobSessionModeTest {

    @Test
    void fromSubmitShouldPreferSessionModeOverLegacyBoolean() {
        assertThat(OfflineJobSessionMode.fromSubmit(null, null))
                .isEqualTo(OfflineJobSessionMode.PER_TASK);
        assertThat(OfflineJobSessionMode.fromSubmit(null, false))
                .isEqualTo(OfflineJobSessionMode.PER_TASK);
        assertThat(OfflineJobSessionMode.fromSubmit(null, true))
                .isEqualTo(OfflineJobSessionMode.SINGLE_SESSION);
        assertThat(OfflineJobSessionMode.fromSubmit("GROUPED", true))
                .isEqualTo(OfflineJobSessionMode.GROUPED);
        assertThat(OfflineJobSessionMode.fromSubmit("grouped", null))
                .isEqualTo(OfflineJobSessionMode.GROUPED);
    }

    @Test
    void shouldRejectUnknownSessionMode() {
        assertThatThrownBy(() -> OfflineJobSessionMode.parse("parallel"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("sessionMode");
    }

    @Test
    void viewsShouldExposeCodeLabelAndDescription() {
        assertThat(OfflineJobSessionMode.views()).extracting(OfflineJobSessionMode.View::getCode)
                .containsExactly("PER_TASK", "SINGLE_SESSION", "GROUPED");
        assertThat(OfflineJobSessionMode.PER_TASK.getLabel()).isEqualTo("单轮");
        assertThat(OfflineJobSessionMode.PER_TASK.getDescription())
                .contains("独立 session").contains("可并发");
        assertThat(OfflineJobSessionMode.SINGLE_SESSION.getDescription())
                .contains("整表共用一个 session");
        assertThat(OfflineJobSessionMode.GROUPED.getDescription())
                .contains("conversationKey");
    }
}
