package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * Run 响应，与 agent-k {@code runDTO} 对齐。
 * status: queued | running | completed | failed
 */
@Data
public class AgentKRunDto {

    private String id;
    private String sessionId;
    private String status;
    private String profileVersion;
    private String createdAt;
    private String finishedAt;
}
