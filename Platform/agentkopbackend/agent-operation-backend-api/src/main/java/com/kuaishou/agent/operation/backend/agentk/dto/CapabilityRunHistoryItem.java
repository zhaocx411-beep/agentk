package com.kuaishou.agent.operation.backend.agentk.dto;

import java.time.LocalDateTime;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * Capability Lab 历史一条：谁、哪个工具、入参、出参。
 */
@Data
public class CapabilityRunHistoryItem {

    private String userId;
    /** agent-k 能力 ID，如 {@code agentk.edit_image}。 */
    private String capabilityId;
    /** agent-k session id，形如 {@code ses_xxx}。 */
    private String sessionId;
    /** agent-k execution id。 */
    private String executionId;
    private String status;
    /** 调用工具时的入参。 */
    private JsonNode args;
    /** 工具输出（含 artifacts）。 */
    private JsonNode output;
    private String error;
    private LocalDateTime createdAt;
    private LocalDateTime finishedAt;
}
