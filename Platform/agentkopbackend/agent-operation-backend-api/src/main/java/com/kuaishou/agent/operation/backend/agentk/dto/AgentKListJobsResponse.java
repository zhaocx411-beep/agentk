package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

@Data
public class AgentKListJobsResponse {

    private List<AgentKJobDto> jobs;
}
