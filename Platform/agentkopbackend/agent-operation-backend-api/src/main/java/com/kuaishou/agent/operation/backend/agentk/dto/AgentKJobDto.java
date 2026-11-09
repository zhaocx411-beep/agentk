package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

/**
 * 异步长任务，与 agent-k {@code jobDTO} 对齐。
 */
@Data
public class AgentKJobDto {

    private String jobId;
    private String kind;
    private String status;
    private String remoteStatus;
    private String wakePolicy;
    private String lastPolledAt;
    private Long elapsedMs;
    private String error;
    private String createdAt;
}
