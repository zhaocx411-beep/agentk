package com.kuaishou.agent.operation.backend.agentk.dto;

import com.fasterxml.jackson.databind.JsonNode;

import lombok.Data;

/**
 * AgentK {@code InvokeCapability} 的 RPC 输出（平台后台线程收取后落库）。
 */
@Data
public class CapabilityInvokeOutputDto {

    private String capabilityId;
    private JsonNode output;
}
