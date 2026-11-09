package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

@Data
public class AgentKListItemsResponse {

    private List<AgentKItemDto> items;
    private Boolean hasMore;

    @Data
    public static class AgentKItemDto {
        private Long seq;
        private String role;
        private JsonNode parts;
        private String createdAt;
    }
}
