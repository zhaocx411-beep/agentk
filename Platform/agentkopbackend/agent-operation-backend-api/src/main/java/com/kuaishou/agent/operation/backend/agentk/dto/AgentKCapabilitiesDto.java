package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.ArrayList;
import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * {@code GET /v1/capabilities} 响应：Worker 已上报的能力目录（契约 + Profile 引用串）。
 */
@Data
public class AgentKCapabilitiesDto {

    private List<AgentKCapabilityItemDto> capabilities = new ArrayList<>();

    @Data
    public static class AgentKCapabilityItemDto {
        private String id;
        /** workflow / activity */
        private String kind;
        private String description;
        private JsonNode inputSchema;
        private JsonNode outputSchema;
        private Long timeoutSeconds;
        /** 写进 Profile tools 时用，如 {@code tool:agentk.generate_brand_ad} */
        private String toolRef;
        /** 写进 Profile pipelines 时用，如 {@code pipeline:agentk.generate_brand_ad} */
        private String pipelineRef;
        private AgentKCapabilityToolDto tool;
        private AgentKCapabilityPipelineDto pipeline;
    }

    @Data
    public static class AgentKCapabilityToolDto {
        private String name;
        private String description;
        /** blocking / background */
        private String mode;
    }

    @Data
    public static class AgentKCapabilityPipelineDto {
        private String name;
        private String description;
    }
}
