package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

@Data
public class AgentKRegressionAgentTaskResponse {

    private String sessionId;
    /** @deprecated 新 API 无 run；保留字段便于旧调用方读到 null。 */
    @Deprecated
    private String runId;
    private String status;
    private AgentKRegressionCheckResultDto check;
    private List<AgentKListEventsResponse.AgentKEventDto> events;
    private Long durationMs;
    private String error;
}
