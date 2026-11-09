package com.kuaishou.agent.operation.backend.keffects.dto;

import java.util.Map;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * KEffects 创建任务通用信封。
 */
@Data
public class KEffectsCreateTaskRequest {

    /** 业务能力标识（必填）；由对应 adapter 校验 {@code input} 结构。 */
    private String taskType;

    /** 幂等键；强烈建议传入（幂等范围：taskType + requestId）。 */
    private String requestId;

    /** 任务专属入参，保持原始 JSON，由 taskType adapter 转成具体 DTO。 */
    private JsonNode input;

    /** 回调等可选项。 */
    private Options options;

    @Data
    public static class Options {
        private String callbackUrl;
        private Map<String, Object> callbackPayload;
    }
}
