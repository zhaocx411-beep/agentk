package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * {@code POST /v1/sessions/{id}/cancel} 请求体（可省略）。
 */
@Data
public class AgentKCancelRequest {

    private String reason;
}
