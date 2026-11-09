package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

/**
 * 创建平台 Workflow 逻辑实体。
 */
@Data
public class CreateWorkflowRequest {
    private String name;
    private String description;
    private String tenantId;
}
