package com.kuaishou.agent.operation.backend.platform.support;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class ChatSessionTitlesTest {

    @Test
    void deriveFromUserInputUsesFullTextWhenWithinLimit() {
        assertThat(ChatSessionTitles.deriveFromUserInput("你好")).isEqualTo("你好");
        assertThat(ChatSessionTitles.deriveFromUserInput("帮我写个脚本")).isEqualTo("帮我写个脚本");
    }

    @Test
    void deriveFromUserInputTruncatesLongTextWithEllipsis() {
        assertThat(ChatSessionTitles.deriveFromUserInput("一二三四五六七八九十十一"))
                .isEqualTo("一二三四五六七八九十…");
    }

    @Test
    void deriveFromUserInputKeepsPlaceholderForBlankInput() {
        assertThat(ChatSessionTitles.deriveFromUserInput("   ")).isEqualTo(ChatSessionTitles.DEFAULT_PLACEHOLDER);
        assertThat(ChatSessionTitles.deriveFromUserInput(null)).isEqualTo(ChatSessionTitles.DEFAULT_PLACEHOLDER);
    }

    @Test
    void resolveCreateTitleFallsBackToPlaceholder() {
        assertThat(ChatSessionTitles.resolveCreateTitle(null)).isEqualTo(ChatSessionTitles.DEFAULT_PLACEHOLDER);
        assertThat(ChatSessionTitles.resolveCreateTitle("  ")).isEqualTo(ChatSessionTitles.DEFAULT_PLACEHOLDER);
        assertThat(ChatSessionTitles.resolveCreateTitle(" 自定义 ")).isEqualTo("自定义");
    }

    @Test
    void resolveCreateTitleUsesFirstQuestionWhenTitleBlank() {
        assertThat(ChatSessionTitles.resolveCreateTitle(null, "帮我做一张 Labubu 海报"))
                .isEqualTo("帮我做一张 Labu…");
        assertThat(ChatSessionTitles.resolveCreateTitle("  ", "你好")).isEqualTo("你好");
        assertThat(ChatSessionTitles.resolveCreateTitle("手动标题", "首问内容")).isEqualTo("手动标题");
    }

    @Test
    void resolveManualTitleRejectsBlankOrTooLong() {
        assertThatThrownBy(() -> ChatSessionTitles.resolveManualTitle(" "))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> ChatSessionTitles.resolveManualTitle("x".repeat(121)))
                .isInstanceOf(IllegalArgumentException.class);
        assertThat(ChatSessionTitles.resolveManualTitle(" 我的标题 ")).isEqualTo("我的标题");
    }
}
