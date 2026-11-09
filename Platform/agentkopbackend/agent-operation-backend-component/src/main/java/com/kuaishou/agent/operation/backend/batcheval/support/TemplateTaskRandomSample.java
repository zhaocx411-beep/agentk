package com.kuaishou.agent.operation.backend.batcheval.support;

import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;

/**
 * AgentLoop 模版入口：指定 pipeline 查用例时随机抽样，避免每次固定返回前 N 条。
 */
public final class TemplateTaskRandomSample {

    public static final int SAMPLE_SIZE = 6;

    private static final Set<String> RANDOM_PIPELINE_NAMES = Collections.unmodifiableSet(new HashSet<>(Arrays.asList(
            "漫剧动态漫Agent",
            "完整创作助手",
            "图文视频创作",
            "视频剪辑与生成")));

    private TemplateTaskRandomSample() {
    }

    public static boolean shouldSample(DatasetTaskListFilter filter) {
        if (filter == null || filter.hasDatasetId()) {
            return false;
        }
        if (!DatasetUsages.TEMPLATE.equals(filter.getUsage()) || !filter.hasPipelineName()) {
            return false;
        }
        return RANDOM_PIPELINE_NAMES.contains(filter.getPipelineName());
    }

    public static Set<String> randomPipelineNames() {
        return RANDOM_PIPELINE_NAMES;
    }
}
