package com.kuaishou.agent.operation.backend.batcheval.dto;

import lombok.Data;

/**
 * 数据集列表筛选条件；字段为空表示不过滤。
 */
@Data
public class DatasetListFilter {

    private String name;
    private String datasetType;
    private String creator;
    /** EVAL=离线跑数，TEMPLATE=pipeline 入口模版 */
    private String usage;
    /** 与 usage=TEMPLATE 搭配，筛某个 pipeline 的模版 */
    private String pipelineName;

    public boolean hasName() {
        return hasText(name);
    }

    public boolean hasDatasetType() {
        return hasText(datasetType);
    }

    public boolean hasCreator() {
        return hasText(creator);
    }

    public boolean hasUsage() {
        return hasText(usage);
    }

    public boolean hasPipelineName() {
        return hasText(pipelineName);
    }

    private static boolean hasText(String value) {
        return value != null && !value.trim().isEmpty();
    }
}
