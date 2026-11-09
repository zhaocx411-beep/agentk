package com.kuaishou.agent.operation.backend.agentk.dto;

import java.time.LocalDateTime;

import lombok.Data;

@Data
public class AgentKCapabilityCacheMetaDto {
    /** live：本次直接来自 agent-k gRPC；cache：读库。 */
    private String source;
    private Boolean stale;
    private Boolean refreshing;
    private LocalDateTime lastRefreshAt;
    private String refreshStatus;
    private String errorMessage;
    private Long ttlSeconds;
}
