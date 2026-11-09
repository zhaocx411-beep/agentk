package com.kuaishou.agent.operation.backend.agentk.dto;

import java.time.LocalDateTime;

import lombok.Data;

@Data
public class AgentKCapabilityRefreshResultDto {
    private Boolean refreshed;
    private Boolean skipped;
    private Boolean changed;
    private Integer added;
    private Integer updated;
    private Integer disabled;
    private Integer total;
    private String snapshotHash;
    private String message;
    private LocalDateTime refreshedAt;
}
