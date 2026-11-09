package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import lombok.Data;

@Data
public class AgentKTraceDto {

    private String runId;
    private String status;
    private String createdAt;
    private String finishedAt;
    private List<AgentKListItemsResponse.AgentKItemDto> items;
    private List<AgentKListEventsResponse.AgentKEventDto> events;
}
