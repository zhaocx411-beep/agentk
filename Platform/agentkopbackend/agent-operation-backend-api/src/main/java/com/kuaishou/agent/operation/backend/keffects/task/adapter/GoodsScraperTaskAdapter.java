package com.kuaishou.agent.operation.backend.keffects.task.adapter;

import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAdapter;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService.AssetProfile;

/**
 * {@code e-commerce-goods-scraper} 任务适配。
 * <p>
 * 上游 {@code kling-aigc-engine}：taskId 与响应体原样透传。
 * 落库由 facade 统一走 {@code aop_pipeline_exec} / {@code aop_asset}（{@code source_kind=api}）。
 */
@Component
public class GoodsScraperTaskAdapter implements KEffectsTaskAdapter {

    public static final String TASK_TYPE = "e-commerce-goods-scraper";

    private static final AssetProfile ASSET_PROFILE = AssetProfile.of(TASK_TYPE);

    @Override
    public String taskType() {
        return TASK_TYPE;
    }

    @Override
    public String upstreamBaseUrl() {
        return KlingAigcEngineUpstream.baseUrl();
    }

    @Override
    public AssetProfile assetProfile() {
        return ASSET_PROFILE;
    }

    @Override
    public void validateCreate(KEffectsCreateTaskRequest request) {
        if (request == null) {
            throw new IllegalArgumentException("request body is required");
        }
        if (StringUtils.hasText(request.getTaskType()) && !TASK_TYPE.equals(request.getTaskType().trim())) {
            throw new IllegalArgumentException("taskType must be " + TASK_TYPE);
        }
        JsonNode input = request.getInput();
        if (input == null || !input.isObject()) {
            throw new IllegalArgumentException("input is required");
        }
        JsonNode url = input.get("url");
        if (url == null || !url.isTextual() || !StringUtils.hasText(url.asText())) {
            throw new IllegalArgumentException("input.url is required");
        }
    }
}
