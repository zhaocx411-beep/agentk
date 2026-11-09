package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

@Data
public class AgentKRegressionWorkflowTaskResponse {

    private String sessionId;
    private String execId;
    private String status;
    private AgentKWorkflowExecDto exec;
    private Long durationMs;
    private String error;
}
