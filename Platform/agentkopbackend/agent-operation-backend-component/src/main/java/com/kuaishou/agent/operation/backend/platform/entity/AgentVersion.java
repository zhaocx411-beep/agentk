package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;
import com.fasterxml.jackson.annotation.JsonRawValue;
import com.fasterxml.jackson.databind.annotation.JsonDeserialize;
import com.kuaishou.agent.operation.backend.platform.support.JsonNodeToStringDeserializer;

import lombok.Data;

@Data
@TableName("aop_agent_version")
public class AgentVersion {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private Long agentId;
    private Integer version;
    /**
     * agent-k Profile JSON（列名历史遗留 profile_yaml）。
     * API 读写均按 JSON object；库内以 JSON 列存储。
     */
    @JsonRawValue
    @JsonDeserialize(using = JsonNodeToStringDeserializer.class)
    private String profileYaml;
    private String contentHash;
    private String status;
    private String createdBy;
    private LocalDateTime createdAt;
    private LocalDateTime publishedAt;
    private String publishedBy;
}
