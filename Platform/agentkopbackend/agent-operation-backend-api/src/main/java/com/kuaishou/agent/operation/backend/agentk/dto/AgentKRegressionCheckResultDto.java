package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

@Data
public class AgentKRegressionCheckResultDto {

    private boolean passed;
    private List<String> reasons;
}
