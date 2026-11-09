package com.kuaishou.agent.operation.backend.platform.support;

import org.springframework.util.StringUtils;

/**
 * 会话标题：建会话占位、首条用户消息派生、手动改名。
 */
public final class ChatSessionTitles {

    /** 未传 title 建会话时的占位标题；首条用户消息成功发送后可被派生标题替换。 */
    public static final String DEFAULT_PLACEHOLDER = "新对话";

    /** 从首条用户输入派生标题时的最大字符数（超出则截断并加省略号）。 */
    public static final int DERIVED_MAX_LEN = 10;

    private static final String ELLIPSIS = "…";

    private ChatSessionTitles() {
    }

    public static boolean isDefaultPlaceholder(String title) {
        return DEFAULT_PLACEHOLDER.equals(normalizeStored(title));
    }

    /**
     * 从用户输入派生标题：trim + 折叠空白；不足 {@link #DERIVED_MAX_LEN} 用全文；
     * 超出则前 {@link #DERIVED_MAX_LEN} 字 + {@code …}；空白则保持占位标题。
     */
    public static String deriveFromUserInput(String userInput) {
        String normalized = normalizeUserInput(userInput);
        if (!StringUtils.hasText(normalized)) {
            return DEFAULT_PLACEHOLDER;
        }
        if (normalized.length() <= DERIVED_MAX_LEN) {
            return normalized;
        }
        return normalized.substring(0, DERIVED_MAX_LEN) + ELLIPSIS;
    }

    /** 建会话标题：显式 title 优先；否则用首问 text 派生；都没有则「新对话」。 */
    public static String resolveCreateTitle(String title) {
        return resolveCreateTitle(title, null);
    }

    public static String resolveCreateTitle(String title, String firstQuestion) {
        if (StringUtils.hasText(title)) {
            return title.trim();
        }
        if (StringUtils.hasText(firstQuestion)) {
            return deriveFromUserInput(firstQuestion);
        }
        return DEFAULT_PLACEHOLDER;
    }

    /** 手动改名：trim 后非空，最长 120 字。 */
    public static String resolveManualTitle(String title) {
        if (!StringUtils.hasText(title)) {
            throw new IllegalArgumentException("title 不能为空");
        }
        String trimmed = title.trim();
        if (trimmed.length() > 120) {
            throw new IllegalArgumentException("title 不能超过 120 个字符");
        }
        return trimmed;
    }

    private static String normalizeStored(String title) {
        return title == null ? "" : title.trim();
    }

    private static String normalizeUserInput(String text) {
        if (text == null) {
            return "";
        }
        return text.replaceAll("\\s+", " ").trim();
    }
}
