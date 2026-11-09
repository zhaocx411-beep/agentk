package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

/**
 * {@code GET /v1/sessions/{id}/pipeline-runs} 响应。
 */
@Data
public class AgentKListWorkflowRunsResponse {

    private List<AgentKWorkflowExecDto> pipelineRuns;
}
