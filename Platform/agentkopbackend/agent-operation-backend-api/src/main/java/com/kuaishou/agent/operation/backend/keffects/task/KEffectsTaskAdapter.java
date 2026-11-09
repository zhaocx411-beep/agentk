package com.kuaishou.agent.operation.backend.keffects.task;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService.AssetProfile;

/**
 * 单个 KEffects taskType 的业务适配器。
 * <p>
 * Adapter 负责参数校验、请求规范化、响应补齐与 {@link #assetProfile()}；
 * HTTP、用户身份、错误映射、以及 {@code aop_pipeline_exec} / {@code aop_asset} 落库由 facade 统一处理。
 */
public interface KEffectsTaskAdapter {

    String taskType();

    /** 该 taskType 对应的 KEffects / kling-aigc-engine 根地址，不含 /v1。 */
    String upstreamBaseUrl();

    void validateCreate(KEffectsCreateTaskRequest request);

    /**
     * 出站上游前规范化请求（如修正 taskType、contents 形状）；默认不修改。
     */
    default void normalizeCreate(KEffectsCreateTaskRequest request) {
    }

    /** 落库用的 taskType + 媒体字段优先级；facade 据此写 pipeline_exec / asset。 */
    default AssetProfile assetProfile() {
        return AssetProfile.of(taskType());
    }

    /**
     * 适配器层额外补齐；bucket+key → blobUrl 由 facade 统一走 BlobStore AK/SK 签发。
     */
    default JsonNode enrichTask(JsonNode upstream) {
        return upstream;
    }
}
