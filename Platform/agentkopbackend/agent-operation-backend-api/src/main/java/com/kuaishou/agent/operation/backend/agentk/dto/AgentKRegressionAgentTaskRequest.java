package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * Agent 在线回归任务请求（BFF 编排：建会话 → 发消息 → 轮询会话事件终态 → 判定）。
 */
@Data
public class AgentKRegressionAgentTaskRequest {

    private String title;
    /** 与 agent-k 一致的 Profile JSON object。 */
    private JsonNode profile;
    private String prompt;
    private AgentKRegressionChecksDto checks;
    /** 是否等待会话内异步 job 全部终态（看 job.* 事件）。 */
    private Boolean waitForJobs;
    private Long timeoutMs;
    private Long pollIntervalMs;
}
