package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

/**
 * {@code GET /v1/sessions/{id}/pipelines} 响应。
 */
@Data
public class AgentKListWorkflowsResponse {

    private List<AgentKWorkflowSummaryDto> pipelines;
}
