package com.kuaishou.agent.operation.backend.platform.dto;

import lombok.Data;

/**
 * 创建 Dataset。
 */
@Data
public class CreateDatasetRequest {
    private String name;
    private String description;
    private String keyColumn;
    private String inputMappingJson;
    private String tenantId;
}
