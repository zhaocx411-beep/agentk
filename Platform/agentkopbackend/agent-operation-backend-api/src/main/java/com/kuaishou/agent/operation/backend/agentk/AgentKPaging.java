package com.kuaishou.agent.operation.backend.agentk;

import org.springframework.util.StringUtils;

/**
 * 与 agent-k 列表分页约定对齐：游标/页大小缺省值、上限收敛、Query 拼装。
 */
public final class AgentKPaging {

    /** Session 列表：缺省页大小 */
    public static final int SESSIONS_DEFAULT_LIMIT = 20;
    /** Session 列表：页大小上限（超限收敛，不报错） */
    public static final int SESSIONS_MAX_LIMIT = 100;

    /** Events 列表：缺省页大小 */
    public static final int EVENTS_DEFAULT_LIMIT = 500;
    /** Events 列表：页大小上限 */
    public static final int EVENTS_MAX_LIMIT = 2000;

    /** Items 列表：缺省页大小（上游已删 items，仅注释代码可能引用） */
    public static final int ITEMS_DEFAULT_LIMIT = 200;
    /** Items 列表：页大小上限 */
    public static final int ITEMS_MAX_LIMIT = 1000;

    /** Events / SSE：缺省游标（取 seq &gt; 0 的全部增量） */
    public static final long AFTER_DEFAULT = 0L;

    /** Jobs：缺省只看未终态（上游已删 jobs，仅注释代码可能引用） */
    public static final String JOBS_STATUS_DEFAULT = "pending";
    public static final String JOBS_STATUS_ALL = "all";

    private AgentKPaging() {
    }

    /**
     * 解析页大小：空 → 缺省；非法（≤0）→ null 表示调用方应回 400；超上限 → 收敛。
     */
    public static Integer resolveLimit(Integer limit, int defaultLimit, int maxLimit) {
        if (limit == null) {
            return defaultLimit;
        }
        if (limit <= 0) {
            return null;
        }
        return Math.min(limit, maxLimit);
    }

    /** 解析 after 游标：空 → 0；负数非法 → null。 */
    public static Long resolveAfter(Long after) {
        if (after == null) {
            return AFTER_DEFAULT;
        }
        if (after < 0) {
            return null;
        }
        return after;
    }

    /** {@code GET /sessions?limit=&before=} */
    public static String sessionsQuery(Integer limit, String before) {
        Integer resolved = resolveLimit(limit, SESSIONS_DEFAULT_LIMIT, SESSIONS_MAX_LIMIT);
        if (resolved == null) {
            return null;
        }
        StringBuilder q = new StringBuilder("limit=").append(resolved);
        if (StringUtils.hasText(before)) {
            q.append("&before=").append(before.trim());
        }
        return q.toString();
    }

    /**
     * {@code GET /sessions/{id}/events?after=&limit=}
     */
    public static String eventsQuery(Long after, Integer limit) {
        Long resolvedAfter = resolveAfter(after);
        Integer resolvedLimit = resolveLimit(limit, EVENTS_DEFAULT_LIMIT, EVENTS_MAX_LIMIT);
        if (resolvedAfter == null || resolvedLimit == null) {
            return null;
        }
        return "after=" + resolvedAfter + "&limit=" + resolvedLimit;
    }

    /**
     * 平台内部完整同步事件账本；不对 Controller 的分页接口暴露。
     * 离线 {@code AgentkClient.ALL_EVENTS_QUERY} 必须与此字符串保持一致。
     */
    public static String allEventsQuery() {
        return eventsQuery(0L, EVENTS_MAX_LIMIT) + "&all=true";
    }

    /**
     * 旧 Run 事件账本：{@code GET /runs/{id}/events?after=}（无 limit）。
     * agent-k 已删除该路径；保留供注释代码引用。
     *
     * @deprecated 改用 {@link #eventsQuery(Long, Integer)}
     */
    @Deprecated
    public static String eventsQuery(Long after) {
        Long resolvedAfter = resolveAfter(after);
        if (resolvedAfter == null) {
            return null;
        }
        return "after=" + resolvedAfter;
    }

    /** @deprecated agent-k 已删除 items；保留供注释代码引用。 */
    @Deprecated
    public static String itemsQuery(Long after, Integer limit) {
        Long resolvedAfter = resolveAfter(after);
        Integer resolvedLimit = resolveLimit(limit, ITEMS_DEFAULT_LIMIT, ITEMS_MAX_LIMIT);
        if (resolvedAfter == null || resolvedLimit == null) {
            return null;
        }
        return "after=" + resolvedAfter + "&limit=" + resolvedLimit;
    }

    /** @deprecated agent-k 已删除 jobs；保留供注释代码引用。 */
    @Deprecated
    public static String jobsQuery(String status) {
        String resolved = StringUtils.hasText(status) ? status.trim() : JOBS_STATUS_DEFAULT;
        if (!JOBS_STATUS_DEFAULT.equals(resolved) && !JOBS_STATUS_ALL.equals(resolved)) {
            return null;
        }
        return "status=" + resolved;
    }
}
