package com.kuaishou.agent.operation.backend.batcheval.dto;

import lombok.Data;

/**
 * 分页结果
 */
@Data
public class PagedResult<T> {

    private T records;
    private long total;
    /** 当前页码，从 1 开始 */
    private int current;
    private int pageSize;
    private long pages;

    public static <T> PagedResult<T> of(T records, long total, int current, int pageSize) {
        PagedResult<T> result = new PagedResult<>();
        result.setRecords(records);
        result.setTotal(total);
        result.setCurrent(current);
        result.setPageSize(pageSize);
        result.setPages(pageSize <= 0 ? 0 : (total - 1) / pageSize + 1);
        return result;
    }
}
