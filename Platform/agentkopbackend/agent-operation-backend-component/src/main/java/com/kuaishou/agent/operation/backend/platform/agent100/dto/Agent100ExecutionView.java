package com.kuaishou.agent.operation.backend.platform.agent100.dto;

import java.util.ArrayList;
import java.util.List;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * Agent100 单次执行快照。
 */
@Data
public class Agent100ExecutionView {

    private String agentId;
    private String sessionId;
    private String executionId;
    private String pipeline;
    private String capabilityId;
    private String status;
    private String error;
    private List<Agent100EventView> events = new ArrayList<>();
    private JsonNode output;
    private JsonNode request;
    private Boolean cached;
    private String statusSyncError;

    @Data
    public static class Agent100EventView {
        private String id;
        private String type;
        private String step;
        private String message;
        private String status;
        private String error;
        private JsonNode data;
    }
}
