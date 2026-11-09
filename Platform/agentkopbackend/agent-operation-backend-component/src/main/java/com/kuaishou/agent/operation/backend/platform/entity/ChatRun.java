package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_execution")
public class ChatRun {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private Long sessionId;
    private String agentkSessionId;
    @TableField("agentk_execution_id")
    private String agentkRunId;
    private String triggerKind;
    @TableField("`status`")
    private String status;
    @TableField(exist = false)
    private String parentAgentkRunId;
    /** 以下 round.started 细节在 aop_session_event，库表已删，仅内存保留。 */
    @TableField(exist = false)
    private Integer lastEventSeq;
    @TableField(exist = false)
    private String lastRoundId;
    @TableField(exist = false)
    private String metricsJson;
    @TableField("`error`")
    private String error;
    @TableField(exist = false)
    private String model;
    @TableField(exist = false)
    private String dialect;
    private String profileVersion;
    @TableField(exist = false)
    private String promptFingerprint;
    @TableField(exist = false)
    private String toolsJson;
    @TableField(exist = false)
    private String toolModelsJson;
    @TableField(exist = false)
    private String workerId;
    @TableField(exist = false)
    private Boolean configMismatch;
    private LocalDateTime createdAt;
    private LocalDateTime startedAt;
    private LocalDateTime finishedAt;
}
