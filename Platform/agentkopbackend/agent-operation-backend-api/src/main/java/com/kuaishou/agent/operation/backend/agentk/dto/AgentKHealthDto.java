package com.kuaishou.agent.operation.backend.agentk.dto;

import lombok.Data;

@Data
public class AgentKHealthDto {

    /** agent-k /healthz 响应体，正常为 "ok"。 */
    private String status;
    private boolean agentkReachable;
    private long latencyMs;
}
