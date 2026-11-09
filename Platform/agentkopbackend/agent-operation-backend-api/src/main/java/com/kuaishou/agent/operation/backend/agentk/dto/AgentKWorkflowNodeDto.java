package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

@Data
public class AgentKWorkflowNodeDto {

    private String nodeId;
    private Integer idx;
    private String status;
    private JsonNode output;
    private String error;
    private Integer attempts;
}
