package com.kuaishou.agent.operation.backend.batcheval.support;

import java.util.ArrayList;
import java.util.List;

/**
 * 离线跑数提交时的会话策略。同一份数据集可分别按三种方式跑，不必复制数据。
 */
public enum OfflineJobSessionMode {

    PER_TASK("单轮", "每条用例独立 session，行与行可并发。"),
    SINGLE_SESSION("整表多轮", "整表共用一个 session，按 turnIndex（无则行序）串行；忽略 conversationKey。"),
    GROUPED("分组多轮", "按 conversationKey 分桶；空键各自独立可并发，同键按轮次串行共用 session。");

    private final String label;
    private final String description;

    OfflineJobSessionMode(String label, String description) {
        this.label = label;
        this.description = description;
    }

    /** 落库 / 接口取值，与 {@link #name()} 相同。 */
    public String getCode() {
        return name();
    }

    public String getLabel() {
        return label;
    }

    public String getDescription() {
        return description;
    }

    public boolean isPerTask() {
        return this == PER_TASK;
    }

    public boolean isSingleSession() {
        return this == SINGLE_SESSION;
    }

    public boolean isGrouped() {
        return this == GROUPED;
    }

    /**
     * 未传时默认 {@link #PER_TASK}。只认枚举名（大小写不敏感）。
     */
    public static OfflineJobSessionMode parse(String sessionMode) {
        if (sessionMode == null || sessionMode.trim().isEmpty()) {
            return PER_TASK;
        }
        String value = sessionMode.trim();
        for (OfflineJobSessionMode mode : values()) {
            if (mode.name().equalsIgnoreCase(value)) {
                return mode;
            }
        }
        throw new IllegalArgumentException(
                "sessionMode 仅支持 PER_TASK / SINGLE_SESSION / GROUPED，当前值: " + sessionMode);
    }

    /**
     * 提交入参：优先 {@code sessionMode}；未传时兼容旧布尔
     * {@code multiRoundConversation}（true=SINGLE_SESSION，false/缺省=PER_TASK）。
     */
    public static OfflineJobSessionMode fromSubmit(String sessionMode, Boolean multiRoundConversation) {
        if (sessionMode != null && !sessionMode.trim().isEmpty()) {
            return parse(sessionMode);
        }
        if (Boolean.TRUE.equals(multiRoundConversation)) {
            return SINGLE_SESSION;
        }
        return PER_TASK;
    }

    public static List<View> views() {
        List<View> views = new ArrayList<>(values().length);
        for (OfflineJobSessionMode mode : values()) {
            views.add(mode.toView());
        }
        return views;
    }

    public View toView() {
        return new View(getCode(), label, description);
    }

    public static final class View {
        private final String code;
        private final String label;
        private final String description;

        View(String code, String label, String description) {
            this.code = code;
            this.label = label;
            this.description = description;
        }

        public String getCode() {
            return code;
        }

        public String getLabel() {
            return label;
        }

        public String getDescription() {
            return description;
        }
    }
}
