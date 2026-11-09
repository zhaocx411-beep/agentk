package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * {@code GET /v1/me} 响应（人身份或 service API Key 身份）。
 */
@Data
public class AgentKMeDto {

    private String id;
    private String kind;
    private String tenantId;
    private String display;
    private String avatar;
}
