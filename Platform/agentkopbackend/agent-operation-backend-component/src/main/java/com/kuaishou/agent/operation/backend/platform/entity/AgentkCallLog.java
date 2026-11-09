package com.kuaishou.agent.operation.backend.platform.entity;

import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

@Data
@TableName("aop_agentk_call_log")
public class AgentkCallLog {
    @TableId(value = "id", type = IdType.AUTO)
    private Long id;
    private String bizType;
    private Long bizId;
    private String userId;
    private String tenantId;
    private String api;
    private String requestJson;
    private String status;
    private Integer httpStatus;
    private String error;
    private String agentkSessionId;
    private String agentkRunId;
    private String agentkJobId;
    private String agentkWorkflowExecId;
    private LocalDateTime createdAt;
    private LocalDateTime finishedAt;
}
