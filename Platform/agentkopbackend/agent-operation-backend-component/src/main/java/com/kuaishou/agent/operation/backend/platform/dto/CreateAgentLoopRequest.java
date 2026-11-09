package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

/**
 * 创建 AgentLoop（平台侧 Agent 逻辑实体，对应 agent-k Agent Loop 的 profile 版本体系）。
 */
@Data
public class CreateAgentLoopRequest {
    private String name;
    private String description;
    private String tenantId;
}
