package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * {@code GET /v1/sessions/{id}/events} 响应。
 */
@Data
public class AgentKListEventsResponse {

    private List<AgentKEventDto> events;
    private Boolean hasMore;

    @Data
    public static class AgentKEventDto {
        /** Agent-K Event id，如 evt_xxx。 */
        private String eventId;
        /** Agent-K Execution id，如 exe_xxx。 */
        private String executionId;
        /**
         * Agent-K 会话内序号。现行 gRPC Event 不含该字段时为 null，禁止平台自增填入。
         */
        private Long seq;
        private String roundId;
        private String kind;
        /** 载荷，形状由 kind 判别（agent-k wire 字段名为 {@code data}）。 */
        private JsonNode data;
        private String createdAt;
    }
}
