package com.kuaishou.agent.operation.backend.agentk.dto;

import java.util.ArrayList;
import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

@Data
public class AgentKCapabilityViewDto {
    private String id;
    /** workflow / activity */
    private String kind;
    private String name;
    private String title;
    private String description;
    private String toolRef;
    private String pipelineRef;
    private String mode;
    private Long timeoutSeconds;
    private JsonNode inputSchema;
    private JsonNode outputSchema;
    private JsonNode uiSchema;
    private List<FieldView> fields = new ArrayList<>();

    @Data
    public static class FieldView {
        private String name;
        private String label;
        private String description;
        private String type;
        private String widget;
        private Boolean required;
        private JsonNode schema;
        private JsonNode defaultValue;
        private List<String> enumValues;
        private FieldView items;
    }
}
