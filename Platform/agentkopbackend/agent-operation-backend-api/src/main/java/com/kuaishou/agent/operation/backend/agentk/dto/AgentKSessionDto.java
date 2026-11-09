package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * Session 响应，与 agent-k {@code sessionDTO} 对齐。
 */
@Data
public class AgentKSessionDto {

    private String id;
    private String tenantId;
    private String title;
    private String createdAt;
}
