package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_agentk_capability")
public class AgentKCapabilityCache {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    /** agent-k 原始返回的 id，如 agentk.generate_brand_ad。 */
    private String agentkId;
    /** workflow / activity */
    private String kind;
    private String name;
    private String description;
    private String inputSchemaJson;
    private String outputSchemaJson;
    private String uiSchemaJson;
    private String toolRef;
    private String pipelineRef;
    private String toolName;
    private String toolDescription;
    private String toolMode;
    private String pipelineName;
    private String pipelineDescription;
    private Long timeoutSeconds;
    private String schemaHash;
    private Boolean enabled;
    private LocalDateTime lastSeenAt;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
