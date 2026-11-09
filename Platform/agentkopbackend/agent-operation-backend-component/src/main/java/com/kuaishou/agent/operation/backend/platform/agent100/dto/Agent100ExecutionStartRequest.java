package com.kuaishou.agent.operation.backend.platform.agent100.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * {@code POST /platform/agent100/{agentId}/executions} 请求体。
 */
@Data
public class Agent100ExecutionStartRequest {

    /** 注册表内的 pipeline name，如 documentary_video。 */
    private String pipeline;
    private JsonNode args;
    /** 可选；复用已有 agent-k session。 */
    private String sessionId;
    private String title;
}
