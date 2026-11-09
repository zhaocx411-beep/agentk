package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.Map;

import lombok.Data;

/**
 * Pipeline 发现摘要，与 agent-k {@code pipeline.Summary} 对齐。
 */
@Data
public class AgentKWorkflowSummaryDto {

    private String name;
    private String description;
    private Map<String, Object> inputs;
}
