package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

@Data
public class AgentKListSessionsResponse {

    private List<AgentKSessionDto> sessions;
    private Boolean hasMore;
}
