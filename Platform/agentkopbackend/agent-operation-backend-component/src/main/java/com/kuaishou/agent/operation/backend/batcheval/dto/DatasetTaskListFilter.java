package com.kuaishou.agent.operation.backend.batcheval.dto;

import lombok.Data;

/**
 * 用例列表筛选：datasetId / usage / pipelineName 至少一项。
 */
@Data
public class DatasetTaskListFilter {

    /** 指定数据集；为空表示跨数据集按 usage / pipelineName 筛 */
    private Long datasetId;
    /** EVAL / TEMPLATE */
    private String usage;
    /** 模版归属 pipeline */
    private String pipelineName;
    /** 素材主类目 code；为空表示不按类目筛 */
    private String category;

    public boolean hasDatasetId() {
        return datasetId != null && datasetId > 0;
    }

    public boolean hasUsage() {
        return hasText(usage);
    }

    public boolean hasPipelineName() {
        return hasText(pipelineName);
    }

    public boolean hasCategory() {
        return hasText(category);
    }

    private static boolean hasText(String value) {
        return value != null && !value.trim().isEmpty();
    }
}
