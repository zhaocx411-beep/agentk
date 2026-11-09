package com.kuaishou.agent.operation.backend.platform.dto;

import java.util.Collections;
import java.util.List;

import lombok.Data;

/**
 * 平台侧统一分页壳（配合 MyBatis-Plus Page）。
 */
@Data
public class PageResult<T> {
    private long page;
    private long pageSize;
    private long total;
    private List<T> records = Collections.emptyList();

    public static <T> PageResult<T> of(long page, long pageSize, long total, List<T> records) {
        PageResult<T> result = new PageResult<>();
        result.page = page;
        result.pageSize = pageSize;
        result.total = total;
        result.records = records == null ? Collections.emptyList() : records;
        return result;
    }

    public static <T> PageResult<T> empty() {
        return empty(1L, 20L);
    }

    public static <T> PageResult<T> empty(long page, long pageSize) {
        return of(page <= 0 ? 1L : page, pageSize <= 0 ? 20L : pageSize, 0L, Collections.emptyList());
    }
}
