package com.kuaishou.agent.operation.backend.batcheval.support;

/**
 * 列表分页参数归一化
 */
public final class PageRequests {

    public static final int DEFAULT_CURRENT = 1;
    public static final int DEFAULT_PAGE_SIZE = 10;
    public static final int MAX_PAGE_SIZE = 100;

    private PageRequests() {
    }

    public static int normalizeCurrent(int current) {
        return current < 1 ? DEFAULT_CURRENT : current;
    }

    public static int normalizePageSize(int pageSize) {
        if (pageSize < 1) {
            return DEFAULT_PAGE_SIZE;
        }
        return Math.min(pageSize, MAX_PAGE_SIZE);
    }

    public static int offset(int current, int pageSize) {
        return (normalizeCurrent(current) - 1) * normalizePageSize(pageSize);
    }
}
