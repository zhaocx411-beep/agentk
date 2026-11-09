package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * Capability 单次执行入参。标识均为 agent-k 字符串，不是平台主键。
 */
@Data
public class CapabilityRunRequest {

    /** Agent-K capability 唯一标识，如 {@code agentk.generate_image}。 */
    private String capabilityId;
    private String title;
    private JsonNode args;
}
