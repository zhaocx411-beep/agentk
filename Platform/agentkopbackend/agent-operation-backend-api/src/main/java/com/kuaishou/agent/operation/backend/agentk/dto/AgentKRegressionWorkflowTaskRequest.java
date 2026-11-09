package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * Pipeline 在线回归任务请求（BFF 编排：建会话 → 启动 pipeline → 轮询终态）。
 */
@Data
public class AgentKRegressionWorkflowTaskRequest {

    private String title;
    /** 与 agent-k 一致的 Profile JSON object。 */
    private JsonNode profile;
    private String pipeline;
    private JsonNode inputs;
    private Long timeoutMs;
    private Long pollIntervalMs;
}
