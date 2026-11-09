package com.kuaishou.agent.operation.backend.platform.agent100.dto;

import lombok.Data;

/**
 * Agent100 目录项（对齐 Demo {@code public_agents()}，不含 worker_module）。
 */
@Data
public class Agent100AgentView {

    private String id;
    private String name;
    private String icon;
    private String accent;
    private String description;
    private String contractVersion;
}
