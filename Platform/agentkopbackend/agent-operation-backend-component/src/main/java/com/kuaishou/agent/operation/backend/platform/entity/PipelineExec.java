package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;
import com.fasterxml.jackson.annotation.JsonRawValue;
import com.fasterxml.jackson.databind.annotation.JsonDeserialize;
import com.kuaishou.agent.operation.backend.platform.support.JsonNodeToStringDeserializer;

import lombok.Data;

/**
 * Pipeline 单独执行投影，对应表 {@code aop_pipeline_exec}。
 * 与 agent_loop 的 {@link ChatRun} 对等：记录一次可单独运行的 pipeline 入参/出参/状态。
 */
@Data
@TableName("aop_pipeline_exec")
public class PipelineExec {

    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private String publicId;
    private String tenantId;
    private String userId;
    private Long sessionId;
    private Long workflowId;
    private Long workflowVersionId;
    private Long agentVersionId;
    private String agentkSessionId;
    private String agentkExecId;
    private String pipelineName;
    /** Lab：agent-k capability id；普通 pipeline / KEffects 为空串。 */
    private String capabilityId;

    @JsonRawValue
    @JsonDeserialize(using = JsonNodeToStringDeserializer.class)
    private String argsJson;

    @JsonRawValue
    @JsonDeserialize(using = JsonNodeToStringDeserializer.class)
    private String outputJson;

    private String profileVersion;

    @TableField("`status`")
    private String status;

    @TableField("`error`")
    private String error;

    private LocalDateTime startedAt;
    private LocalDateTime finishedAt;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
